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
	"go.opentelemetry.io/collector/model/pdata"
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
					ProcessorSettings: config.NewProcessorSettings(config.NewID(typeStr)),
				},
				tmn,
				amp,
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

func (rmsb resourceMetricsBuilder) addResourceMetrics(resourceAttributes map[string]pdata.AttributeValue) metricsBuilder {
	rm := pdata.NewResourceMetrics()

	if resourceAttributes != nil {
		rm.Resource().Attributes().InitFromMap(resourceAttributes)
	}

	rm.InstrumentationLibraryMetrics().Resize(1)
	ilm := rm.InstrumentationLibraryMetrics().At(0)

	rmsb.rms.Append(rm)
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
	metric := pdata.NewMetric()
	metric.SetName(name)
	metric.SetDataType(t)

	switch t {
	case pdata.MetricDataTypeIntSum:
		sum := metric.IntSum()
		sum.SetIsMonotonic(isMonotonic)
		sum.SetAggregationTemporality(pdata.AggregationTemporalityCumulative)
	case pdata.MetricDataTypeDoubleSum:
		sum := metric.DoubleSum()
		sum.SetIsMonotonic(isMonotonic)
		sum.SetAggregationTemporality(pdata.AggregationTemporalityCumulative)
	case pdata.MetricDataTypeIntGauge:
		metric.IntGauge()
	case pdata.MetricDataTypeDoubleGauge:
		metric.DoubleGauge()
	}

	msb.metrics.Append(metric)
	return metricBuilder{metric: metric, timestamp: msb.timestamp}
}

type metricBuilder struct {
	metric    pdata.Metric
	timestamp pdata.Timestamp
}

func (mb metricBuilder) addIntDataPoint(value int64, labels map[string]string) metricBuilder {
	idp := pdata.NewIntDataPoint()
	idp.LabelsMap().InitFromMap(labels)
	idp.SetValue(value)
	idp.SetTimestamp(mb.timestamp)

	switch mb.metric.DataType() {
	case pdata.MetricDataTypeIntSum:
		mb.metric.IntSum().DataPoints().Append(idp)
	case pdata.MetricDataTypeIntGauge:
		mb.metric.IntGauge().DataPoints().Append(idp)
	}

	return mb
}

func (mb metricBuilder) addDoubleDataPoint(value float64, labels map[string]string) metricBuilder {
	ddp := pdata.NewDoubleDataPoint()
	ddp.LabelsMap().InitFromMap(labels)
	ddp.SetValue(value)
	ddp.SetTimestamp(mb.timestamp)

	switch mb.metric.DataType() {
	case pdata.MetricDataTypeDoubleSum:
		mb.metric.DoubleSum().DataPoints().Append(ddp)
	case pdata.MetricDataTypeDoubleGauge:
		mb.metric.DoubleGauge().DataPoints().Append(ddp)
	}

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
		ilmsAct := rmAct.InstrumentationLibraryMetrics()
		ilmsExp := rmExp.InstrumentationLibraryMetrics()
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
				case pdata.MetricDataTypeIntSum:
					assert.Equal(t, metricAct.IntSum().AggregationTemporality(), metricExp.IntSum().AggregationTemporality(), "Metric %s", metricAct.Name())
					assert.Equal(t, metricAct.IntSum().IsMonotonic(), metricExp.IntSum().IsMonotonic(), "Metric %s", metricAct.Name())
					assertEqualIntDataPointSlice(t, metricAct.Name(), metricAct.IntSum().DataPoints(), metricExp.IntSum().DataPoints())
				case pdata.MetricDataTypeDoubleSum:
					assert.Equal(t, metricAct.DoubleSum().AggregationTemporality(), metricExp.DoubleSum().AggregationTemporality(), "Metric %s", metricAct.Name())
					assert.Equal(t, metricAct.DoubleSum().IsMonotonic(), metricExp.DoubleSum().IsMonotonic(), "Metric %s", metricAct.Name())
					assertEqualDoubleDataPointSlice(t, metricAct.Name(), metricAct.DoubleSum().DataPoints(), metricExp.DoubleSum().DataPoints())
				case pdata.MetricDataTypeIntGauge:
					assertEqualIntDataPointSlice(t, metricAct.Name(), metricAct.IntGauge().DataPoints(), metricExp.IntGauge().DataPoints())
				case pdata.MetricDataTypeDoubleGauge:
					assertEqualDoubleDataPointSlice(t, metricAct.Name(), metricAct.DoubleGauge().DataPoints(), metricExp.DoubleGauge().DataPoints())
				default:
					assert.Fail(t, "unexpected metric type", t)
				}
			}
		}
	}
}

func assertEqualIntDataPointSlice(t *testing.T, metricName string, idpsAct, idpsExp pdata.IntDataPointSlice) {
	require.Equalf(t, idpsExp.Len(), idpsAct.Len(), "Metric %s", metricName)

	// build a map of expected data points
	idpsExpMap := make(map[string]pdata.IntDataPoint, idpsExp.Len())
	for k := 0; k < idpsExp.Len(); k++ {
		idpsExpMap[labelsAsKey(idpsExp.At(k).LabelsMap())] = idpsExp.At(k)
	}

	for l := 0; l < idpsAct.Len(); l++ {
		idpAct := idpsAct.At(l)

		idpExp, ok := idpsExpMap[labelsAsKey(idpAct.LabelsMap())]
		if !ok {
			require.Failf(t, fmt.Sprintf("no data point for %s", labelsAsKey(idpAct.LabelsMap())), "Metric %s", metricName)
		}

		assert.Equalf(t, idpExp.LabelsMap().Sort(), idpAct.LabelsMap().Sort(), "Metric %s", metricName)
		assert.Equalf(t, idpExp.StartTimestamp(), idpAct.StartTimestamp(), "Metric %s", metricName)
		assert.Equalf(t, idpExp.Timestamp(), idpAct.Timestamp(), "Metric %s", metricName)
		assert.Equalf(t, idpExp.Value(), idpAct.Value(), "Metric %s", metricName)
	}
}

func assertEqualDoubleDataPointSlice(t *testing.T, metricName string, ddpsAct, ddpsExp pdata.DoubleDataPointSlice) {
	require.Equalf(t, ddpsExp.Len(), ddpsAct.Len(), "Metric %s number of points", metricName)

	// build a map of expected data points
	ddpsExpMap := make(map[string]pdata.DoubleDataPoint, ddpsExp.Len())
	for k := 0; k < ddpsExp.Len(); k++ {
		ddpsExpMap[labelsAsKey(ddpsExp.At(k).LabelsMap())] = ddpsExp.At(k)
	}

	for l := 0; l < ddpsAct.Len(); l++ {
		ddpAct := ddpsAct.At(l)

		key := labelsAsKey(ddpAct.LabelsMap())

		ddpExp, ok := ddpsExpMap[key]
		if !ok {
			require.Failf(t, fmt.Sprintf("no data point for %s", key), "Metric %s", metricName)
		}

		assert.Equalf(t, ddpExp.LabelsMap().Sort(), ddpAct.LabelsMap().Sort(), "Labels for metric %s point %d labels %q", metricName, l, labelsAsKey(ddpAct.LabelsMap()))
		assert.Equalf(t, ddpExp.StartTimestamp(), ddpAct.StartTimestamp(), "StartTimestamp for metric %s point %d labels %q", metricName, l, labelsAsKey(ddpAct.LabelsMap()))
		assert.Equalf(t, ddpExp.Timestamp(), ddpAct.Timestamp(), "Timestamp for metric %s point %d labels %q", metricName, l, labelsAsKey(ddpAct.LabelsMap()))
		assert.InDeltaf(t, ddpExp.Value(), ddpAct.Value(), 0.00000001, "Value for metric %s point %d labels %q", metricName, l, labelsAsKey(ddpAct.LabelsMap()))
	}
}
