// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package modifyscopeprocessor

import (
	"context"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

type ModifyScopeProcessor struct {
	logger *zap.Logger
	cfg    *Config
}

func newModifyScopeProcessor(logger *zap.Logger, cfg *Config) *ModifyScopeProcessor {
	return &ModifyScopeProcessor{
		logger: logger,
		cfg:    cfg,
	}
}

// ProcessMetrics implements the MProcessor interface.
func (msp *ModifyScopeProcessor) ProcessMetrics(ctx context.Context, metrics pmetric.Metrics) (pmetric.Metrics, error) {
	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		rms := metrics.ResourceMetrics().At(i)
		msp.transformMetrics(rms)
	}

	return metrics, nil
}

func (msp *ModifyScopeProcessor) transformMetrics(rms pmetric.ResourceMetrics) {
	sms := rms.ScopeMetrics()
	for i := 0; i < sms.Len(); i++ {
		sm := sms.At(i)
		scope := sm.Scope()
		if msp.cfg.OverrideScopeName != nil {
			scope.SetName(*msp.cfg.OverrideScopeName)
		}
		if msp.cfg.OverrideScopeVersion != nil {
			scope.SetVersion(*msp.cfg.OverrideScopeVersion)
		}
	}
}
