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
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor/processortest"
)

func TestAgentPrometheusReceiver(t *testing.T) {
	conf := confmap.NewFromStringMap(agentPrometheusReceiver(20201))
	scrapeConfigs, ok := conf.Get("config::scrape_configs").([]map[string]any)
	require.True(t, ok)
	require.Len(t, scrapeConfigs, 1)
	assert.Equal(t, "otel-collector", scrapeConfigs[0]["job_name"])
	assert.Equal(t, "1m", scrapeConfigs[0]["scrape_interval"])

	staticConfigs, ok := scrapeConfigs[0]["static_configs"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, staticConfigs, 1)
	assert.Equal(t, []string{"0.0.0.0:20201"}, staticConfigs[0]["targets"])
}

func TestTelemetryConfig(t *testing.T) {
	conf := confmap.NewFromStringMap(telemetryConfig(20201))
	assert.Equal(t, "detailed", conf.Get("metrics::level"))

	readers, ok := conf.Get("metrics::readers").([]map[string]any)
	require.True(t, ok)
	require.Len(t, readers, 1)

	readerConf := confmap.NewFromStringMap(readers[0])
	assert.Equal(t, "0.0.0.0", readerConf.Get("pull::exporter::prometheus::host"))
	assert.Equal(t, 20201, readerConf.Get("pull::exporter::prometheus::port"))
	assert.Equal(t, true, readerConf.Get("pull::exporter::prometheus::without_scope_info"))
	assert.Equal(t, true, readerConf.Get("pull::exporter::prometheus::without_units"))
	assert.Equal(t, true, readerConf.Get("pull::exporter::prometheus::without_type_suffix"))
}

func TestAgentPrometheusProcessors(t *testing.T) {
	procs := map[string]any{}
	ids := registerProcessors(procs, agentPrometheusProcessors())
	assert.Equal(t, []string{
		"transform/agent_prometheus_0",
		"transform/agent_prometheus_1",
		"transform/agent_prometheus_2",
	}, ids)
	require.Len(t, procs, 3)

	for _, id := range ids {
		assert.Contains(t, procs, id)
	}
}

func TestOtelSelfMetricsProcessors(t *testing.T) {
	procs := map[string]any{}
	ids := registerProcessors(procs, otelSelfMetricsProcessors("test-agent/1.0.0"))
	assert.Equal(t, []string{
		"transform/otel_0",
		"filter/otel_1",
		"filter/otel_2",
		"metricstransform/otel_3",
	}, ids)
	require.Len(t, procs, 4)

	conf := confmap.NewFromStringMap(procs)
	assert.Equal(t, "ignore", conf.Get("transform/otel_0::error_mode"))
	assert.Equal(t, []string{
		`metric.name == "rpc.client.call.duration_count" and (not IsMatch(datapoint.attributes["rpc.method"], "opentelemetry.proto.collector.metrics.v1.MetricsService/Export"))`,
	}, conf.Get("filter/otel_1::metrics::datapoint"))
	assert.Equal(t, []string{
		"otelcol_process_uptime",
		"otelcol_process_memory_rss",
		"otelcol_exporter_sent_metric_points",
		"otelcol_exporter_send_failed_metric_points",
		"rpc.client.call.duration_count",
	}, conf.Get("filter/otel_2::metrics::include::metric_names"))

	transforms, ok := conf.Get("metricstransform/otel_3::transforms").([]map[string]any)
	require.True(t, ok)
	require.Len(t, transforms, 7)

	// First transform renames otelcol_process_uptime -> agent/uptime with custom version label.
	assert.Equal(t, "otelcol_process_uptime", transforms[0]["include"])
	assert.Equal(t, "agent/uptime", transforms[0]["new_name"])
	ops, ok := transforms[0]["operations"].([]map[string]any)
	require.True(t, ok)
	assert.Contains(t, ops, map[string]any{
		"action":    "add_label",
		"new_label": "version",
		"new_value": "test-agent/1.0.0",
	})

	// Last transform adds the agent.googleapis.com/ prefix.
	assert.Equal(t, "^(.*)$", transforms[len(transforms)-1]["include"])
	assert.Equal(t, "agent.googleapis.com/${1}", transforms[len(transforms)-1]["new_name"])
}

func TestLoggingSelfMetricsProcessors(t *testing.T) {
	procs := map[string]any{}
	ids := registerProcessors(procs, loggingSelfMetricsProcessors())
	assert.Equal(t, []string{
		"transform/loggingmetrics_0",
		"filter/loggingmetrics_1",
		"filter/loggingmetrics_2",
		"metricstransform/loggingmetrics_3",
		"transform/loggingmetrics_4",
		"interval/loggingmetrics_5",
		"metricstransform/loggingmetrics_6",
	}, ids)
	require.Len(t, procs, 7)

	conf := confmap.NewFromStringMap(procs)
	assert.Equal(t, []string{
		`metric.name == "rpc.client.call.duration_count" and (not IsMatch(datapoint.attributes["rpc.method"], "opentelemetry.proto.collector.logs.v1.LogsService/Export"))`,
	}, conf.Get("filter/loggingmetrics_1::metrics::datapoint"))
	assert.Equal(t, []string{
		"otelcol_exporter_sent_log_records",
		"otelcol_exporter_send_failed_log_records",
		"rpc.client.call.duration_count",
	}, conf.Get("filter/loggingmetrics_2::metrics::include::metric_names"))
	assert.Equal(t, "1m", conf.Get("interval/loggingmetrics_5::interval"))

	transforms, ok := conf.Get("metricstransform/loggingmetrics_3::transforms").([]map[string]any)
	require.True(t, ok)
	require.Len(t, transforms, 5)
	assert.Equal(t, "agent/log_entry_retry_count", transforms[0]["new_name"])
	assert.Equal(t, "agent/request_count", transforms[1]["new_name"])
	assert.Equal(t, "agent/log_entry_count", transforms[2]["new_name"])
	assert.Equal(t, "agent/log_entry_count", transforms[3]["new_name"])
	assert.Equal(t, "combine", transforms[4]["action"])
}

func TestRenameLabelValuesSorted(t *testing.T) {
	op := renameLabelValues("status", map[string]string{
		"Z_ERR": "500",
		"A_OK":  "200",
		"M_BAD": "400",
	})
	assert.Equal(t, "update_label", op["action"])
	assert.Equal(t, "status", op["label"])
	assert.Equal(t, []map[string]string{
		{"value": "A_OK", "new_value": "200"},
		{"value": "M_BAD", "new_value": "400"},
		{"value": "Z_ERR", "new_value": "500"},
	}, op["value_actions"])
}

func buildProcessorChain(t *testing.T, procIDs []string) (consumer.Metrics, *consumertest.MetricsSink) {
	t.Helper()
	ctx := context.Background()
	configFile, _ := writeTestConfig(t, "")
	factories := testFactories(t)

	configProvider, err := otelcol.NewConfigProvider(otelcol.ConfigProviderSettings{
		ResolverSettings: confmap.ResolverSettings{
			URIs:              []string{"opsagentconf:" + configFile},
			ProviderFactories: []confmap.ProviderFactory{NewFactory()},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, configProvider.Shutdown(ctx))
	})

	cfg, err := configProvider.Get(ctx, factories)
	require.NoError(t, err)

	sink := new(consumertest.MetricsSink)
	var next consumer.Metrics = sink
	for i := len(procIDs) - 1; i >= 0; i-- {
		var id component.ID
		require.NoError(t, id.UnmarshalText([]byte(procIDs[i])))
		factory := factories.Processors[id.Type()]
		require.NotNil(t, factory, "missing processor factory for %s", id)
		procCfg := cfg.Processors[id]
		require.NotNil(t, procCfg, "missing processor config for %s", id)

		set := processortest.NewNopSettings(id.Type())
		set.ID = id
		proc, err := factory.CreateMetrics(ctx, set, procCfg, next)
		require.NoError(t, err)
		require.NoError(t, proc.Start(ctx, componenttest.NewNopHost()))
		t.Cleanup(func() {
			require.NoError(t, proc.Shutdown(ctx))
		})
		next = proc
	}
	return next, sink
}

func TestOtelSelfMetricsTransformation(t *testing.T) {
	expectedVersionLabel := detectHostInfo().versionLabel()
	promIDs := registerProcessors(map[string]any{}, agentPrometheusProcessors())
	otelIDs := registerProcessors(map[string]any{}, otelSelfMetricsProcessors(expectedVersionLabel))
	chain, sink := buildProcessorChain(t, append(promIDs, otelIDs...))

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "otelcol")
	rm.Resource().Attributes().PutStr("server.port", "20201")
	rm.Resource().Attributes().PutStr("host.name", "test-vm")

	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName("otelcol/prometheus")
	sm.Scope().SetVersion("0.162.0")

	// 1. otelcol_process_uptime (double -> int64 agent/uptime with version label)
	mUptime := sm.Metrics().AppendEmpty()
	mUptime.SetName("otelcol_process_uptime")
	dpUptime := mUptime.SetEmptySum().DataPoints().AppendEmpty()
	dpUptime.SetDoubleValue(42.0)

	// 2. otelcol_process_memory_rss (-> agent/memory_usage)
	mMem := sm.Metrics().AppendEmpty()
	mMem.SetName("otelcol_process_memory_rss")
	dpMem := mMem.SetEmptyGauge().DataPoints().AppendEmpty()
	dpMem.SetIntValue(1048576)
	dpMem.Attributes().PutStr("extra", "dropped")

	// 3. Sent and failed metric points (-> combined agent/monitoring/point_count)
	mSent := sm.Metrics().AppendEmpty()
	mSent.SetName("otelcol_exporter_sent_metric_points")
	dpSent := mSent.SetEmptySum().DataPoints().AppendEmpty()
	dpSent.SetDoubleValue(100.0)

	mFailed := sm.Metrics().AppendEmpty()
	mFailed.SetName("otelcol_exporter_send_failed_metric_points")
	dpFailed := mFailed.SetEmptySum().DataPoints().AppendEmpty()
	dpFailed.SetDoubleValue(5.0)
	dpFailed.Attributes().PutStr("error.type", "DeadlineExceeded")

	// 4. rpc.client.call.duration histogram (one MetricsService/Export, one LogsService/Export)
	mRPC := sm.Metrics().AppendEmpty()
	mRPC.SetName("rpc.client.call.duration")
	h := mRPC.SetEmptyHistogram()
	h.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dpMetricsRPC := h.DataPoints().AppendEmpty()
	dpMetricsRPC.SetCount(7)
	dpMetricsRPC.Attributes().PutStr("rpc.method", "opentelemetry.proto.collector.metrics.v1.MetricsService/Export")
	dpMetricsRPC.Attributes().PutStr("rpc.response.status_code", "OK")

	dpLogsRPC := h.DataPoints().AppendEmpty()
	dpLogsRPC.SetCount(99)
	dpLogsRPC.Attributes().PutStr("rpc.method", "opentelemetry.proto.collector.logs.v1.LogsService/Export")
	dpLogsRPC.Attributes().PutStr("rpc.response.status_code", "OK")

	// 5. Unrelated metric (should be filtered out)
	mUnrelated := sm.Metrics().AppendEmpty()
	mUnrelated.SetName("otelcol_scraper_errored_metric_points")
	mUnrelated.SetEmptySum().DataPoints().AppendEmpty().SetIntValue(1)

	require.NoError(t, chain.ConsumeMetrics(context.Background(), md))

	all := sink.AllMetrics()
	require.Len(t, all, 1)
	outRM := all[0].ResourceMetrics().At(0)

	// Verify agent_prometheus resource/scope attribute cleanup.
	_, hasServiceName := outRM.Resource().Attributes().Get("service.name")
	assert.False(t, hasServiceName)
	_, hasServerPort := outRM.Resource().Attributes().Get("server.port")
	assert.False(t, hasServerPort)
	hostVal, hasHost := outRM.Resource().Attributes().Get("host.name")
	require.True(t, hasHost)
	assert.Equal(t, "test-vm", hostVal.Str())

	outSM := outRM.ScopeMetrics().At(0)
	assert.Empty(t, outSM.Scope().Name())
	assert.Empty(t, outSM.Scope().Version())

	gotMetrics := make(map[string]pmetric.Metric)
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		gotMetrics[m.Name()] = m
	}

	require.Len(t, gotMetrics, 4)
	require.Contains(t, gotMetrics, "agent.googleapis.com/agent/uptime")
	require.Contains(t, gotMetrics, "agent.googleapis.com/agent/memory_usage")
	require.Contains(t, gotMetrics, "agent.googleapis.com/agent/api_request_count")
	require.Contains(t, gotMetrics, "agent.googleapis.com/agent/monitoring/point_count")

	uptimeDP := gotMetrics["agent.googleapis.com/agent/uptime"].Sum().DataPoints().At(0)
	assert.Equal(t, int64(42), uptimeDP.IntValue())
	verVal, ok := uptimeDP.Attributes().Get("version")
	require.True(t, ok)
	assert.Equal(t, expectedVersionLabel, verVal.Str())

	apiDP := gotMetrics["agent.googleapis.com/agent/api_request_count"].Sum().DataPoints()
	require.Equal(t, 1, apiDP.Len())
	assert.Equal(t, int64(7), apiDP.At(0).IntValue())
	stateVal, ok := apiDP.At(0).Attributes().Get("state")
	require.True(t, ok)
	assert.Equal(t, "OK", stateVal.Str())
}

func TestLoggingSelfMetricsTransformation(t *testing.T) {
	// Test all loggingmetrics processors except interval/loggingmetrics_5 so datapoints emit immediately.
	chain, sink := buildProcessorChain(t, []string{
		"transform/loggingmetrics_0",
		"filter/loggingmetrics_1",
		"filter/loggingmetrics_2",
		"metricstransform/loggingmetrics_3",
		"transform/loggingmetrics_4",
		"metricstransform/loggingmetrics_6",
	})

	md := pmetric.NewMetrics()
	sm := md.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty()

	mSent := sm.Metrics().AppendEmpty()
	mSent.SetName("otelcol_exporter_sent_log_records")
	mSent.SetEmptySum().DataPoints().AppendEmpty().SetDoubleValue(25.0)

	mFailed := sm.Metrics().AppendEmpty()
	mFailed.SetName("otelcol_exporter_send_failed_log_records")
	mFailed.SetEmptySum().DataPoints().AppendEmpty().SetDoubleValue(3.0)

	mRPC := sm.Metrics().AppendEmpty()
	mRPC.SetName("rpc.client.call.duration")
	h := mRPC.SetEmptyHistogram()
	h.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	dpLogsRPC := h.DataPoints().AppendEmpty()
	dpLogsRPC.SetCount(4)
	dpLogsRPC.Attributes().PutStr("rpc.method", "opentelemetry.proto.collector.logs.v1.LogsService/Export")
	dpLogsRPC.Attributes().PutStr("rpc.response.status_code", "PermissionDenied")

	require.NoError(t, chain.ConsumeMetrics(context.Background(), md))

	all := sink.AllMetrics()
	require.Len(t, all, 1)
	outSM := all[0].ResourceMetrics().At(0).ScopeMetrics().At(0)

	gotMetrics := make(map[string]pmetric.Metric)
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		gotMetrics[m.Name()] = m
		assert.Equal(t, "1", m.Unit())
	}

	assert.Len(t, gotMetrics, 3)
	assert.Contains(t, gotMetrics, "agent.googleapis.com/agent/log_entry_retry_count")
	assert.Contains(t, gotMetrics, "agent.googleapis.com/agent/request_count")
	assert.Contains(t, gotMetrics, "agent.googleapis.com/agent/log_entry_count")

	reqDP := gotMetrics["agent.googleapis.com/agent/request_count"].Sum().DataPoints()
	require.Equal(t, 1, reqDP.Len())
	assert.Equal(t, int64(4), reqDP.At(0).IntValue())
	codeVal, ok := reqDP.At(0).Attributes().Get("response_code")
	require.True(t, ok)
	assert.Equal(t, "403", codeVal.Str())
}
