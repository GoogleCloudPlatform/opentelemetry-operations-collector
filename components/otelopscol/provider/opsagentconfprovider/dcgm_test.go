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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

func TestDCGMValidate(t *testing.T) {
	testCases := []struct {
		name      string
		isWindows bool
		config    string
		wantErr   string
	}{
		{
			name:      "dcgm_rejected_on_windows",
			isWindows: true,
			config: `metrics:
  receivers:
    dcgm:
      type: dcgm
`,
			wantErr: `metrics receiver "dcgm" with type "dcgm" is not supported`,
		},
		{
			name:      "dcgm_with_config_rejected",
			isWindows: false,
			config: `metrics:
  receivers:
    dcgm:
      type: dcgm
      config:
        foo: bar
`,
			wantErr: `metrics receiver "dcgm" with type "dcgm" does not support config`,
		},
		{
			name:      "dcgm_collection_interval_too_short",
			isWindows: false,
			config: `metrics:
  receivers:
    dcgm:
      type: dcgm
      collection_interval: 5s
`,
			wantErr: `metrics receiver "dcgm" has invalid collection_interval "5s"`,
		},
		{
			name:      "dcgm_invalid_receiver_version",
			isWindows: false,
			config: `metrics:
  receivers:
    dcgm:
      type: dcgm
      receiver_version: 3
`,
			wantErr: `metrics receiver "dcgm" has invalid receiver_version "3": must be one of [1 2]`,
		},
		{
			name:      "dcgm_invalid_endpoint_missing_port",
			isWindows: false,
			config: `metrics:
  receivers:
    dcgm:
      type: dcgm
      endpoint: localhost
`,
			wantErr: `metrics receiver "dcgm" has invalid endpoint "localhost": must be a valid host:port`,
		},
		{
			name:      "dcgm_invalid_endpoint_empty_host",
			isWindows: false,
			config: `metrics:
  receivers:
    dcgm:
      type: dcgm
      endpoint: ":5555"
`,
			wantErr: `metrics receiver "dcgm" has invalid endpoint ":5555": must be a valid host:port`,
		},
		{
			name:      "dcgm_invalid_endpoint_out_of_range_port",
			isWindows: false,
			config: `metrics:
  receivers:
    dcgm:
      type: dcgm
      endpoint: "localhost:70000"
`,
			wantErr: `metrics receiver "dcgm" has invalid endpoint "localhost:70000": must be a valid host:port`,
		},
		{
			name:      "hostmetrics_with_endpoint_rejected",
			isWindows: false,
			config: `metrics:
  receivers:
    hostmetrics:
      type: hostmetrics
      endpoint: "localhost:5555"
`,
			wantErr: `metrics receiver "hostmetrics" with type "hostmetrics" does not support endpoint`,
		},
		{
			name:      "iis_with_endpoint_rejected",
			isWindows: true,
			config: `metrics:
  receivers:
    iis:
      type: iis
      endpoint: "localhost:5555"
`,
			wantErr: `metrics receiver "iis" with type "iis" does not support endpoint`,
		},
		{
			name:      "prometheus_with_endpoint_rejected",
			isWindows: false,
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      endpoint: "localhost:5555"
`,
			wantErr: `metrics receiver "prometheus" with type "prometheus" does not support endpoint`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configFile, _ := writeTestConfig(t, tc.config)
			osName := "linux"
			if tc.isWindows {
				osName = "windows"
			}
			p := &provider{
				logger:   zap.NewNop(),
				hostInfo: hostInfo{OS: osName},
			}
			_, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestRetrieveDCGMPipelines(t *testing.T) {
	linuxHost := hostInfo{OS: "linux"}
	userYAML := `metrics:
  receivers:
    dcgm:
      type: dcgm
    dcgm_v2:
      type: dcgm
      receiver_version: 2
      collection_interval: 30s
      endpoint: 127.0.0.1:5556
  service:
    pipelines:
      dcgm:
        receivers:
          - dcgm
          - dcgm_v2
`
	configFile, _ := writeTestConfig(t, userYAML)
	p := &provider{logger: zap.NewNop(), hostInfo: linuxHost}
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)

	// dcgm v1 receiver config and pipeline
	assert.Equal(t, "60s", conf.Get("receivers::dcgm/dcgm::collection_interval"))
	assert.Equal(t, "localhost:5555", conf.Get("receivers::dcgm/dcgm::endpoint"))
	assert.Equal(t, true, conf.Get("receivers::dcgm/dcgm::metrics::gpu.dcgm.sm.utilization::enabled"))
	assert.Equal(t, false, conf.Get("receivers::dcgm/dcgm::metrics::gpu.dcgm.utilization::enabled"))
	assert.Equal(t, []string{"dcgm/dcgm"}, conf.Get("service::pipelines::metrics/dcgm_dcgm::receivers"))
	assert.Equal(t, []string{
		"metricstransform/dcgm_0",
		"cumulativetodelta/dcgm_1",
		"deltatorate/dcgm_2",
		"metricstransform/dcgm_3",
		"metricstransform/dcgm_4",
		"transform/dcgm_5",
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}, conf.Get("service::pipelines::metrics/dcgm_dcgm::processors"))

	// dcgm v2 receiver config and pipeline
	assert.Equal(t, "30s", conf.Get("receivers::dcgm/dcgm__v2::collection_interval"))
	assert.Equal(t, "127.0.0.1:5556", conf.Get("receivers::dcgm/dcgm__v2::endpoint"))
	assert.False(t, conf.IsSet("receivers::dcgm/dcgm__v2::metrics"))
	assert.Equal(t, []string{"dcgm/dcgm__v2"}, conf.Get("service::pipelines::metrics/dcgm_dcgm__v2::receivers"))
	assert.Equal(t, []string{
		"metricstransform/dcgm__v2_0",
		"metricstransform/dcgm__v2_1",
		"transform/dcgm__v2_2",
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}, conf.Get("service::pipelines::metrics/dcgm_dcgm__v2::processors"))
}

func TestDCGMV1MetricsTransformation(t *testing.T) {
	linuxHost := hostInfo{OS: "linux"}
	userYAML := `metrics:
  receivers:
    dcgm:
      type: dcgm
  service:
    pipelines:
      dcgm:
        receivers: [dcgm]
`
	procIDs := registerProcessors(map[string]any{}, (MetricsReceiver{Type: "dcgm"}).dcgmProcessors("dcgm"))
	chain, sink := buildProcessorChainForHost(t, userYAML, linuxHost, procIDs)

	ts0 := pcommon.NewTimestampFromTime(time.Unix(900, 0))
	ts1 := pcommon.NewTimestampFromTime(time.Unix(1000, 0))
	ts2 := pcommon.NewTimestampFromTime(time.Unix(1060, 0))

	buildScrape := func(ts pcommon.Timestamp, nvlinkRx, nvlinkTx, pcieRx int64) pmetric.Metrics {
		md := pmetric.NewMetrics()
		rm := md.ResourceMetrics().AppendEmpty()
		rm.Resource().Attributes().PutStr("gpu.model", "NVIDIA A100")
		rm.Resource().Attributes().PutStr("gpu.number", "0")
		rm.Resource().Attributes().PutStr("gpu.uuid", "GPU-1234")

		sm := rm.ScopeMetrics().AppendEmpty()

		addGauge := func(name string, val float64, attrs map[string]string) {
			m := sm.Metrics().AppendEmpty()
			m.SetName(name)
			dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
			dp.SetTimestamp(ts)
			dp.SetDoubleValue(val)
			for k, v := range attrs {
				dp.Attributes().PutStr(k, v)
			}
		}

		addGauge("gpu.dcgm.memory.bandwidth_utilization", 0.45, nil)
		addGauge("gpu.dcgm.pipe.utilization", 0.80, map[string]string{"gpu.pipe": "tensor"})
		addGauge("gpu.dcgm.sm.occupancy", 0.65, nil)
		addGauge("gpu.dcgm.sm.utilization", 0.75, nil)

		mNvlink := sm.Metrics().AppendEmpty()
		mNvlink.SetName("gpu.dcgm.nvlink.io")
		sNvlink := mNvlink.SetEmptySum()
		sNvlink.SetIsMonotonic(true)
		sNvlink.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
		dpRx := sNvlink.DataPoints().AppendEmpty()
		dpRx.SetStartTimestamp(ts0)
		dpRx.SetTimestamp(ts)
		dpRx.SetIntValue(nvlinkRx)
		dpRx.Attributes().PutStr("network.io.direction", "receive")
		dpTx := sNvlink.DataPoints().AppendEmpty()
		dpTx.SetStartTimestamp(ts0)
		dpTx.SetTimestamp(ts)
		dpTx.SetIntValue(nvlinkTx)
		dpTx.Attributes().PutStr("network.io.direction", "transmit")

		mPCIe := sm.Metrics().AppendEmpty()
		mPCIe.SetName("gpu.dcgm.pcie.io")
		sPCIe := mPCIe.SetEmptySum()
		sPCIe.SetIsMonotonic(true)
		sPCIe.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
		dpPCIe := sPCIe.DataPoints().AppendEmpty()
		dpPCIe.SetStartTimestamp(ts0)
		dpPCIe.SetTimestamp(ts)
		dpPCIe.SetIntValue(pcieRx)
		dpPCIe.Attributes().PutStr("network.io.direction", "receive")

		return md
	}

	require.NoError(t, chain.ConsumeMetrics(context.Background(), buildScrape(ts1, 1000, 2000, 500)))
	// 60 seconds later: +60000 bytes rx (1000 B/s), +120000 bytes tx (2000 B/s), +30000 bytes pcie rx (500 B/s)
	require.NoError(t, chain.ConsumeMetrics(context.Background(), buildScrape(ts2, 61000, 122000, 30500)))

	all := sink.AllMetrics()
	require.NotEmpty(t, all)
	lastBatch := all[len(all)-1]
	outSM := lastBatch.ResourceMetrics().At(0).ScopeMetrics().At(0)
	assert.Equal(t, "agent.googleapis.com/dcgm", outSM.Scope().Name())
	assert.Equal(t, "1.0", outSM.Scope().Version())

	gotMetrics := make(map[string]pmetric.Metric)
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		gotMetrics[m.Name()] = m
	}

	require.Contains(t, gotMetrics, "workload.googleapis.com/dcgm.gpu.profiling.dram_utilization")
	dramDP := gotMetrics["workload.googleapis.com/dcgm.gpu.profiling.dram_utilization"].Gauge().DataPoints().At(0)
	assert.InDelta(t, 0.45, dramDP.DoubleValue(), 1e-6)
	modelAttr, _ := dramDP.Attributes().Get("model")
	assert.Equal(t, "NVIDIA A100", modelAttr.Str())
	gpuNumAttr, _ := dramDP.Attributes().Get("gpu_number")
	assert.Equal(t, "0", gpuNumAttr.Str())
	uuidAttr, _ := dramDP.Attributes().Get("uuid")
	assert.Equal(t, "GPU-1234", uuidAttr.Str())

	require.Contains(t, gotMetrics, "workload.googleapis.com/dcgm.gpu.profiling.pipe_utilization")
	pipeDP := gotMetrics["workload.googleapis.com/dcgm.gpu.profiling.pipe_utilization"].Gauge().DataPoints().At(0)
	pipeAttr, _ := pipeDP.Attributes().Get("pipe")
	assert.Equal(t, "tensor", pipeAttr.Str())

	require.Contains(t, gotMetrics, "workload.googleapis.com/dcgm.gpu.profiling.nvlink_traffic_rate")
	nvlinkMetric := gotMetrics["workload.googleapis.com/dcgm.gpu.profiling.nvlink_traffic_rate"]
	assert.Equal(t, pmetric.MetricTypeGauge, nvlinkMetric.Type())
	nvlinkRates := map[string]int64{}
	for i := 0; i < nvlinkMetric.Gauge().DataPoints().Len(); i++ {
		dp := nvlinkMetric.Gauge().DataPoints().At(i)
		dir, _ := dp.Attributes().Get("direction")
		nvlinkRates[dir.Str()] = dp.IntValue()
	}
	assert.Equal(t, map[string]int64{"rx": 1000, "tx": 2000}, nvlinkRates)

	require.Contains(t, gotMetrics, "workload.googleapis.com/dcgm.gpu.profiling.pcie_traffic_rate")
	pcieDP := gotMetrics["workload.googleapis.com/dcgm.gpu.profiling.pcie_traffic_rate"].Gauge().DataPoints().At(0)
	assert.Equal(t, int64(500), pcieDP.IntValue())
}

func TestDCGMV2MetricsTransformation(t *testing.T) {
	linuxHost := hostInfo{OS: "linux"}
	userYAML := `metrics:
  receivers:
    dcgm_v2:
      type: dcgm
      receiver_version: 2
  service:
    pipelines:
      dcgm:
        receivers: [dcgm_v2]
`
	procIDs := registerProcessors(map[string]any{}, (MetricsReceiver{Type: "dcgm", ReceiverVersion: "2"}).dcgmProcessors("dcgm__v2"))
	chain, sink := buildProcessorChainForHost(t, userYAML, linuxHost, procIDs)

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("gpu.model", "NVIDIA H100")
	rm.Resource().Attributes().PutStr("gpu.number", "1")
	rm.Resource().Attributes().PutStr("gpu.uuid", "GPU-5678")

	sm := rm.ScopeMetrics().AppendEmpty()

	addMetricWithAttr := func(name, attrKey, attrVal string) {
		m := sm.Metrics().AppendEmpty()
		m.SetName(name)
		dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
		dp.SetIntValue(10)
		dp.Attributes().PutStr(attrKey, attrVal)
	}

	addMetricWithAttr("gpu.dcgm.pipe.utilization", "gpu.pipe", "fp16")
	addMetricWithAttr("gpu.dcgm.memory.bytes_used", "gpu.memory.state", "used")
	addMetricWithAttr("gpu.dcgm.nvlink.io", "network.io.direction", "receive")
	addMetricWithAttr("gpu.dcgm.pcie.io", "network.io.direction", "transmit")
	addMetricWithAttr("gpu.dcgm.clock.throttle_duration.time", "gpu.clock.violation", "thermal")
	addMetricWithAttr("gpu.dcgm.ecc_errors", "gpu.error.type", "single_bit")
	addMetricWithAttr("gpu.dcgm.xid_errors", "gpu.error.xid", "31")

	require.NoError(t, chain.ConsumeMetrics(context.Background(), md))

	all := sink.AllMetrics()
	require.Len(t, all, 1)
	outSM := all[0].ResourceMetrics().At(0).ScopeMetrics().At(0)
	assert.Equal(t, "agent.googleapis.com/dcgm", outSM.Scope().Name())
	assert.Equal(t, "2.0", outSM.Scope().Version())

	gotMetrics := make(map[string]pmetric.Metric)
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		gotMetrics[m.Name()] = m
	}

	assertAttr := func(metricName, key, want string) {
		require.Contains(t, gotMetrics, metricName)
		dp := gotMetrics[metricName].Gauge().DataPoints().At(0)
		val, ok := dp.Attributes().Get(key)
		require.True(t, ok, "missing %s on %s", key, metricName)
		assert.Equal(t, want, val.Str())
		modelAttr, _ := dp.Attributes().Get("model")
		assert.Equal(t, "NVIDIA H100", modelAttr.Str())
	}

	assertAttr("workload.googleapis.com/gpu.dcgm.pipe.utilization", "pipe", "fp16")
	assertAttr("workload.googleapis.com/gpu.dcgm.memory.bytes_used", "state", "used")
	assertAttr("workload.googleapis.com/gpu.dcgm.nvlink.io", "direction", "receive")
	assertAttr("workload.googleapis.com/gpu.dcgm.pcie.io", "direction", "transmit")
	assertAttr("workload.googleapis.com/gpu.dcgm.clock.throttle_duration.time", "violation", "thermal")
	assertAttr("workload.googleapis.com/gpu.dcgm.ecc_errors", "error_type", "single_bit")
	assertAttr("workload.googleapis.com/gpu.dcgm.xid_errors", "xid", "31")
}
