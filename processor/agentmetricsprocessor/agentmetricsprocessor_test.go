// Copyright 2020 Google LLC
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

package agentmetricsprocessor

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/model/otlp"
	"go.opentelemetry.io/collector/model/pdata"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.uber.org/zap"
)

type testCase struct {
	name                      string
	input                     pdata.Metrics
	expected                  pdata.Metrics
	prevCPUTimeValuesInput    map[string]float64
	prevCPUTimeValuesExpected map[string]float64
	prevOpInput               map[opKey]opData
}

func TestAgentMetricsProcessor(t *testing.T) {
	tests := []testCase{
		{
			name:     "non-monotonic-sums-case",
			input:    generateNonMonotonicSumsInput(),
			expected: generateNonMonotonicSumsExpected(),
		},
		{
			name:     "remove-version-case",
			input:    generateVersionInput(),
			expected: generateVersionExpected(),
		},
		{
			name:     "remove--just-version-case",
			input:    generateMultiAttrVersionInput(),
			expected: generateMultiAttrVersionExpected(),
		},
		{
			name:     "process-resources-case",
			input:    generateProcessResourceMetricsInput(),
			expected: generateProcessResourceMetricsExpected(),
		},
		{
			name:     "read-write-split-case",
			input:    generateReadWriteMetricsInput(),
			expected: generateReadWriteMetricsExpected(),
		},
		{
			name:                      "utilization-case",
			input:                     generateUtilizationMetricsInput(),
			expected:                  generateUtilizationMetricsExpected(),
			prevCPUTimeValuesInput:    generateUtilizationPrevCPUTimeValuesInput(),
			prevCPUTimeValuesExpected: generateUtilizationPrevCPUTimeValuesExpected(),
		},
		{
			name:     "cpu-number-case",
			input:    generateCPUMetricsInput(),
			expected: generateCPUMetricsExpected(),
		},
		{
			name:     "average-disk",
			input:    generateAverageDiskInput(),
			expected: generateAverageDiskExpected(),
		},
		{
			name:        "average-disk-prev",
			input:       generateAverageDiskInput(),
			expected:    generateAverageDiskPrevExpected(),
			prevOpInput: generateAverageDiskPrevOpInput(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amp := newAgentMetricsProcessor(zap.NewExample(), &Config{
				BlankLabelMetrics: []string{"system.cpu.time"},
			})

			tmn := &consumertest.MetricsSink{}
			rmp, err := processorhelper.NewMetricsProcessor(
				&Config{
					ProcessorSettings: config.NewProcessorSettings(config.NewComponentID(typeStr)),
				},
				tmn,
				amp.ProcessMetrics,
				processorhelper.WithCapabilities(processorCapabilities))
			require.NoError(t, err)
			assert.True(t, rmp.Capabilities().MutatesData)

			amp.prevCPUTimeValues = tt.prevCPUTimeValuesInput
			if tt.prevOpInput != nil {
				amp.prevOp = tt.prevOpInput
			}
			require.NoError(t, rmp.Start(context.Background(), componenttest.NewNopHost()))
			defer func() { assert.NoError(t, rmp.Shutdown(context.Background())) }()

			err = rmp.ConsumeMetrics(context.Background(), tt.input)
			require.NoError(t, err)

			marshaler := otlp.NewJSONMetricsMarshaler()
			outJSON, err := marshaler.MarshalMetrics(tmn.AllMetrics()[0])
			require.NoError(t, err)
			t.Logf("actual metrics: %s", outJSON)

			assertEqual(t, tt.expected, tmn.AllMetrics()[0])
			if tt.prevCPUTimeValuesExpected != nil {
				assert.Equal(t, tt.prevCPUTimeValuesExpected, amp.prevCPUTimeValues)
			}
		})
	}
}

// builders to generate test metrics

type resourceMetricsBuilder struct {
	rms pdata.ResourceMetricsSlice
}

func newResourceMetricsBuilder() resourceMetricsBuilder {
	return resourceMetricsBuilder{rms: pdata.NewResourceMetricsSlice()}
}

func (rmsb resourceMetricsBuilder) addResourceMetrics(resourceAttributes map[string]pdata.Value) metricsBuilder {
	rm := rmsb.rms.AppendEmpty()

	for k, v := range resourceAttributes {
		rm.Resource().Attributes().Insert(k, v)
	}

	ilm := rm.ScopeMetrics().AppendEmpty()

	return metricsBuilder{metrics: ilm.Metrics()}
}

func (rmsb resourceMetricsBuilder) Build() pdata.ResourceMetricsSlice {
	return rmsb.rms
}

type metricsBuilder struct {
	metrics   pdata.MetricSlice
	timestamp pdata.Timestamp
}

func (msb metricsBuilder) addMetric(name string, t pdata.MetricDataType, isMonotonic bool) metricBuilder {
	metric := msb.metrics.AppendEmpty()
	metric.SetName(name)
	metric.SetDataType(t)

	switch t {
	case pmetric.MetricDataTypeSum:
		sum := metric.Sum()
		sum.SetIsMonotonic(isMonotonic)
		sum.SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
	case pmetric.MetricDataTypeGauge:
		metric.Gauge()
	}

	return metricBuilder{metric: metric, timestamp: msb.timestamp}
}

type metricBuilder struct {
	metric    pdata.Metric
	timestamp pdata.Timestamp
}

func (mb metricBuilder) addIntDataPoint(value int64, labels map[string]string) metricBuilder {
	var idp pdata.NumberDataPoint
	switch mb.metric.DataType() {
	case pmetric.MetricDataTypeSum:
		idp = mb.metric.Sum().DataPoints().AppendEmpty()
	case pmetric.MetricDataTypeGauge:
		idp = mb.metric.Gauge().DataPoints().AppendEmpty()
	}
	for k, v := range labels {
		idp.Attributes().InsertString(k, v)
	}
	idp.SetIntVal(value)
	idp.SetTimestamp(mb.timestamp)

	return mb
}

func (mb metricBuilder) addDoubleDataPoint(value float64, labels map[string]string) metricBuilder {
	var ddp pdata.NumberDataPoint
	switch mb.metric.DataType() {
	case pmetric.MetricDataTypeSum:
		ddp = mb.metric.Sum().DataPoints().AppendEmpty()
	case pmetric.MetricDataTypeGauge:
		ddp = mb.metric.Gauge().DataPoints().AppendEmpty()
	}
	for k, v := range labels {
		ddp.Attributes().InsertString(k, v)
	}
	ddp.SetDoubleVal(value)
	ddp.SetTimestamp(mb.timestamp)

	return mb
}

// assertEqual is required because Attribute & Label Maps are not sorted by default
// and we don't provide any guarantees on the order of transformed metrics
func assertEqual(t *testing.T, expected, actual pdata.Metrics) {
	rmsAct := actual.ResourceMetrics()
	rmsExp := expected.ResourceMetrics()
	require.Equal(t, rmsExp.Len(), rmsAct.Len())
	for i := 0; i < rmsAct.Len(); i++ {
		rmAct := rmsAct.At(i)
		rmExp := rmsExp.At(i)

		// assert equality of resource attributes
		assert.Equal(t, rmExp.Resource().Attributes().Sort(), rmAct.Resource().Attributes().Sort())

		// assert equality of IL metrics
		ilmsAct := rmAct.ScopeMetrics()
		ilmsExp := rmExp.ScopeMetrics()
		require.Equal(t, ilmsExp.Len(), ilmsAct.Len())
		for j := 0; j < ilmsAct.Len(); j++ {
			ilmAct := ilmsAct.At(j)
			ilmExp := ilmsExp.At(j)

			// assert equality of metrics
			metricsAct := ilmAct.Metrics()
			metricsExp := ilmExp.Metrics()
			require.Equal(t, metricsExp.Len(), metricsAct.Len(), "Number of metrics")

			// build a map of expected metrics
			metricsExpMap := make(map[string]pdata.Metric, metricsExp.Len())
			for k := 0; k < metricsExp.Len(); k++ {
				metricsExpMap[metricsExp.At(k).Name()] = metricsExp.At(k)
			}

			for k := 0; k < metricsAct.Len(); k++ {
				metricAct := metricsAct.At(k)
				metricExp, ok := metricsExpMap[metricAct.Name()]
				if !ok {
					require.Fail(t, fmt.Sprintf("unexpected metric %v", metricAct.Name()))
				}

				// assert equality of descriptors
				assert.Equal(t, metricExp.Name(), metricAct.Name())
				assert.Equalf(t, metricExp.Description(), metricAct.Description(), "Metric %s", metricAct.Name())
				assert.Equalf(t, metricExp.Unit(), metricAct.Unit(), "Metric %s", metricAct.Name())
				assert.Equalf(t, metricExp.DataType(), metricAct.DataType(), "Metric %s", metricAct.Name())

				// assert equality of aggregation info & data points
				switch ty := metricAct.DataType(); ty {
				case pmetric.MetricDataTypeSum:
					assert.Equal(t, metricAct.Sum().AggregationTemporality(), metricExp.Sum().AggregationTemporality(), "Metric %s", metricAct.Name())
					assert.Equal(t, metricAct.Sum().IsMonotonic(), metricExp.Sum().IsMonotonic(), "Metric %s", metricAct.Name())
					assertEqualNumberDataPointSlice(t, metricAct.Name(), metricAct.Sum().DataPoints(), metricExp.Sum().DataPoints())
				case pmetric.MetricDataTypeGauge:
					assertEqualNumberDataPointSlice(t, metricAct.Name(), metricAct.Gauge().DataPoints(), metricExp.Gauge().DataPoints())
				default:
					assert.Fail(t, "unexpected metric type", t)
				}
			}
		}
	}
}

const epsilon = 0.0000000001

func assertEqualNumberDataPointSlice(t *testing.T, metricName string, ndpsAct, ndpsExp pdata.NumberDataPointSlice) {
	require.Equalf(t, ndpsExp.Len(), ndpsAct.Len(), "Metric %s", metricName)

	// build a map of expected data points
	ndpsExpMap := make(map[string]pdata.NumberDataPoint, ndpsExp.Len())
	for k := 0; k < ndpsExp.Len(); k++ {
		ndpsExpMap[labelsAsKey(ndpsExp.At(k).Attributes())] = ndpsExp.At(k)
	}

	for l := 0; l < ndpsAct.Len(); l++ {
		ndpAct := ndpsAct.At(l)

		key := labelsAsKey(ndpAct.Attributes())

		ndpExp, ok := ndpsExpMap[key]
		if !ok {
			require.Failf(t, fmt.Sprintf("no data point for %s", labelsAsKey(ndpAct.Attributes())), "Metric %s", metricName)
		}

		assert.Equalf(t, ndpExp.Attributes().Sort(), ndpAct.Attributes().Sort(), "Metric %s attributes %s", metricName, key)
		assert.Equalf(t, ndpExp.StartTimestamp(), ndpAct.StartTimestamp(), "Metric %s attributes %s", metricName, key)
		assert.Equalf(t, ndpExp.Timestamp(), ndpAct.Timestamp(), "Metric %s attributes %s", metricName, key)
		assert.Equalf(t, ndpExp.ValueType(), ndpAct.ValueType(), "Metric %s attributes %s", metricName, key)
		switch ndpExp.ValueType() {
		case pmetric.MetricValueTypeInt:
			assert.Equalf(t, ndpExp.IntVal(), ndpAct.IntVal(), "Metric %s attributes %s", metricName, key)
		case pmetric.MetricValueTypeDouble:
			assert.InEpsilonf(t, ndpExp.DoubleVal(), ndpAct.DoubleVal(), epsilon, "Metric %s attributes %s", metricName, key)
		}
	}
}
