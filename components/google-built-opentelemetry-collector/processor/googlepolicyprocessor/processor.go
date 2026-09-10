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

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

type googlePolicyProcessor struct {
	cfg       *Config
	logger    *zap.Logger
	evaluator atomic.Pointer[Evaluator]
	watcherCh googlepolicy.WatcherChannel
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func newGooglePolicyProcessor(cfg *Config, logger *zap.Logger) *googlePolicyProcessor {
	return &googlePolicyProcessor{
		cfg:    cfg,
		logger: logger,
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

func (p *googlePolicyProcessor) processLogs(_ context.Context, ld plog.Logs) (plog.Logs, error) {
	if ev := p.evaluator.Load(); ev != nil {
		ev.FilterLogs(ld)
	}
	return ld, nil
}

func (p *googlePolicyProcessor) processMetrics(_ context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	if ev := p.evaluator.Load(); ev != nil {
		ev.FilterMetrics(md)
	}
	return md, nil
}

func (p *googlePolicyProcessor) processTraces(_ context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	if ev := p.evaluator.Load(); ev != nil {
		ev.FilterTraces(td)
	}
	return td, nil
}
