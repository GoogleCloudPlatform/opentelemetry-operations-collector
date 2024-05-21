// Code generated by mdatagen. DO NOT EDIT.

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type testDataSet int

const (
	testDataSetDefault testDataSet = iota
	testDataSetAll
	testDataSetNone
)

func TestMetricsBuilder(t *testing.T) {
	tests := []struct {
		name        string
		metricsSet  testDataSet
		resAttrsSet testDataSet
		expectEmpty bool
	}{
		{
			name: "default",
		},
		{
			name:        "all_set",
			metricsSet:  testDataSetAll,
			resAttrsSet: testDataSetAll,
		},
		{
			name:        "none_set",
			metricsSet:  testDataSetNone,
			resAttrsSet: testDataSetNone,
			expectEmpty: true,
		},
		{
			name:        "filter_set_include",
			resAttrsSet: testDataSetAll,
		},
		{
			name:        "filter_set_exclude",
			resAttrsSet: testDataSetAll,
			expectEmpty: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := pcommon.Timestamp(1_000_000_000)
			ts := pcommon.Timestamp(1_000_001_000)
			observedZapCore, observedLogs := observer.New(zap.WarnLevel)
			settings := receivertest.NewNopCreateSettings()
			settings.Logger = zap.New(observedZapCore)
			mb := NewMetricsBuilder(loadMetricsBuilderConfig(t, test.name), settings, WithStartTime(start))

			expectedWarnings := 0

			assert.Equal(t, expectedWarnings, observedLogs.Len())

			defaultMetricsCount := 0
			allMetricsCount := 0

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishBackendConnectionCountDataPoint(ts, 1, AttributeBackendConnectionTypeSuccess)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishBackendRequestCountDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishCacheOperationCountDataPoint(ts, 1, AttributeCacheOperationsHit)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishClientRequestCountDataPoint(ts, 1, AttributeStateReceived)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishClientRequestErrorCountDataPoint(ts, 1, "http.status_code-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishObjectCountDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishObjectExpiredDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishObjectMovedDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishObjectNukedDataPoint(ts, 1)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishSessionCountDataPoint(ts, 1, AttributeSessionTypeAccepted)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordVarnishThreadOperationCountDataPoint(ts, 1, AttributeThreadOperationsCreated)

			rb := mb.NewResourceBuilder()
			rb.SetVarnishCacheName("varnish.cache.name-val")
			res := rb.Emit()
			metrics := mb.Emit(WithResource(res))

			if test.expectEmpty {
				assert.Equal(t, 0, metrics.ResourceMetrics().Len())
				return
			}

			assert.Equal(t, 1, metrics.ResourceMetrics().Len())
			rm := metrics.ResourceMetrics().At(0)
			assert.Equal(t, res, rm.Resource())
			assert.Equal(t, 1, rm.ScopeMetrics().Len())
			ms := rm.ScopeMetrics().At(0).Metrics()
			if test.metricsSet == testDataSetDefault {
				assert.Equal(t, defaultMetricsCount, ms.Len())
			}
			if test.metricsSet == testDataSetAll {
				assert.Equal(t, allMetricsCount, ms.Len())
			}
			validatedMetrics := make(map[string]bool)
			for i := 0; i < ms.Len(); i++ {
				switch ms.At(i).Name() {
				case "varnish.backend.connection.count":
					assert.False(t, validatedMetrics["varnish.backend.connection.count"], "Found a duplicate in the metrics slice: varnish.backend.connection.count")
					validatedMetrics["varnish.backend.connection.count"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The backend connection type count.", ms.At(i).Description())
					assert.Equal(t, "{connections}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("kind")
					assert.True(t, ok)
					assert.EqualValues(t, "success", attrVal.Str())
				case "varnish.backend.request.count":
					assert.False(t, validatedMetrics["varnish.backend.request.count"], "Found a duplicate in the metrics slice: varnish.backend.request.count")
					validatedMetrics["varnish.backend.request.count"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The backend requests count.", ms.At(i).Description())
					assert.Equal(t, "{requests}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "varnish.cache.operation.count":
					assert.False(t, validatedMetrics["varnish.cache.operation.count"], "Found a duplicate in the metrics slice: varnish.cache.operation.count")
					validatedMetrics["varnish.cache.operation.count"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The cache operation type count.", ms.At(i).Description())
					assert.Equal(t, "{operations}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("operation")
					assert.True(t, ok)
					assert.EqualValues(t, "hit", attrVal.Str())
				case "varnish.client.request.count":
					assert.False(t, validatedMetrics["varnish.client.request.count"], "Found a duplicate in the metrics slice: varnish.client.request.count")
					validatedMetrics["varnish.client.request.count"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The client request count.", ms.At(i).Description())
					assert.Equal(t, "{requests}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("state")
					assert.True(t, ok)
					assert.EqualValues(t, "received", attrVal.Str())
				case "varnish.client.request.error.count":
					assert.False(t, validatedMetrics["varnish.client.request.error.count"], "Found a duplicate in the metrics slice: varnish.client.request.error.count")
					validatedMetrics["varnish.client.request.error.count"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The client request errors received by status code.", ms.At(i).Description())
					assert.Equal(t, "{requests}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("status_code")
					assert.True(t, ok)
					assert.EqualValues(t, "http.status_code-val", attrVal.Str())
				case "varnish.object.count":
					assert.False(t, validatedMetrics["varnish.object.count"], "Found a duplicate in the metrics slice: varnish.object.count")
					validatedMetrics["varnish.object.count"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The HTTP objects in the cache count.", ms.At(i).Description())
					assert.Equal(t, "{objects}", ms.At(i).Unit())
					assert.Equal(t, false, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "varnish.object.expired":
					assert.False(t, validatedMetrics["varnish.object.expired"], "Found a duplicate in the metrics slice: varnish.object.expired")
					validatedMetrics["varnish.object.expired"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The expired objects from old age count.", ms.At(i).Description())
					assert.Equal(t, "{objects}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "varnish.object.moved":
					assert.False(t, validatedMetrics["varnish.object.moved"], "Found a duplicate in the metrics slice: varnish.object.moved")
					validatedMetrics["varnish.object.moved"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The moved operations done on the LRU list count.", ms.At(i).Description())
					assert.Equal(t, "{objects}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "varnish.object.nuked":
					assert.False(t, validatedMetrics["varnish.object.nuked"], "Found a duplicate in the metrics slice: varnish.object.nuked")
					validatedMetrics["varnish.object.nuked"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The objects that have been forcefully evicted from storage count.", ms.At(i).Description())
					assert.Equal(t, "{objects}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
				case "varnish.session.count":
					assert.False(t, validatedMetrics["varnish.session.count"], "Found a duplicate in the metrics slice: varnish.session.count")
					validatedMetrics["varnish.session.count"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The session connection type count.", ms.At(i).Description())
					assert.Equal(t, "{connections}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("kind")
					assert.True(t, ok)
					assert.EqualValues(t, "accepted", attrVal.Str())
				case "varnish.thread.operation.count":
					assert.False(t, validatedMetrics["varnish.thread.operation.count"], "Found a duplicate in the metrics slice: varnish.thread.operation.count")
					validatedMetrics["varnish.thread.operation.count"] = true
					assert.Equal(t, pmetric.MetricTypeSum, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Sum().DataPoints().Len())
					assert.Equal(t, "The thread operation type count.", ms.At(i).Description())
					assert.Equal(t, "{operations}", ms.At(i).Unit())
					assert.Equal(t, true, ms.At(i).Sum().IsMonotonic())
					assert.Equal(t, pmetric.AggregationTemporalityCumulative, ms.At(i).Sum().AggregationTemporality())
					dp := ms.At(i).Sum().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("operation")
					assert.True(t, ok)
					assert.EqualValues(t, "created", attrVal.Str())
				}
			}
		})
	}
}
