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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.uber.org/zap"
)

func TestModifyScopeProcessor(t *testing.T) {
	testStart := time.Now().Unix()

	newName := "newName"
	newVersion := "newVersion"
	for _, tt := range []struct {
		name                    string
		scopeName, scopeVersion *string
	}{
		{"empty", nil, nil},
		{"name-only", &newName, nil},
		{"both", &newName, &newVersion},
		{"version-only", nil, &newVersion},
	} {
		t.Run(tt.name, func(t *testing.T) {
			id := component.NewID(typeStr)
			settings := config.NewProcessorSettings(id)
			cfg := &Config{
				ProcessorSettings:    &settings,
				OverrideScopeName:    tt.scopeName,
				OverrideScopeVersion: tt.scopeVersion,
			}
			msp := newModifyScopeProcessor(zap.NewExample(), cfg)

			tmn := &consumertest.MetricsSink{}
			rmp, err := processorhelper.NewMetricsProcessor(
				context.Background(),
				componenttest.NewNopProcessorCreateSettings(),
				cfg,
				tmn,
				msp.ProcessMetrics,
				processorhelper.WithCapabilities(processorCapabilities))
			require.NoError(t, err)

			require.True(t, rmp.Capabilities().MutatesData)

			require.NoError(t, rmp.Start(context.Background(), componenttest.NewNopHost()))
			defer func() { require.NoError(t, rmp.Shutdown(context.Background())) }()

			origMetrics := generateMetrics(testStart)
			err = rmp.ConsumeMetrics(context.Background(), origMetrics)
			require.NoError(t, err)

			got := tmn.AllMetrics()
			require.Equal(t, len(got), 1)
			require.Equal(t, got[0].ResourceMetrics().Len(), 1)
			rmsGot := got[0].ResourceMetrics().At(0)
			require.Equal(t, rmsGot.ScopeMetrics().Len(), 1)
			smGot := rmsGot.ScopeMetrics().At(0)
			if tt.scopeName != nil {
				require.Equal(t, smGot.Scope().Name(), *tt.scopeName)
			} else {
				require.Equal(t, smGot.Scope().Name(), "name")
			}
			if tt.scopeVersion != nil {
				require.Equal(t, smGot.Scope().Version(), *tt.scopeVersion)
			} else {
				require.Equal(t, smGot.Scope().Version(), "version")
			}
		})
	}
}
func generateMetrics(startTime int64) pmetric.Metrics {
	input := pmetric.NewMetrics()

	rm := input.ResourceMetrics().AppendEmpty()
	sms := rm.ScopeMetrics()
	sm := sms.AppendEmpty()
	sm.Scope().SetName("name")
	sm.Scope().SetVersion("version")

	metric := sm.Metrics().AppendEmpty()
	metric.SetEmptyGauge()

	idp := metric.Gauge().DataPoints().AppendEmpty()
	idp.SetIntValue(1)
	idp.SetTimestamp(pcommon.NewTimestampFromTime(time.Unix(startTime, 0)))

	return input
}
