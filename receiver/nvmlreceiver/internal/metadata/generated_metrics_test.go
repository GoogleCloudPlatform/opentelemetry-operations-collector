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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := pcommon.Timestamp(1_000_000_000)
			ts := pcommon.Timestamp(1_000_001_000)
			observedZapCore, observedLogs := observer.New(zap.WarnLevel)
			settings := receivertest.NewNopSettings(receivertest.NopType)
			settings.Logger = zap.New(observedZapCore)
			mb := NewMetricsBuilder(loadMetricsBuilderConfig(t, tt.name), settings, WithStartTime(start))

			expectedWarnings := 0

			assert.Equal(t, expectedWarnings, observedLogs.Len())

			defaultMetricsCount := 0
			allMetricsCount := 0

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNvmlGpuMemoryBytesUsedDataPoint(ts, 1, "model-val", "gpu_number-val", "uuid-val", AttributeMemoryStateUsed)

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNvmlGpuProcessesMaxBytesUsedDataPoint(ts, 1, "model-val", "gpu_number-val", "uuid-val", 3, "process-val", "command-val", "command_line-val", "owner-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNvmlGpuProcessesUtilizationDataPoint(ts, 1, "model-val", "gpu_number-val", "uuid-val", 3, "process-val", "command-val", "command_line-val", "owner-val")

			defaultMetricsCount++
			allMetricsCount++
			mb.RecordNvmlGpuUtilizationDataPoint(ts, 1, "model-val", "gpu_number-val", "uuid-val")

			res := pcommon.NewResource()
			metrics := mb.Emit(WithResource(res))

			if tt.expectEmpty {
				assert.Equal(t, 0, metrics.ResourceMetrics().Len())
				return
			}

			assert.Equal(t, 1, metrics.ResourceMetrics().Len())
			rm := metrics.ResourceMetrics().At(0)
			assert.Equal(t, res, rm.Resource())
			assert.Equal(t, 1, rm.ScopeMetrics().Len())
			ms := rm.ScopeMetrics().At(0).Metrics()
			if tt.metricsSet == testDataSetDefault {
				assert.Equal(t, defaultMetricsCount, ms.Len())
			}
			if tt.metricsSet == testDataSetAll {
				assert.Equal(t, allMetricsCount, ms.Len())
			}
			validatedMetrics := make(map[string]bool)
			for i := 0; i < ms.Len(); i++ {
				switch ms.At(i).Name() {
				case "nvml.gpu.memory.bytes_used":
					assert.False(t, validatedMetrics["nvml.gpu.memory.bytes_used"], "Found a duplicate in the metrics slice: nvml.gpu.memory.bytes_used")
					validatedMetrics["nvml.gpu.memory.bytes_used"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Current number of GPU memory bytes used by state. Summing the values of all states yields the total GPU memory space.", ms.At(i).Description())
					assert.Equal(t, "By", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("model")
					assert.True(t, ok)
					assert.EqualValues(t, "model-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("gpu_number")
					assert.True(t, ok)
					assert.EqualValues(t, "gpu_number-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("uuid")
					assert.True(t, ok)
					assert.EqualValues(t, "uuid-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("memory_state")
					assert.True(t, ok)
					assert.EqualValues(t, "used", attrVal.Str())
				case "nvml.gpu.processes.max_bytes_used":
					assert.False(t, validatedMetrics["nvml.gpu.processes.max_bytes_used"], "Found a duplicate in the metrics slice: nvml.gpu.processes.max_bytes_used")
					validatedMetrics["nvml.gpu.processes.max_bytes_used"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Maximum total GPU memory in bytes that was ever allocated by the process.", ms.At(i).Description())
					assert.Equal(t, "By", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeInt, dp.ValueType())
					assert.Equal(t, int64(1), dp.IntValue())
					attrVal, ok := dp.Attributes().Get("model")
					assert.True(t, ok)
					assert.EqualValues(t, "model-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("gpu_number")
					assert.True(t, ok)
					assert.EqualValues(t, "gpu_number-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("uuid")
					assert.True(t, ok)
					assert.EqualValues(t, "uuid-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("pid")
					assert.True(t, ok)
					assert.EqualValues(t, 3, attrVal.Int())
					attrVal, ok = dp.Attributes().Get("process")
					assert.True(t, ok)
					assert.EqualValues(t, "process-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("command")
					assert.True(t, ok)
					assert.EqualValues(t, "command-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("command_line")
					assert.True(t, ok)
					assert.EqualValues(t, "command_line-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("owner")
					assert.True(t, ok)
					assert.EqualValues(t, "owner-val", attrVal.Str())
				case "nvml.gpu.processes.utilization":
					assert.False(t, validatedMetrics["nvml.gpu.processes.utilization"], "Found a duplicate in the metrics slice: nvml.gpu.processes.utilization")
					validatedMetrics["nvml.gpu.processes.utilization"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Fraction of time over the process's life thus far during which one or more kernels was executing on the GPU.", ms.At(i).Description())
					assert.Equal(t, "1", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeDouble, dp.ValueType())
					assert.InDelta(t, float64(1), dp.DoubleValue(), 0.01)
					attrVal, ok := dp.Attributes().Get("model")
					assert.True(t, ok)
					assert.EqualValues(t, "model-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("gpu_number")
					assert.True(t, ok)
					assert.EqualValues(t, "gpu_number-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("uuid")
					assert.True(t, ok)
					assert.EqualValues(t, "uuid-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("pid")
					assert.True(t, ok)
					assert.EqualValues(t, 3, attrVal.Int())
					attrVal, ok = dp.Attributes().Get("process")
					assert.True(t, ok)
					assert.EqualValues(t, "process-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("command")
					assert.True(t, ok)
					assert.EqualValues(t, "command-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("command_line")
					assert.True(t, ok)
					assert.EqualValues(t, "command_line-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("owner")
					assert.True(t, ok)
					assert.EqualValues(t, "owner-val", attrVal.Str())
				case "nvml.gpu.utilization":
					assert.False(t, validatedMetrics["nvml.gpu.utilization"], "Found a duplicate in the metrics slice: nvml.gpu.utilization")
					validatedMetrics["nvml.gpu.utilization"] = true
					assert.Equal(t, pmetric.MetricTypeGauge, ms.At(i).Type())
					assert.Equal(t, 1, ms.At(i).Gauge().DataPoints().Len())
					assert.Equal(t, "Fraction of time GPU was not idle since the last sample.", ms.At(i).Description())
					assert.Equal(t, "1", ms.At(i).Unit())
					dp := ms.At(i).Gauge().DataPoints().At(0)
					assert.Equal(t, start, dp.StartTimestamp())
					assert.Equal(t, ts, dp.Timestamp())
					assert.Equal(t, pmetric.NumberDataPointValueTypeDouble, dp.ValueType())
					assert.InDelta(t, float64(1), dp.DoubleValue(), 0.01)
					attrVal, ok := dp.Attributes().Get("model")
					assert.True(t, ok)
					assert.EqualValues(t, "model-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("gpu_number")
					assert.True(t, ok)
					assert.EqualValues(t, "gpu_number-val", attrVal.Str())
					attrVal, ok = dp.Attributes().Get("uuid")
					assert.True(t, ok)
					assert.EqualValues(t, "uuid-val", attrVal.Str())
				}
			}
		})
	}
}
