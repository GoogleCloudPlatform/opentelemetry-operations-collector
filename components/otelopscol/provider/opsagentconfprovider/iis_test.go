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

func TestIISValidate(t *testing.T) {
	testCases := []struct {
		name      string
		isWindows bool
		config    string
		wantErr   string
	}{
		{
			name:      "iis_rejected_on_linux",
			isWindows: false,
			config: `metrics:
  receivers:
    iis:
      type: iis
`,
			wantErr: `metrics receiver "iis" with type "iis" is not supported`,
		},
		{
			name:      "iis_with_config_rejected",
			isWindows: true,
			config: `metrics:
  receivers:
    iis:
      type: iis
      config:
        foo: bar
`,
			wantErr: `metrics receiver "iis" with type "iis" does not support config`,
		},
		{
			name:      "iis_collection_interval_too_short",
			isWindows: true,
			config: `metrics:
  receivers:
    iis:
      type: iis
      collection_interval: 5s
`,
			wantErr: `metrics receiver "iis" has invalid collection_interval "5s"`,
		},
		{
			name:      "iis_invalid_receiver_version",
			isWindows: true,
			config: `metrics:
  receivers:
    iis:
      type: iis
      receiver_version: 3
`,
			wantErr: `metrics receiver "iis" has invalid receiver_version "3": must be one of [1 2]`,
		},
		{
			name:      "duplicate_iis_receivers_in_same_pipeline_rejected",
			isWindows: true,
			config: `metrics:
  receivers:
    iis_1:
      type: iis
    iis_2:
      type: iis
      receiver_version: 2
  service:
    pipelines:
      custom_pipe:
        receivers: [iis_1, iis_2]
`,
			wantErr: `at most one metrics receiver with type "iis" is allowed in pipeline "custom_pipe"`,
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

func TestRetrieveIISPipelines(t *testing.T) {
	winHost := hostInfo{OS: "windows"}

	t.Run("default_windows_iis_v1_and_duplicate_v2", func(t *testing.T) {
		userYAML := `metrics:
  receivers:
    iis_v2:
      type: iis
      receiver_version: 2
  service:
    pipelines:
      iispipeline:
        receivers:
          - iis_v2
`
		configFile, _ := writeTestConfig(t, userYAML)
		p := &provider{logger: zap.NewNop(), hostInfo: winHost}
		retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
		require.NoError(t, err)

		conf, err := retrieved.AsConf()
		require.NoError(t, err)

		// Default iis v1 uses windowsperfcounters/iis
		assert.Equal(t, "60s", conf.Get("receivers::windowsperfcounters/iis::collection_interval"))
		assert.Equal(t, []string{"windowsperfcounters/iis"}, conf.Get("service::pipelines::metrics/default__pipeline_iis::receivers"))
		assert.Equal(t, []string{
			"metricstransform/iis_0",
			"transform/iis_1",
			"metric_start_time/iis_2",
			"transform/iis_3",
			"transform/iis_4",
			"transform/iis_5",
			"filter/default__pipeline_iis_0",
			"resourcedetection/_global_0",
			"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
			"batch/otlp_grpc/otlp_metrics_metrics_2",
		}, conf.Get("service::pipelines::metrics/default__pipeline_iis::processors"))

		// iis_v2 uses iis/iis__v2
		assert.Equal(t, "60s", conf.Get("receivers::iis/iis__v2::collection_interval"))
		assert.Equal(t, []string{"iis/iis__v2"}, conf.Get("service::pipelines::metrics/iispipeline_iis__v2::receivers"))
		assert.Equal(t, []string{
			"transform/iis__v2_0",
			"transform/iis__v2_1",
			"transform/iis__v2_2",
			"groupbyattrs/iis__v2_3",
			"metricstransform/iis__v2_4",
			"metric_start_time/iis__v2_5",
			"resourcedetection/_global_0",
			"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
			"batch/otlp_grpc/otlp_metrics_metrics_2",
		}, conf.Get("service::pipelines::metrics/iispipeline_iis__v2::processors"))
	})

	t.Run("override_builtin_iis_to_v2", func(t *testing.T) {
		userYAML := `metrics:
  receivers:
    iis:
      type: iis
      collection_interval: 30s
      receiver_version: 2
  service:
    pipelines:
      iispipeline:
        receivers:
          - iis
`
		configFile, _ := writeTestConfig(t, userYAML)
		p := &provider{logger: zap.NewNop(), hostInfo: winHost}
		retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
		require.NoError(t, err)

		conf, err := retrieved.AsConf()
		require.NoError(t, err)

		assert.False(t, conf.IsSet("receivers::windowsperfcounters/iis"))
		assert.Equal(t, "30s", conf.Get("receivers::iis/iis::collection_interval"))
		assert.Equal(t, []string{"iis/iis"}, conf.Get("service::pipelines::metrics/default__pipeline_iis::receivers"))
		assert.Equal(t, []string{"iis/iis"}, conf.Get("service::pipelines::metrics/iispipeline_iis::receivers"))
	})
}

func TestIISV1MetricsTransformation(t *testing.T) {
	winHost := hostInfo{OS: "windows"}
	procIDs := registerProcessors(map[string]any{}, (MetricsReceiver{Type: "iis"}).iisProcessors("iis"))
	chain, sink := buildProcessorChainForHost(t, "", winHost, procIDs)

	ts1 := pcommon.NewTimestampFromTime(time.Unix(1000, 0))
	ts2 := pcommon.NewTimestampFromTime(time.Unix(1060, 0))

	buildScrape := func(ts pcommon.Timestamp, scale float64) pmetric.Metrics {
		md := pmetric.NewMetrics()
		rm := md.ResourceMetrics().AppendEmpty()
		rm.Resource().Attributes().PutStr("service.name", "iis")
		rm.Resource().Attributes().PutStr("host.name", "win-vm")

		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName("windowsperfcountersreceiver")
		sm.Scope().SetVersion("0.162.0")

		addGauge := func(name string, val float64) {
			m := sm.Metrics().AppendEmpty()
			m.SetName(name)
			dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
			dp.SetTimestamp(ts)
			dp.SetDoubleValue(val)
		}

		addGauge(`\Web Service(_Total)\Current Connections`, 15)
		addGauge(`\Web Service(_Total)\Total Bytes Received`, 1000*scale)
		addGauge(`\Web Service(_Total)\Total Bytes Sent`, 2000*scale)
		addGauge(`\Web Service(_Total)\Total Connection Attempts (all instances)`, 50*scale)
		addGauge(`\Web Service(_Total)\Total Get Requests`, 120*scale)
		addGauge(`\Web Service(_Total)\Total Post Requests`, 30*scale)
		return md
	}

	require.NoError(t, chain.ConsumeMetrics(context.Background(), buildScrape(ts1, 1)))
	require.NoError(t, chain.ConsumeMetrics(context.Background(), buildScrape(ts2, 2)))

	all := sink.AllMetrics()
	require.NotEmpty(t, all)
	lastBatch := all[len(all)-1]
	require.Equal(t, 1, lastBatch.ResourceMetrics().Len())

	outRM := lastBatch.ResourceMetrics().At(0)
	_, hasServiceName := outRM.Resource().Attributes().Get("service.name")
	assert.False(t, hasServiceName)

	outSM := outRM.ScopeMetrics().At(0)
	assert.Empty(t, outSM.Scope().Name())
	assert.Empty(t, outSM.Scope().Version())

	gotMetrics := make(map[string]pmetric.Metric)
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		gotMetrics[m.Name()] = m
	}

	require.Contains(t, gotMetrics, "agent.googleapis.com/iis/current_connections")
	assert.Equal(t, pmetric.MetricTypeGauge, gotMetrics["agent.googleapis.com/iis/current_connections"].Type())
	assert.InDelta(t, 15.0, gotMetrics["agent.googleapis.com/iis/current_connections"].Gauge().DataPoints().At(0).DoubleValue(), 1e-6)

	require.Contains(t, gotMetrics, "agent.googleapis.com/iis/network/transferred_bytes_count")
	bytesMetric := gotMetrics["agent.googleapis.com/iis/network/transferred_bytes_count"]
	assert.Equal(t, pmetric.MetricTypeSum, bytesMetric.Type())
	assert.True(t, bytesMetric.Sum().IsMonotonic())
	assert.Equal(t, pmetric.AggregationTemporalityCumulative, bytesMetric.Sum().AggregationTemporality())
	bytesByDir := map[string]int64{}
	for i := 0; i < bytesMetric.Sum().DataPoints().Len(); i++ {
		dp := bytesMetric.Sum().DataPoints().At(i)
		dir, _ := dp.Attributes().Get("direction")
		bytesByDir[dir.Str()] = dp.IntValue()
	}
	assert.Equal(t, map[string]int64{"received": 1000, "sent": 2000}, bytesByDir)

	require.Contains(t, gotMetrics, "agent.googleapis.com/iis/new_connection_count")
	connMetric := gotMetrics["agent.googleapis.com/iis/new_connection_count"]
	assert.Equal(t, pmetric.MetricTypeSum, connMetric.Type())
	assert.Equal(t, int64(50), connMetric.Sum().DataPoints().At(0).IntValue())

	require.Contains(t, gotMetrics, "agent.googleapis.com/iis/request_count")
	reqMetric := gotMetrics["agent.googleapis.com/iis/request_count"]
	assert.Equal(t, pmetric.MetricTypeSum, reqMetric.Type())
	reqsByMethod := map[string]int64{}
	for i := 0; i < reqMetric.Sum().DataPoints().Len(); i++ {
		dp := reqMetric.Sum().DataPoints().At(i)
		method, _ := dp.Attributes().Get("http_method")
		reqsByMethod[method.Str()] = dp.IntValue()
	}
	assert.Equal(t, map[string]int64{"get": 120, "post": 30}, reqsByMethod)
}

func TestIISV2MetricsTransformation(t *testing.T) {
	winHost := hostInfo{OS: "windows"}
	userYAML := `metrics:
  receivers:
    iis_v2:
      type: iis
      receiver_version: 2
  service:
    pipelines:
      iispipeline:
        receivers: [iis_v2]
`
	procIDs := registerProcessors(map[string]any{}, (MetricsReceiver{Type: "iis", ReceiverVersion: "2"}).iisProcessors("iis__v2"))
	chain, sink := buildProcessorChainForHost(t, userYAML, winHost, procIDs)

	ts1 := pcommon.NewTimestampFromTime(time.Unix(1000, 0))
	ts2 := pcommon.NewTimestampFromTime(time.Unix(1060, 0))

	buildScrape := func(ts pcommon.Timestamp, scale int64) pmetric.Metrics {
		md := pmetric.NewMetrics()

		// Site 1 ResourceMetrics
		rm1 := md.ResourceMetrics().AppendEmpty()
		rm1.Resource().Attributes().PutStr("iis.site", "Default Web Site")
		rm1.Resource().Attributes().PutStr("service.name", "iis")
		sm1 := rm1.ScopeMetrics().AppendEmpty()
		mReq := sm1.Metrics().AppendEmpty()
		mReq.SetName("iis.request.count")
		sReq := mReq.SetEmptySum()
		sReq.SetIsMonotonic(true)
		sReq.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
		dp1 := sReq.DataPoints().AppendEmpty()
		dp1.SetTimestamp(ts)
		dp1.SetIntValue(100 * scale)
		dp1.Attributes().PutStr("request", "get")
		dp1.Attributes().PutStr("site_extra", "dropped")

		// App Pool ResourceMetrics
		rm2 := md.ResourceMetrics().AppendEmpty()
		rm2.Resource().Attributes().PutStr("iis.application_pool", "DefaultAppPool")
		sm2 := rm2.ScopeMetrics().AppendEmpty()
		mProc := sm2.Metrics().AppendEmpty()
		mProc.SetName("iis.application_pool.process.count")
		dpProc := mProc.SetEmptyGauge().DataPoints().AppendEmpty()
		dpProc.SetTimestamp(ts)
		dpProc.SetIntValue(3)

		return md
	}

	require.NoError(t, chain.ConsumeMetrics(context.Background(), buildScrape(ts1, 1)))
	require.NoError(t, chain.ConsumeMetrics(context.Background(), buildScrape(ts2, 2)))

	all := sink.AllMetrics()
	require.NotEmpty(t, all)
	lastBatch := all[len(all)-1]
	// groupbyattrs/iis__v2_3 condenses the cleared resources into a single ResourceMetrics.
	require.Equal(t, 1, lastBatch.ResourceMetrics().Len())

	outRM := lastBatch.ResourceMetrics().At(0)
	assert.Equal(t, 0, outRM.Resource().Attributes().Len())

	outSM := outRM.ScopeMetrics().At(0)
	assert.Equal(t, "agent.googleapis.com/iis", outSM.Scope().Name())
	assert.Equal(t, "2.0", outSM.Scope().Version())

	gotMetrics := make(map[string]pmetric.Metric)
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		gotMetrics[m.Name()] = m
	}

	require.Contains(t, gotMetrics, "workload.googleapis.com/iis.request.count")
	reqDP := gotMetrics["workload.googleapis.com/iis.request.count"].Sum().DataPoints().At(0)
	assert.Equal(t, int64(100), reqDP.IntValue())
	reqAttr, ok := reqDP.Attributes().Get("request")
	require.True(t, ok)
	assert.Equal(t, "get", reqAttr.Str())
	_, hasExtra := reqDP.Attributes().Get("site_extra")
	assert.False(t, hasExtra)

	require.Contains(t, gotMetrics, "workload.googleapis.com/iis.application_pool.process.count")
	procDP := gotMetrics["workload.googleapis.com/iis.application_pool.process.count"].Gauge().DataPoints().At(0)
	assert.Equal(t, int64(3), procDP.IntValue())
}
