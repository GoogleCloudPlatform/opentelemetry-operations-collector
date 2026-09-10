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
	"go.opentelemetry.io/collector/pdata/pcommon"
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

	// 1. Initial compilation of active transformation policies
	p.reloadPolicies()

	// 2. Register for dynamic policy updates
	p.watcherCh = googlepolicy.RegisterWatcherChannel()
	p.stopCh = make(chan struct{})

	p.wg.Add(1)
	go p.watchPolicyUpdates()

	return nil
}

func (p *googlePolicyProcessor) reloadPolicies() {
	activeSet := googlepolicy.ActivePolicySet()
	var policies []googlepolicy.TransformationPolicy
	if activeSet != nil {
		policies = activeSet.TransformationPolicies()
	}

	ev, err := NewEvaluator(policies)
	if err != nil {
		p.logger.Error("Failed to compile active transformation policies", zap.Error(err))
		return
	}
	p.evaluator.Store(ev)
	p.logger.Debug("Reloaded transformation policies", zap.Int("policy_count", len(policies)))
}

func (p *googlePolicyProcessor) watchPolicyUpdates() {
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
	ev := p.evaluator.Load()
	if ev == nil {
		return ld, nil
	}

	ld.ResourceLogs().RemoveIf(func(rl plog.ResourceLogs) bool {
		resource := rl.Resource()
		resourceSchemaURL := rl.SchemaUrl()

		rl.ScopeLogs().RemoveIf(func(sl plog.ScopeLogs) bool {
			scope := sl.Scope()
			scopeSchemaURL := sl.SchemaUrl()

			sl.LogRecords().RemoveIf(func(lr plog.LogRecord) bool {
				return ev.EvalLog(lr, resource, scope, resourceSchemaURL, scopeSchemaURL)
			})

			return sl.LogRecords().Len() == 0
		})

		return rl.ScopeLogs().Len() == 0
	})

	return ld, nil
}

func (p *googlePolicyProcessor) processMetrics(_ context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	ev := p.evaluator.Load()
	if ev == nil {
		return md, nil
	}

	md.ResourceMetrics().RemoveIf(func(rm pmetric.ResourceMetrics) bool {
		resource := rm.Resource()
		resourceSchemaURL := rm.SchemaUrl()

		rm.ScopeMetrics().RemoveIf(func(sm pmetric.ScopeMetrics) bool {
			scope := sm.Scope()
			scopeSchemaURL := sm.SchemaUrl()

			sm.Metrics().RemoveIf(func(m pmetric.Metric) bool {
				return p.processMetricDatapoints(m, resource, scope, resourceSchemaURL, scopeSchemaURL, ev)
			})

			return sm.Metrics().Len() == 0
		})

		return rm.ScopeMetrics().Len() == 0
	})

	return md, nil
}

func (p *googlePolicyProcessor) processMetricDatapoints(m pmetric.Metric, resource pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string, ev *Evaluator) bool {
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		m.Gauge().DataPoints().RemoveIf(func(dp pmetric.NumberDataPoint) bool {
			return ev.EvalMetricDatapoint(m, dp.Attributes(), pmetric.AggregationTemporalityUnspecified, resource, scope, resSchema, scopeSchema)
		})
		return m.Gauge().DataPoints().Len() == 0
	case pmetric.MetricTypeSum:
		sum := m.Sum()
		sum.DataPoints().RemoveIf(func(dp pmetric.NumberDataPoint) bool {
			return ev.EvalMetricDatapoint(m, dp.Attributes(), sum.AggregationTemporality(), resource, scope, resSchema, scopeSchema)
		})
		return sum.DataPoints().Len() == 0
	case pmetric.MetricTypeHistogram:
		hist := m.Histogram()
		hist.DataPoints().RemoveIf(func(dp pmetric.HistogramDataPoint) bool {
			return ev.EvalMetricDatapoint(m, dp.Attributes(), hist.AggregationTemporality(), resource, scope, resSchema, scopeSchema)
		})
		return hist.DataPoints().Len() == 0
	case pmetric.MetricTypeExponentialHistogram:
		expHist := m.ExponentialHistogram()
		expHist.DataPoints().RemoveIf(func(dp pmetric.ExponentialHistogramDataPoint) bool {
			return ev.EvalMetricDatapoint(m, dp.Attributes(), expHist.AggregationTemporality(), resource, scope, resSchema, scopeSchema)
		})
		return expHist.DataPoints().Len() == 0
	case pmetric.MetricTypeSummary:
		m.Summary().DataPoints().RemoveIf(func(dp pmetric.SummaryDataPoint) bool {
			return ev.EvalMetricDatapoint(m, dp.Attributes(), pmetric.AggregationTemporalityUnspecified, resource, scope, resSchema, scopeSchema)
		})
		return m.Summary().DataPoints().Len() == 0
	default:
		return false
	}
}

func (p *googlePolicyProcessor) processTraces(_ context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	ev := p.evaluator.Load()
	if ev == nil {
		return td, nil
	}

	td.ResourceSpans().RemoveIf(func(rs ptrace.ResourceSpans) bool {
		resource := rs.Resource()
		resourceSchemaURL := rs.SchemaUrl()

		rs.ScopeSpans().RemoveIf(func(ss ptrace.ScopeSpans) bool {
			scope := ss.Scope()
			scopeSchemaURL := ss.SchemaUrl()

			ss.Spans().RemoveIf(func(span ptrace.Span) bool {
				return ev.EvalTraceSpan(span, resource, scope, resourceSchemaURL, scopeSchemaURL)
			})

			return ss.Spans().Len() == 0
		})

		return rs.ScopeSpans().Len() == 0
	})

	return td, nil
}
