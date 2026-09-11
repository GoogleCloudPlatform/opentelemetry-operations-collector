// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package googlepolicyprocessor

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/processor/googlepolicyprocessor/internal/metadata"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

type googlePolicyProcessor struct {
	cfg       *Config
	logger    *zap.Logger
	telemetry *metadata.TelemetryBuilder
	evaluator atomic.Pointer[Evaluator]
	watcherCh googlepolicy.WatcherChannel
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func newGooglePolicyProcessor(cfg *Config, logger *zap.Logger, telemetry *metadata.TelemetryBuilder) *googlePolicyProcessor {
	return &googlePolicyProcessor{
		cfg:       cfg,
		logger:    logger,
		telemetry: telemetry,
	}
}

func (p *googlePolicyProcessor) start(_ context.Context, _ component.Host) error {
	p.logger.Info("Starting Google policy processor")
	p.stopCh = make(chan struct{})
	p.watcherCh = googlepolicy.RegisterWatcherChannel()

	p.reloadPolicies()

	p.wg.Add(1)
	go p.watchPolicies()

	return nil
}

func (p *googlePolicyProcessor) reloadPolicies() {
	ps := googlepolicy.ActivePolicySet()
	if ps == nil {
		p.evaluator.Store(nil)
		return
	}

	policies := ps.TransformationPolicies()
	ev, err := NewEvaluator(policies)
	if err != nil {
		p.logger.Error("Failed to compile updated transformation policies", zap.Error(err))
		return
	}

	p.evaluator.Store(ev)
	p.logger.Info("Successfully compiled and updated transformation policy engine",
		zap.String("revision_id", ps.RevisionID),
		zap.Int("policy_count", len(policies)),
	)
}

func (p *googlePolicyProcessor) watchPolicies() {
	defer p.wg.Done()
	for {
		select {
		case <-p.stopCh:
			return
		case _, ok := <-p.watcherCh:
			if !ok {
				return
			}
			p.logger.Info("Detected policy set update, reloading transformation policies")
			p.reloadPolicies()
		}
	}
}

func (p *googlePolicyProcessor) shutdown(_ context.Context) error {
	p.logger.Info("Shutting down Google policy processor")
	if p.stopCh != nil {
		close(p.stopCh)
	}
	if p.watcherCh != nil {
		googlepolicy.UnregisterWatcherChannel(p.watcherCh)
	}
	p.wg.Wait()
	return nil
}

func (p *googlePolicyProcessor) recordBatchMetrics(ctx context.Context, telemetryType string, stats TransformStats) {
	if p.telemetry == nil {
		return
	}

	// Tier 1: Aggregate records
	if stats.Dropped > 0 {
		p.telemetry.ProcessorGooglepolicyRecords.Add(ctx, stats.Dropped, metric.WithAttributes(
			attribute.String("telemetry_type", telemetryType),
			attribute.String("result", string(ResultDropped)),
		))
	}
	if stats.Kept > 0 {
		p.telemetry.ProcessorGooglepolicyRecords.Add(ctx, stats.Kept, metric.WithAttributes(
			attribute.String("telemetry_type", telemetryType),
			attribute.String("result", string(ResultKept)),
		))
	}
	if stats.NoMatch > 0 {
		p.telemetry.ProcessorGooglepolicyRecords.Add(ctx, stats.NoMatch, metric.WithAttributes(
			attribute.String("telemetry_type", telemetryType),
			attribute.String("result", string(ResultNoMatch)),
		))
	}
	if stats.Transformed > 0 {
		p.telemetry.ProcessorGooglepolicyRecords.Add(ctx, stats.Transformed, metric.WithAttributes(
			attribute.String("telemetry_type", telemetryType),
			attribute.String("result", string(ResultTransformed)),
		))
	}

	// Tier 2: Per-policy records
	for policyID, outcomes := range stats.PolicyRecords {
		for result, count := range outcomes {
			if count > 0 {
				p.telemetry.ProcessorGooglepolicyPolicyRecords.Add(ctx, count, metric.WithAttributes(
					attribute.String("policy_id", policyID),
					attribute.String("telemetry_type", telemetryType),
					attribute.String("result", string(result)),
				))
			}
		}
	}
}

func (p *googlePolicyProcessor) processLogs(ctx context.Context, ld plog.Logs) (plog.Logs, error) {
	if ev := p.evaluator.Load(); ev != nil {
		stats := ev.TransformLogs(ld)
		p.recordBatchMetrics(ctx, "logs", stats)
	}
	return ld, nil
}

func (p *googlePolicyProcessor) processMetrics(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	if ev := p.evaluator.Load(); ev != nil {
		stats := ev.TransformMetrics(md)
		p.recordBatchMetrics(ctx, "metrics", stats)
	}
	return md, nil
}

func (p *googlePolicyProcessor) processTraces(ctx context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	if ev := p.evaluator.Load(); ev != nil {
		stats := ev.TransformTraces(td)
		p.recordBatchMetrics(ctx, "traces", stats)
	}
	return td, nil
}
