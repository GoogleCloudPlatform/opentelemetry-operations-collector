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

package opsagentconfprovider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

func TestCombinedValidate(t *testing.T) {
	testCases := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name: "combined_receiver_id_with_lib_prefix",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"lib:otlp": {Type: "otlp"},
					},
				},
				Traces: &Traces{},
			},
			wantErr: `combined receiver ID "lib:otlp" cannot start with "lib:"`,
		},
		{
			name: "unsupported_combined_receiver_type",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "jaeger"},
					},
				},
				Traces: &Traces{},
			},
			wantErr: `combined receiver "otlp" with type "jaeger" is not supported`,
		},
		{
			name: "invalid_grpc_endpoint_missing_port",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "otlp", GRPCEndpoint: "127.0.0.1"},
					},
				},
				Traces: &Traces{},
			},
			wantErr: `combined receiver "otlp" has invalid grpc_endpoint "127.0.0.1"`,
		},
		{
			name: "invalid_grpc_endpoint_missing_host",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "otlp", GRPCEndpoint: ":4317"},
					},
				},
				Traces: &Traces{},
			},
			wantErr: `combined receiver "otlp" has invalid grpc_endpoint ":4317"`,
		},
		{
			name: "invalid_grpc_endpoint_bad_port",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "otlp", GRPCEndpoint: "127.0.0.1:99999"},
					},
				},
				Traces: &Traces{},
			},
			wantErr: `combined receiver "otlp" has invalid grpc_endpoint "127.0.0.1:99999"`,
		},
		{
			name: "invalid_metrics_mode",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "otlp", MetricsMode: "google"},
					},
				},
				Traces: &Traces{},
			},
			wantErr: `combined receiver "otlp" has invalid metrics_mode "google": must be one of [googlecloudmonitoring googlemanagedprometheus]`,
		},
		{
			name: "combined_receiver_missing_traces_section",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "otlp"},
					},
				},
			},
			wantErr: `combined receiver "otlp" found with no traces section`,
		},
		{
			name: "combined_receiver_same_name_as_metrics_receiver",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"hostmetrics": {Type: "otlp"},
					},
				},
				Traces: &Traces{},
			},
			wantErr: `metrics receiver "hostmetrics" has the same name as combined receiver "hostmetrics"`,
		},
		{
			name: "otlp_gmp_incompatible_with_processors",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "otlp"},
					},
				},
				Metrics: &Metrics{
					Service: &MetricsService{
						Pipelines: map[string]*Pipeline{
							"otlp_pipe": {
								ReceiverIDs:  []string{"otlp"},
								ProcessorIDs: []string{"metrics_filter"},
							},
						},
					},
				},
				Traces: &Traces{},
			},
			wantErr: `otlp receiver is incompatible with Ops Agent processors`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.config.generateOtelConfig(context.Background(), t.TempDir())
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestRetrieveOTLPPipelines(t *testing.T) {
	t.Run("gmp_mode_with_multiple_pipelines", func(t *testing.T) {
		configYAML := `combined:
  receivers:
    otlp:
      type: otlp
metrics:
  service:
    pipelines:
      otlp:
        receivers: [otlp]
      otlp2:
        receivers: [otlp]
traces: {}
`
		configFile, _ := writeTestConfig(t, configYAML)
		p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
		retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
		require.NoError(t, err)

		conf, err := retrieved.AsConf()
		require.NoError(t, err)

		assert.Equal(t, "0.0.0.0:4317", conf.Get("receivers::otlp/otlp::protocols::grpc::endpoint"))
		assert.False(t, conf.IsSet("exporters::otlp_grpc/otlp_traces"))
		assert.Equal(t, false, conf.Get("processors::resourcedetection/otlp_0::override"))

		expectedGMPProcs := []string{
			"resourcedetection/otlp_0",
			"transform/otlp_1",
			"groupbyattrs/otlp_2",
			"transform/otlp_3",
			"metricstransform/otlp_4",
			"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
			"batch/otlp_grpc/otlp_metrics_metrics_2",
		}
		assert.Equal(t, expectedGMPProcs, conf.Get("service::pipelines::metrics/otlp_otlp::processors"))
		assert.Equal(t, expectedGMPProcs, conf.Get("service::pipelines::metrics/otlp2_otlp::processors"))
	})

	t.Run("gcm_mode_with_processor", func(t *testing.T) {
		configYAML := `combined:
  receivers:
    otlp_recv:
      type: otlp
      grpc_endpoint: 127.0.0.1:4317
      metrics_mode: googlecloudmonitoring
metrics:
  processors:
    drop_foo:
      type: exclude_metrics
      metrics_pattern:
        - workload.googleapis.com/foo/*
  service:
    pipelines:
      otlp_pipe:
        receivers: [otlp_recv]
        processors: [drop_foo]
traces:
  service:
    pipelines: {}
`
		configFile, _ := writeTestConfig(t, configYAML)
		p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
		retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
		require.NoError(t, err)

		conf, err := retrieved.AsConf()
		require.NoError(t, err)

		assert.Equal(t, "127.0.0.1:4317", conf.Get("receivers::otlp/otlp__recv::protocols::grpc::endpoint"))
		assert.False(t, conf.IsSet("exporters::otlp_grpc/otlp_traces"))
		assert.False(t, conf.IsSet("processors::batch/otlp_grpc/otlp_traces_traces_1"))
		assert.True(t, conf.IsSet("processors::resourcedetection/_global_1"))

		assert.Equal(t, []string{
			"metricstransform/otlp__recv_0",
			"filter/otlp__pipe_otlp__recv_0",
			"resourcedetection/_global_1",
			"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
			"batch/otlp_grpc/otlp_metrics_metrics_2",
		}, conf.Get("service::pipelines::metrics/otlp__pipe_otlp__recv::processors"))
	})
}

func TestOTLPGMPTransformation(t *testing.T) {
	configYAML := `combined:
  receivers:
    otlp:
      type: otlp
      metrics_mode: googlemanagedprometheus
metrics:
  service:
    pipelines:
      otlp:
        receivers: [otlp]
traces: {}
`
	// Execute transform/otlp_1, groupbyattrs/otlp_2, transform/otlp_3, metricstransform/otlp_4
	// (skipping resourcedetection/otlp_0 in unit tests to supply deterministic GCE resource attributes).
	chain, sink := buildProcessorChainWithConfig(t, configYAML, []string{
		"transform/otlp_1",
		"groupbyattrs/otlp_2",
		"transform/otlp_3",
		"metricstransform/otlp_4",
	})

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	resAttrs := rm.Resource().Attributes()
	resAttrs.PutStr("cloud.platform", "gcp_compute_engine")
	resAttrs.PutStr("cloud.availability_zone", "us-central1-a")
	resAttrs.PutStr("host.id", "987654321")
	resAttrs.PutStr("host.name", "gce-vm-1")
	resAttrs.PutStr("host.type", "e2-medium")

	sm := rm.ScopeMetrics().AppendEmpty()

	// 1. Standard gauge metric
	mGauge := sm.Metrics().AppendEmpty()
	mGauge.SetName("http_requests_active")
	dpGauge := mGauge.SetEmptyGauge().DataPoints().AppendEmpty()
	dpGauge.SetIntValue(12)

	// 2. Prometheus unknown type metric (metadata prometheus.type = "unknown")
	mUnknown := sm.Metrics().AppendEmpty()
	mUnknown.SetName("custom_untyped_total")
	mUnknown.Metadata().PutStr("prometheus.type", "unknown")
	dpUnknown := mUnknown.SetEmptyGauge().DataPoints().AppendEmpty()
	dpUnknown.SetDoubleValue(42.5)

	require.NoError(t, chain.ConsumeMetrics(context.Background(), md))

	all := sink.AllMetrics()
	require.Len(t, all, 1)
	require.Equal(t, 1, all[0].ResourceMetrics().Len())

	outRM := all[0].ResourceMetrics().At(0)
	outResAttrs := outRM.Resource().Attributes()

	// groupbyattrs/otlp_2 moves location, namespace, and cluster to Resource attributes.
	locVal, ok := outResAttrs.Get("location")
	require.True(t, ok)
	assert.Equal(t, "us-central1-a", locVal.Str())

	nsVal, ok := outResAttrs.Get("namespace")
	require.True(t, ok)
	assert.Equal(t, "987654321/gce-vm-1", nsVal.Str())

	clusterVal, ok := outResAttrs.Get("cluster")
	require.True(t, ok)
	assert.Equal(t, "__gce__", clusterVal.Str())

	outSM := outRM.ScopeMetrics().At(0)
	// http_requests_active (1) + custom_untyped_total (original gauge + converted sum = 2) = 3 metrics.
	require.Equal(t, 3, outSM.Metrics().Len())

	var sawActiveGauge, sawUnknownGauge, sawUnknownSum bool
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		switch m.Name() {
		case "prometheus.googleapis.com/http_requests_active":
			sawActiveGauge = true
			dp := m.Gauge().DataPoints().At(0)
			instVal, hasInst := dp.Attributes().Get("instance_name")
			require.True(t, hasInst)
			assert.Equal(t, "gce-vm-1", instVal.Str())
			machVal, hasMach := dp.Attributes().Get("machine_type")
			require.True(t, hasMach)
			assert.Equal(t, "e2-medium", machVal.Str())
		case "prometheus.googleapis.com/custom_untyped_total":
			if m.Type() == pmetric.MetricTypeGauge {
				sawUnknownGauge = true
				assert.InDelta(t, 42.5, m.Gauge().DataPoints().At(0).DoubleValue(), 1e-6)
			} else if m.Type() == pmetric.MetricTypeSum {
				sawUnknownSum = true
				assert.True(t, m.Sum().IsMonotonic())
				assert.Equal(t, pmetric.AggregationTemporalityCumulative, m.Sum().AggregationTemporality())
				assert.InDelta(t, 42.5, m.Sum().DataPoints().At(0).DoubleValue(), 1e-6)
			}
		}
	}
	assert.True(t, sawActiveGauge)
	assert.True(t, sawUnknownGauge)
	assert.True(t, sawUnknownSum)
}

func TestOTLPGCMTransformation(t *testing.T) {
	configYAML := `combined:
  receivers:
    otlp:
      type: otlp
      metrics_mode: googlecloudmonitoring
metrics:
  processors:
    exclude_secret:
      type: exclude_metrics
      metrics_pattern:
        - workload.googleapis.com/secret_*
  service:
    pipelines:
      otlp_pipe:
        receivers: [otlp]
        processors: [exclude_secret]
traces: {}
`
	chain, sink := buildProcessorChainWithConfig(t, configYAML, []string{
		"metricstransform/otlp_0",
		"filter/otlp__pipe_otlp_0",
	})

	md := pmetric.NewMetrics()
	sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()

	inputNames := []string{
		"my_app_requests",
		"secret_token_count",
		"custom.googleapis.com/my_metric",
		"kubernetes.io/container/cpu/core_usage_time",
		"istio.io/service/server/request_count",
		"knative.dev/serving/revision/request_count",
	}
	for _, name := range inputNames {
		m := sm.Metrics().AppendEmpty()
		m.SetName(name)
		m.SetEmptyGauge().DataPoints().AppendEmpty().SetIntValue(1)
	}

	require.NoError(t, chain.ConsumeMetrics(context.Background(), md))

	all := sink.AllMetrics()
	require.Len(t, all, 1)
	outSM := all[0].ResourceMetrics().At(0).ScopeMetrics().At(0)

	gotNames := make([]string, 0, outSM.Metrics().Len())
	for i := 0; i < outSM.Metrics().Len(); i++ {
		gotNames = append(gotNames, outSM.Metrics().At(i).Name())
	}

	assert.Equal(t, []string{
		"workload.googleapis.com/my_app_requests",
		"custom.googleapis.com/my_metric",
		"kubernetes.io/container/cpu/core_usage_time",
		"istio.io/service/server/request_count",
		"knative.dev/serving/revision/request_count",
	}, gotNames)
}
