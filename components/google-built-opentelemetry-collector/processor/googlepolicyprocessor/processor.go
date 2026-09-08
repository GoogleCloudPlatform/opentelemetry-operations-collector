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

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

type googlePolicyProcessor struct {
	cfg    *Config
	logger *zap.Logger

	mu         sync.RWMutex
	logFilters []googlepolicy.LogRecordFilter

	watcherCh googlepolicy.WatcherChannel
	done      chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once
	stopOnce  sync.Once
}

func newGooglePolicyProcessor(cfg *Config, logger *zap.Logger) *googlePolicyProcessor {
	return &googlePolicyProcessor{
		cfg:    cfg,
		logger: logger,
	}
}

func (p *googlePolicyProcessor) start(_ context.Context, _ component.Host) error {
	p.startOnce.Do(func() {
		p.mu.Lock()
		p.watcherCh = googlepolicy.RegisterWatcherChannel()
		p.done = make(chan struct{})
		p.mu.Unlock()

		p.updatePolicies()

		p.wg.Add(1)
		go p.watchPolicies()
	})
	return nil
}

func (p *googlePolicyProcessor) shutdown(_ context.Context) error {
	p.stopOnce.Do(func() {
		var ch googlepolicy.WatcherChannel
		p.mu.Lock()
		if p.done != nil {
			close(p.done)
		}
		ch = p.watcherCh
		p.watcherCh = nil
		p.mu.Unlock()

		p.wg.Wait()

		if ch != nil {
			googlepolicy.UnregisterWatcherChannel(ch)
		}
	})
	return nil
}

func (p *googlePolicyProcessor) watchPolicies() {
	defer p.wg.Done()
	for {
		select {
		case <-p.done:
			return
		case <-p.watcherCh:
			p.updatePolicies()
		}
	}
}

func (p *googlePolicyProcessor) updatePolicies() {
	ps := googlepolicy.ActivePolicySet()
	var filters []googlepolicy.LogRecordFilter
	var revisionID string
	if ps != nil {
		revisionID = ps.RevisionID
		for _, pol := range ps.LoadPoliciesOfClass(googlepolicy.PolicyClassTransformation) {
			if lf, ok := pol.(googlepolicy.LogRecordFilter); ok {
				filters = append(filters, lf)
			}
		}
	}

	p.mu.Lock()
	p.logFilters = filters
	p.mu.Unlock()

	if p.logger != nil {
		p.logger.Info("Propagated updated policies from discovery",
			zap.String("revision_id", revisionID),
			zap.Int("active_filters", len(filters)),
		)
	}
}

func (p *googlePolicyProcessor) getLogFilters() []googlepolicy.LogRecordFilter {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.watcherCh != nil {
		return p.logFilters
	}
	// Fallback if processor was not started via start() lifecycle (e.g. simple direct unit tests)
	return p.fetchActiveFilters()
}

func (p *googlePolicyProcessor) fetchActiveFilters() []googlepolicy.LogRecordFilter {
	ps := googlepolicy.ActivePolicySet()
	if ps == nil {
		return nil
	}
	var filters []googlepolicy.LogRecordFilter
	for _, pol := range ps.LoadPoliciesOfClass(googlepolicy.PolicyClassTransformation) {
		if lf, ok := pol.(googlepolicy.LogRecordFilter); ok {
			filters = append(filters, lf)
		}
	}
	return filters
}

func (p *googlePolicyProcessor) processTraces(_ context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	// TODO: Apply policy transformations to traces.
	return td, nil
}

func (p *googlePolicyProcessor) processMetrics(_ context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	// TODO: Apply policy transformations to metrics.
	return md, nil
}

func (p *googlePolicyProcessor) processLogs(_ context.Context, ld plog.Logs) (plog.Logs, error) {
	filters := p.getLogFilters()
	if len(filters) == 0 {
		return ld, nil
	}

	ld.ResourceLogs().RemoveIf(func(rl plog.ResourceLogs) bool {
		rl.ScopeLogs().RemoveIf(func(sl plog.ScopeLogs) bool {
			sl.LogRecords().RemoveIf(func(lr plog.LogRecord) bool {
				for _, lf := range filters {
					if lf.ShouldDrop(lr, sl, rl) {
						return true
					}
				}
				return false
			})
			return sl.LogRecords().Len() == 0
		})
		return rl.ScopeLogs().Len() == 0
	})

	return ld, nil
}
