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
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

func TestMergeMetricsConfig(t *testing.T) {
	t.Run("nil_user_config_returns_defaults", func(t *testing.T) {
		merged := mergeMetricsConfig(nil)
		assert.Equal(t, MetricsReceiver{
			Type:               "hostmetrics",
			CollectionInterval: "60s",
		}, merged.Receivers["hostmetrics"])
		assert.Equal(t, MetricsProcessor{
			Type: "exclude_metrics",
		}, merged.Processors["metrics_filter"])
		require.NotNil(t, merged.Service)
		assert.Equal(t, &Pipeline{
			ReceiverIDs:  []string{"hostmetrics"},
			ProcessorIDs: []string{"metrics_filter"},
		}, merged.Service.Pipelines["default_pipeline"])
	})

	t.Run("override_builtin_receiver_and_processor", func(t *testing.T) {
		merged := mergeMetricsConfig(&Metrics{
			Receivers: map[string]MetricsReceiver{
				"hostmetrics": {
					Type:               "hostmetrics",
					CollectionInterval: "30s",
				},
			},
			Processors: map[string]MetricsProcessor{
				"metrics_filter": {
					Type:           "exclude_metrics",
					MetricsPattern: []string{"agent.googleapis.com/cpu/*"},
				},
			},
			Service: &MetricsService{
				LogLevel: "debug",
			},
		})
		assert.Equal(t, "30s", merged.Receivers["hostmetrics"].CollectionInterval)
		assert.Equal(t, []string{"agent.googleapis.com/cpu/*"}, merged.Processors["metrics_filter"].MetricsPattern)
		assert.Equal(t, "debug", merged.Service.LogLevel)
		// Built-in default_pipeline remains wired to hostmetrics and metrics_filter.
		assert.Equal(t, &Pipeline{
			ReceiverIDs:  []string{"hostmetrics"},
			ProcessorIDs: []string{"metrics_filter"},
		}, merged.Service.Pipelines["default_pipeline"])
	})

	t.Run("disable_default_pipeline", func(t *testing.T) {
		merged := mergeMetricsConfig(&Metrics{
			Service: &MetricsService{
				LogLevel: "info",
				Pipelines: map[string]*Pipeline{
					"default_pipeline": {
						ReceiverIDs: []string{},
					},
					"empty_pipeline": nil,
				},
			},
		})
		assert.Empty(t, merged.Service.LogLevel)
		assert.Empty(t, merged.Service.Pipelines["default_pipeline"].ReceiverIDs)
		assert.Empty(t, merged.Service.Pipelines["default_pipeline"].ProcessorIDs)
		assert.NotNil(t, merged.Service.Pipelines["empty_pipeline"])
	})
}

func TestMetricsValidate(t *testing.T) {
	require.NoError(t, defaultMetricsConfig().validate())

	testCases := []struct {
		name    string
		mutate  func(m *Metrics)
		wantErr string
	}{
		{
			name: "receiver_id_with_lib_prefix",
			mutate: func(m *Metrics) {
				m.Receivers["lib:host"] = MetricsReceiver{Type: "hostmetrics"}
			},
			wantErr: `metrics receiver ID "lib:host" cannot start with "lib:"`,
		},
		{
			name: "unsupported_receiver_type",
			mutate: func(m *Metrics) {
				m.Receivers["custom"] = MetricsReceiver{Type: "unknown"}
			},
			wantErr: `metrics receiver "custom" with type "unknown" is not supported`,
		},
		{
			name: "collection_interval_too_short",
			mutate: func(m *Metrics) {
				m.Receivers["hostmetrics"] = MetricsReceiver{Type: "hostmetrics", CollectionInterval: "5s"}
			},
			wantErr: `metrics receiver "hostmetrics" has invalid collection_interval "5s"`,
		},
		{
			name: "collection_interval_unparseable",
			mutate: func(m *Metrics) {
				m.Receivers["hostmetrics"] = MetricsReceiver{Type: "hostmetrics", CollectionInterval: "not-a-duration"}
			},
			wantErr: `metrics receiver "hostmetrics" has invalid collection_interval "not-a-duration"`,
		},
		{
			name: "processor_id_with_lib_prefix",
			mutate: func(m *Metrics) {
				m.Processors["lib:filter"] = MetricsProcessor{Type: "exclude_metrics"}
			},
			wantErr: `metrics processor ID "lib:filter" cannot start with "lib:"`,
		},
		{
			name: "unsupported_processor_type",
			mutate: func(m *Metrics) {
				m.Processors["custom"] = MetricsProcessor{Type: "modify_fields"}
			},
			wantErr: `metrics processor "custom" with type "modify_fields" is not supported`,
		},
		{
			name: "invalid_log_level",
			mutate: func(m *Metrics) {
				m.Service.LogLevel = "trace"
			},
			wantErr: `metrics service has invalid log_level "trace"`,
		},
		{
			name: "pipeline_id_with_lib_prefix",
			mutate: func(m *Metrics) {
				m.Service.Pipelines["lib:pipe"] = &Pipeline{}
			},
			wantErr: `metrics pipeline ID "lib:pipe" cannot start with "lib:"`,
		},
		{
			name: "undefined_receiver_in_pipeline",
			mutate: func(m *Metrics) {
				m.Service.Pipelines["default_pipeline"].ReceiverIDs = []string{"missing_receiver"}
			},
			wantErr: `metrics receiver "missing_receiver" from pipeline "default_pipeline" is not defined`,
		},
		{
			name: "undefined_processor_in_pipeline",
			mutate: func(m *Metrics) {
				m.Service.Pipelines["default_pipeline"].ProcessorIDs = []string{"missing_processor"}
			},
			wantErr: `metrics processor "missing_processor" from pipeline "default_pipeline" is not defined`,
		},
		{
			name: "more_than_one_hostmetrics_receiver_in_pipeline",
			mutate: func(m *Metrics) {
				m.Receivers["hostmetrics_2"] = MetricsReceiver{Type: "hostmetrics"}
				m.Service.Pipelines["default_pipeline"].ReceiverIDs = []string{"hostmetrics", "hostmetrics_2"}
			},
			wantErr: `at most one metrics receiver with type "hostmetrics" is allowed in pipeline "default_pipeline"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := defaultMetricsConfig()
			tc.mutate(m)
			err := m.validate()
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestGlobToRegex(t *testing.T) {
	assert.Equal(t, `^agent\.googleapis\.com/cpu/utilization$`, globToRegex("agent.googleapis.com/cpu/utilization"))
	assert.Equal(t, `^agent\.googleapis\.com/cpu/.*$`, globToRegex("agent.googleapis.com/cpu/*"))
	assert.Equal(t, `^agent\.googleapis\.com/proce.*ses/.*$`, globToRegex("agent.googleapis.com/proce*ses/*"))
	assert.Equal(t, `^agent\.googleapis\.com/x\$y\(z\)\+\?/.*$`, globToRegex("agent.googleapis.com/x$y(z)+?/*"))
}

func TestRetrieveCustomMetricsPipelines(t *testing.T) {
	configYAML := `metrics:
  receivers:
    my_host:
      type: hostmetrics
      collection_interval: 20s
  processors:
    filter_1:
      type: exclude_metrics
      metrics_pattern:
        - agent.googleapis.com/gpu/memory/*
    filter_2:
      type: exclude_metrics
      metrics_pattern:
        - agent.googleapis.com/gpu/processes/*
  service:
    log_level: warn
    pipelines:
      default_pipeline:
        receivers: []
      custom_pipe:
        receivers: [my_host]
        processors: [filter_1, filter_2]
`
	configFile, _ := writeTestConfig(t, configYAML)
	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)

	assert.False(t, conf.IsSet("service::pipelines::metrics/default__pipeline_hostmetrics"))
	assert.True(t, conf.IsSet("service::pipelines::metrics/custom__pipe_my__host"))
	assert.Equal(t, "20s", conf.Get("receivers::hostmetrics/my__host::collection_interval"))
	assert.Equal(t, "debug", conf.Get("service::telemetry::logs::level"))

	assert.Equal(t, []string{`^agent\.googleapis\.com/gpu/memory/.*$`}, conf.Get("processors::filter/custom__pipe_my__host_0::metrics::exclude::metric_names"))
	assert.Equal(t, []string{`^agent\.googleapis\.com/gpu/processes/.*$`}, conf.Get("processors::filter/custom__pipe_my__host_1::metrics::exclude::metric_names"))

	assert.Equal(t, []string{
		"agentmetrics/my__host_0",
		"filter/my__host_1",
		"metricstransform/my__host_2",
		"transform/my__host_3",
		"transform/my__host_4",
		"filter/custom__pipe_my__host_0",
		"filter/custom__pipe_my__host_1",
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}, conf.Get("service::pipelines::metrics/custom__pipe_my__host::processors"))
}

func TestRetrieveUnknownFieldError(t *testing.T) {
	configFile, _ := writeTestConfig(t, "metrics:\n  receivers:\n    hostmetrics:\n      type: hostmetrics\n      unknown_field: true\n")
	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	_, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.ErrorContains(t, err, "field unknown_field not found")
}

func TestExcludeMetricsTransformation(t *testing.T) {
	configYAML := `metrics:
  processors:
    metrics_filter:
      type: exclude_metrics
      metrics_pattern:
        - agent.googleapis.com/cpu/*
        - agent.googleapis.com/processes/disk/*
`
	procIDs := registerProcessors(map[string]any{}, hostmetricsProcessors("hostmetrics", runtime.GOOS == "windows"))
	procIDs = append(procIDs, "filter/default__pipeline_hostmetrics_0")
	chain, sink := buildProcessorChainWithConfig(t, configYAML, procIDs)

	md := pmetric.NewMetrics()
	sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()

	// 1. system.cpu.time -> renamed to agent.googleapis.com/cpu/usage_time -> excluded by agent.googleapis.com/cpu/*
	mCPU := sm.Metrics().AppendEmpty()
	mCPU.SetName("system.cpu.time")
	cpuSum := mCPU.SetEmptySum()
	cpuSum.SetIsMonotonic(true)
	cpuSum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dpCPU := cpuSum.DataPoints().AppendEmpty()
	dpCPU.SetDoubleValue(10.0)
	dpCPU.Attributes().PutStr("cpu", "cpu0")
	dpCPU.Attributes().PutStr("state", "user")

	// 2. process.disk.read_io -> renamed to agent.googleapis.com/processes/disk/read_bytes_count -> excluded by agent.googleapis.com/processes/disk/*
	mProcDisk := sm.Metrics().AppendEmpty()
	mProcDisk.SetName("process.disk.read_io")
	procDiskSum := mProcDisk.SetEmptySum()
	procDiskSum.SetIsMonotonic(true)
	procDiskSum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	procDiskSum.DataPoints().AppendEmpty().SetIntValue(512)

	// 3. system.memory.usage -> renamed to agent.googleapis.com/memory/bytes_used -> kept!
	mMem := sm.Metrics().AppendEmpty()
	mMem.SetName("system.memory.usage")
	memSum := mMem.SetEmptySum()
	memSum.SetIsMonotonic(false)
	memSum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dpMem := memSum.DataPoints().AppendEmpty()
	dpMem.SetIntValue(1024)
	dpMem.Attributes().PutStr("state", "used")

	require.NoError(t, chain.ConsumeMetrics(context.Background(), md))

	all := sink.AllMetrics()
	require.Len(t, all, 1)
	outSM := all[0].ResourceMetrics().At(0).ScopeMetrics().At(0)

	gotNames := make([]string, 0, outSM.Metrics().Len())
	for i := 0; i < outSM.Metrics().Len(); i++ {
		gotNames = append(gotNames, outSM.Metrics().At(i).Name())
	}

	assert.NotContains(t, gotNames, "agent.googleapis.com/cpu/usage_time")
	assert.NotContains(t, gotNames, "agent.googleapis.com/cpu/utilization")
	assert.NotContains(t, gotNames, "agent.googleapis.com/processes/disk/read_bytes_count")
	assert.Contains(t, gotNames, "agent.googleapis.com/memory/bytes_used")
	assert.Contains(t, gotNames, "agent.googleapis.com/memory/percent_used")
}
