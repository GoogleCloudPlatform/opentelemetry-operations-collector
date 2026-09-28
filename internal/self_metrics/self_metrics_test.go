// Copyright 2022 Google LLC
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

package self_metrics_test

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator/resourcedetector"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/experiments"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/platform"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/self_metrics"
	"github.com/shirou/gopsutil/host"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"
	"gotest.tools/v3/assert"
)

func TestEnabledReceiversDefaultConfig(t *testing.T) {
	for _, test := range []struct {
		name                 string
		config               *confgenerator.UnifiedConfig
		enabledReceivers     self_metrics.EnabledReceivers
		experimentalFeatures string
	}{
		{
			name:   "builtin_linux",
			config: confgenerator.BuiltInConfStructs["linux"],
			enabledReceivers: self_metrics.EnabledReceivers{
				MetricsReceiverCountsByType: map[string]int{"hostmetrics": 1},
				LogsReceiverCountsByType:    map[string]int{"files": 1},
			},
		},
		{
			name:   "builtin_windows",
			config: confgenerator.BuiltInConfStructs["windows"],
			enabledReceivers: self_metrics.EnabledReceivers{
				MetricsReceiverCountsByType: map[string]int{"hostmetrics": 1, "iis": 1, "mssql": 1},
				LogsReceiverCountsByType:    map[string]int{"windows_event_log": 1},
			},
		},
		{
			name: "combined_receiver",
			config: &confgenerator.UnifiedConfig{
				Combined: &confgenerator.Combined{
					Receivers: map[string]confgenerator.CombinedReceiver{
						"otlp": confgenerator.ReceiverOTLP{},
					},
				},
				Logging: &confgenerator.Logging{
					Service: &confgenerator.LoggingService{
						Pipelines: map[string]*confgenerator.Pipeline{
							"otlp": {
								ReceiverIDs: []string{"otlp"},
							},
						},
					},
				},
				Metrics: &confgenerator.Metrics{
					Service: &confgenerator.MetricsService{
						Pipelines: map[string]*confgenerator.Pipeline{
							"otlp": {
								ReceiverIDs: []string{"otlp"},
							},
						},
					},
				},
			},
			enabledReceivers: self_metrics.EnabledReceivers{
				MetricsReceiverCountsByType: map[string]int{"otlp": 1},
				LogsReceiverCountsByType:    map[string]int{"otlp": 1},
			},
			experimentalFeatures: "otlp_logging",
		},
		{
			name: "multiple_and_unreferenced_receivers",
			config: &confgenerator.UnifiedConfig{
				Logging: &confgenerator.Logging{
					Receivers: map[string]confgenerator.LoggingReceiver{
						"files_1":       confgenerator.LoggingReceiverFiles{},
						"files_2":       confgenerator.LoggingReceiverFiles{},
						"unused_syslog": confgenerator.LoggingReceiverSyslog{},
					},
					Service: &confgenerator.LoggingService{
						Pipelines: map[string]*confgenerator.Pipeline{
							"default_pipeline": {
								ReceiverIDs: []string{"files_1"},
							},
							"custom_pipeline": {
								ReceiverIDs: []string{"files_2"},
							},
						},
					},
				},
				Metrics: &confgenerator.Metrics{
					Receivers: map[string]confgenerator.MetricsReceiver{
						"prom_1":             confgenerator.PrometheusMetrics{},
						"prom_2":             confgenerator.PrometheusMetrics{},
						"unused_hostmetrics": confgenerator.MetricsReceiverHostmetrics{},
					},
					Service: &confgenerator.MetricsService{
						Pipelines: map[string]*confgenerator.Pipeline{
							"default_pipeline": {
								ReceiverIDs: []string{"prom_1", "prom_2"},
							},
						},
					},
				},
			},
			enabledReceivers: self_metrics.EnabledReceivers{
				MetricsReceiverCountsByType: map[string]int{"prometheus": 2},
				LogsReceiverCountsByType:    map[string]int{"files": 2},
			},
		},
		{
			name: "empty_pipelines",
			config: &confgenerator.UnifiedConfig{
				Logging: &confgenerator.Logging{
					Receivers: map[string]confgenerator.LoggingReceiver{
						"files": confgenerator.LoggingReceiverFiles{},
					},
					Service: &confgenerator.LoggingService{
						Pipelines: map[string]*confgenerator.Pipeline{
							"default_pipeline": {
								ReceiverIDs: []string{},
							},
						},
					},
				},
				Metrics: &confgenerator.Metrics{
					Receivers: map[string]confgenerator.MetricsReceiver{
						"hostmetrics": confgenerator.MetricsReceiverHostmetrics{},
					},
					Service: &confgenerator.MetricsService{
						Pipelines: map[string]*confgenerator.Pipeline{
							"default_pipeline": {
								ReceiverIDs: []string{},
							},
						},
					},
				},
			},
			enabledReceivers: self_metrics.EnabledReceivers{
				MetricsReceiverCountsByType: map[string]int{},
				LogsReceiverCountsByType:    map[string]int{},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := experiments.ContextWithExperiments(context.Background(), experiments.ParseExperimentalFeatures(test.experimentalFeatures))
			eR, err := self_metrics.CountEnabledReceivers(ctx, test.config)
			assert.NilError(t, err)
			assert.DeepEqual(t, eR, test.enabledReceivers)
		})
	}
}

func TestRegisterSelfMetrics(t *testing.T) {
	pl := platform.Platform{
		Type: platform.Linux,
		HostInfo: &host.InfoStat{
			OS:              "linux",
			Platform:        "linux_platform",
			PlatformVersion: "linux_platform_version",
		},
		TestGCEResourceOverride: resourcedetector.GCEResource{
			Project: "test-project",
		},
	}
	ctx := pl.TestContext(context.Background())
	userUc := &confgenerator.UnifiedConfig{}
	mergedUc := confgenerator.BuiltInConfStructs["linux"]

	err := self_metrics.SetSelfMetrics(ctx, userUc, mergedUc)
	assert.NilError(t, err)

	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	err = self_metrics.RegisterSelfMetrics(mp)
	assert.NilError(t, err)

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	assert.NilError(t, err)
	assert.Assert(t, len(rm.ScopeMetrics) == 1)
	assert.Assert(t, len(rm.ScopeMetrics[0].Metrics) == 2)

	expectedEnabledReceivers := metricdata.Metrics{
		Name: "ops_agent_enabled_receivers",
		Data: metricdata.Gauge[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{
					Attributes: attribute.NewSet(
						attribute.String("telemetry_type", "metrics"),
						attribute.String("receiver_type", "hostmetrics"),
					),
					Value: 1,
				},
				{
					Attributes: attribute.NewSet(
						attribute.String("telemetry_type", "logs"),
						attribute.String("receiver_type", "files"),
					),
					Value: 1,
				},
			},
		},
	}
	metricdatatest.AssertEqual(t, expectedEnabledReceivers, rm.ScopeMetrics[0].Metrics[0], metricdatatest.IgnoreTimestamp())

	expectedFeatureTracking := metricdata.Metrics{
		Name: "ops_agent_feature_tracking",
		Data: metricdata.Gauge[int64]{
			DataPoints: []metricdata.DataPoint[int64]{
				{
					Attributes: attribute.NewSet(
						attribute.String("module", "logging"),
						attribute.String("feature", "service:pipelines"),
						attribute.String("key", "default_pipeline_overridden"),
						attribute.String("value", "false"),
					),
					Value: 1,
				},
				{
					Attributes: attribute.NewSet(
						attribute.String("module", "metrics"),
						attribute.String("feature", "service:pipelines"),
						attribute.String("key", "default_pipeline_overridden"),
						attribute.String("value", "false"),
					),
					Value: 1,
				},
				{
					Attributes: attribute.NewSet(
						attribute.String("module", "global"),
						attribute.String("feature", "default:self_log"),
						attribute.String("key", "default_self_log_file_collection"),
						attribute.String("value", "true"),
					),
					Value: 1,
				},
			},
		},
	}
	metricdatatest.AssertEqual(t, expectedFeatureTracking, rm.ScopeMetrics[0].Metrics[1], metricdatatest.IgnoreTimestamp())
}
