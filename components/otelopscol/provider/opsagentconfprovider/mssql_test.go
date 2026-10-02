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

func TestMSSQLValidate(t *testing.T) {
	testCases := []struct {
		name      string
		isWindows bool
		config    string
		wantErr   string
	}{
		{
			name:      "mssql_rejected_on_linux",
			isWindows: false,
			config: `metrics:
  receivers:
    mssql:
      type: mssql
`,
			wantErr: `metrics receiver "mssql" with type "mssql" is not supported`,
		},
		{
			name:      "mssql_with_config_rejected",
			isWindows: true,
			config: `metrics:
  receivers:
    mssql:
      type: mssql
      config:
        foo: bar
`,
			wantErr: `metrics receiver "mssql" with type "mssql" does not support config`,
		},
		{
			name:      "mssql_collection_interval_too_short",
			isWindows: true,
			config: `metrics:
  receivers:
    mssql:
      type: mssql
      collection_interval: 5s
`,
			wantErr: `metrics receiver "mssql" has invalid collection_interval "5s"`,
		},
		{
			name:      "mssql_invalid_receiver_version",
			isWindows: true,
			config: `metrics:
  receivers:
    mssql:
      type: mssql
      receiver_version: 3
`,
			wantErr: `metrics receiver "mssql" has invalid receiver_version "3": must be one of [1 2]`,
		},
		{
			name:      "duplicate_mssql_receivers_in_same_pipeline_rejected",
			isWindows: true,
			config: `metrics:
  receivers:
    mssql_1:
      type: mssql
    mssql_2:
      type: mssql
      receiver_version: 2
  service:
    pipelines:
      custom_pipe:
        receivers: [mssql_1, mssql_2]
`,
			wantErr: `at most one metrics receiver with type "mssql" is allowed in pipeline "custom_pipe"`,
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

func TestRetrieveMSSQLPipelines(t *testing.T) {
	winHost := hostInfo{OS: "windows"}

	t.Run("default_windows_mssql_v1_and_duplicate_v2", func(t *testing.T) {
		userYAML := `metrics:
  receivers:
    mssql_v2:
      type: mssql
      receiver_version: 2
  service:
    pipelines:
      mssql_v2:
        receivers:
          - mssql_v2
`
		configFile, _ := writeTestConfig(t, userYAML)
		p := &provider{logger: zap.NewNop(), hostInfo: winHost}
		retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
		require.NoError(t, err)

		conf, err := retrieved.AsConf()
		require.NoError(t, err)

		// Default mssql v1 uses windowsperfcounters/mssql
		assert.Equal(t, "60s", conf.Get("receivers::windowsperfcounters/mssql::collection_interval"))
		assert.Equal(t, []string{"windowsperfcounters/mssql"}, conf.Get("service::pipelines::metrics/default__pipeline_mssql::receivers"))
		assert.Equal(t, []string{
			"metricstransform/mssql_0",
			"transform/mssql_1",
			"transform/mssql_2",
			"transform/mssql_3",
			"filter/default__pipeline_mssql_0",
			"resourcedetection/_global_0",
			"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
			"batch/otlp_grpc/otlp_metrics_metrics_2",
		}, conf.Get("service::pipelines::metrics/default__pipeline_mssql::processors"))

		// mssql_v2 uses sqlserver/mssql__v2
		assert.Equal(t, "60s", conf.Get("receivers::sqlserver/mssql__v2::collection_interval"))
		assert.Equal(t, []string{"sqlserver/mssql__v2"}, conf.Get("service::pipelines::metrics/mssql__v2_mssql__v2::receivers"))
		assert.Equal(t, []string{
			"metricstransform/mssql__v2_0",
			"transform/mssql__v2_1",
			"transform/mssql__v2_2",
			"normalizesums/mssql__v2_3",
			"resourcedetection/_global_0",
			"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
			"batch/otlp_grpc/otlp_metrics_metrics_2",
		}, conf.Get("service::pipelines::metrics/mssql__v2_mssql__v2::processors"))
	})

	t.Run("override_builtin_mssql_to_v2", func(t *testing.T) {
		userYAML := `metrics:
  receivers:
    mssql:
      type: mssql
      collection_interval: 30s
      receiver_version: 2
  service:
    pipelines:
      mssql:
        receivers:
          - mssql
`
		configFile, _ := writeTestConfig(t, userYAML)
		p := &provider{logger: zap.NewNop(), hostInfo: winHost}
		retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
		require.NoError(t, err)

		conf, err := retrieved.AsConf()
		require.NoError(t, err)

		assert.False(t, conf.IsSet("receivers::windowsperfcounters/mssql"))
		assert.Equal(t, "30s", conf.Get("receivers::sqlserver/mssql::collection_interval"))
		assert.Equal(t, []string{"sqlserver/mssql"}, conf.Get("service::pipelines::metrics/default__pipeline_mssql::receivers"))
		assert.Equal(t, []string{"sqlserver/mssql"}, conf.Get("service::pipelines::metrics/mssql_mssql::receivers"))
	})
}

func TestMSSQLV1MetricsTransformation(t *testing.T) {
	winHost := hostInfo{OS: "windows"}
	procIDs := registerProcessors(map[string]any{}, (MetricsReceiver{Type: "mssql"}).mssqlProcessors("mssql"))
	chain, sink := buildProcessorChainForHost(t, "", winHost, procIDs)

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "mssql")
	rm.Resource().Attributes().PutStr("host.name", "win-sql")

	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName("windowsperfcountersreceiver")
	sm.Scope().SetVersion("0.162.0")

	addGauge := func(name string, val float64) {
		m := sm.Metrics().AppendEmpty()
		m.SetName(name)
		m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(val)
	}

	addGauge(`\SQLServer:General Statistics(_Total)\User Connections`, 42)
	addGauge(`\SQLServer:Databases(_Total)\Transactions/sec`, 125.5)
	addGauge(`\SQLServer:Databases(_Total)\Write Transactions/sec`, 34.5)

	require.NoError(t, chain.ConsumeMetrics(context.Background(), md))

	all := sink.AllMetrics()
	require.Len(t, all, 1)
	outRM := all[0].ResourceMetrics().At(0)
	_, hasServiceName := outRM.Resource().Attributes().Get("service.name")
	assert.False(t, hasServiceName)

	outSM := outRM.ScopeMetrics().At(0)
	assert.Empty(t, outSM.Scope().Name())
	assert.Empty(t, outSM.Scope().Version())

	gotMetrics := make(map[string]float64)
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		gotMetrics[m.Name()] = m.Gauge().DataPoints().At(0).DoubleValue()
	}

	assert.Equal(t, map[string]float64{
		"agent.googleapis.com/mssql/connections/user":       42,
		"agent.googleapis.com/mssql/transaction_rate":       125.5,
		"agent.googleapis.com/mssql/write_transaction_rate": 34.5,
	}, gotMetrics)
}

func TestMSSQLV2MetricsTransformation(t *testing.T) {
	winHost := hostInfo{OS: "windows"}
	userYAML := `metrics:
  receivers:
    mssql_v2:
      type: mssql
      receiver_version: 2
  service:
    pipelines:
      mssql_v2:
        receivers: [mssql_v2]
`
	procIDs := registerProcessors(map[string]any{}, (MetricsReceiver{Type: "mssql", ReceiverVersion: "2"}).mssqlProcessors("mssql__v2"))
	chain, sink := buildProcessorChainForHost(t, userYAML, winHost, procIDs)

	ts1 := pcommon.NewTimestampFromTime(time.Unix(1000, 0))
	ts2 := pcommon.NewTimestampFromTime(time.Unix(1060, 0))

	buildScrape := func(ts pcommon.Timestamp, batchReqs int64) pmetric.Metrics {
		md := pmetric.NewMetrics()
		rm := md.ResourceMetrics().AppendEmpty()
		rm.Resource().Attributes().PutStr("sqlserver.database.name", "tempdb")
		rm.Resource().Attributes().PutStr("service.name", "sqlserver")

		sm := rm.ScopeMetrics().AppendEmpty()

		mUsage := sm.Metrics().AppendEmpty()
		mUsage.SetName("sqlserver.transaction_log.usage")
		dpUsage := mUsage.SetEmptyGauge().DataPoints().AppendEmpty()
		dpUsage.SetTimestamp(ts)
		dpUsage.SetDoubleValue(65.5)

		mBatch := sm.Metrics().AppendEmpty()
		mBatch.SetName("sqlserver.batch.request.rate")
		sBatch := mBatch.SetEmptySum()
		sBatch.SetIsMonotonic(true)
		sBatch.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
		dpBatch := sBatch.DataPoints().AppendEmpty()
		dpBatch.SetTimestamp(ts)
		dpBatch.SetIntValue(batchReqs)

		return md
	}

	require.NoError(t, chain.ConsumeMetrics(context.Background(), buildScrape(ts1, 100)))
	require.NoError(t, chain.ConsumeMetrics(context.Background(), buildScrape(ts2, 250)))

	all := sink.AllMetrics()
	require.NotEmpty(t, all)
	lastBatch := all[len(all)-1]
	require.Equal(t, 1, lastBatch.ResourceMetrics().Len())

	outRM := lastBatch.ResourceMetrics().At(0)
	_, hasServiceName := outRM.Resource().Attributes().Get("service.name")
	assert.False(t, hasServiceName)

	outSM := outRM.ScopeMetrics().At(0)
	assert.Equal(t, "agent.googleapis.com/mssql", outSM.Scope().Name())
	assert.Equal(t, "2.0", outSM.Scope().Version())

	gotMetrics := make(map[string]pmetric.Metric)
	for i := 0; i < outSM.Metrics().Len(); i++ {
		m := outSM.Metrics().At(i)
		gotMetrics[m.Name()] = m
	}

	require.Contains(t, gotMetrics, "workload.googleapis.com/sqlserver.transaction_log.percent_used")
	usageDP := gotMetrics["workload.googleapis.com/sqlserver.transaction_log.percent_used"].Gauge().DataPoints().At(0)
	assert.InDelta(t, 65.5, usageDP.DoubleValue(), 1e-6)
	dbVal, ok := usageDP.Attributes().Get("database")
	require.True(t, ok)
	assert.Equal(t, "tempdb", dbVal.Str())

	require.Contains(t, gotMetrics, "workload.googleapis.com/sqlserver.batch.request.rate")
	batchDP := gotMetrics["workload.googleapis.com/sqlserver.batch.request.rate"].Sum().DataPoints().At(0)
	// normalizesums subtracts initial point (250 - 100 = 150) and sets start timestamp to ts1.
	assert.Equal(t, int64(150), batchDP.IntValue())
	assert.Equal(t, ts1, batchDP.StartTimestamp())
	dbBatchVal, ok := batchDP.Attributes().Get("database")
	require.True(t, ok)
	assert.Equal(t, "tempdb", dbBatchVal.Str())
}
