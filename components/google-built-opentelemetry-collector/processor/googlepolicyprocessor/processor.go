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

	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

type googlePolicyProcessor struct {
	cfg    *Config
	logger *zap.Logger
}

func newGooglePolicyProcessor(cfg *Config, logger *zap.Logger) *googlePolicyProcessor {
	return &googlePolicyProcessor{
		cfg:    cfg,
		logger: logger,
	}
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
	// TODO: Apply policy transformations to logs.
	return ld, nil
}
