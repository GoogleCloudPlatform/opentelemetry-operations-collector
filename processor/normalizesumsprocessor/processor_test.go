// Copyright 2021 Google LLC
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

package normalizesumsprocessor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/model/pdata"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.uber.org/zap"
)

type testCase struct {
	name     string
	inputs   []pdata.Metrics
	expected []pdata.Metrics
}

func TestNormalizeSumsProcessor(t *testing.T) {
	testStart := time.Now().Unix()
	tests := []testCase{
		{
			name:     "no-transform-case",
			inputs:   generateNoTransformMetrics(testStart),
			expected: generateNoTransformMetrics(testStart),
		},
		{
			name:     "removed-metric-case",
			inputs:   generateRemoveInput(testStart),
			expected: generateRemoveOutput(testStart),
		},
		{
			name:     "transform-all-happy-case",
			inputs:   generateLabelledInput(testStart),
			expected: generateLabelledOutput(testStart),
		},
		{
			name:     "transform-all-label-separated-case",
			inputs:   generateSeparatedLabelledInput(testStart),
			expected: generateSeparatedLabelledOutput(testStart),
		},
		{
			name:     "more-complex-case",
			inputs:   generateComplexInput(testStart),
			expected: generateComplexOutput(testStart),
		},
		{
			name:     "multiple-resource-case",
			inputs:   generateMultipleResourceInput(testStart),
			expected: generateMultipleResourceOutput(testStart),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nsp := newNormalizeSumsProcessor(zap.NewExample())

			tmn := &consumertest.MetricsSink{}
			id := config.NewID(typeStr)
			settings := config.NewProcessorSettings(id)
			rmp, err := processorhelper.NewMetricsProcessor(
				&Config{
					ProcessorSettings: &settings,
				},
				tmn,
				nsp,
				processorhelper.WithCapabilities(processorCapabilities))
			require.NoError(t, err)

			require.True(t, rmp.Capabilities().MutatesData)

			require.NoError(t, rmp.Start(context.Background(), componenttest.NewNopHost()))
			defer func() { require.NoError(t, rmp.Shutdown(context.Background())) }()

			for _, input := range tt.inputs {
				err = rmp.ConsumeMetrics(context.Background(), input)
				require.NoError(t, err)
			}

			requireEqual(t, tt.expected, tmn.AllMetrics())
		})
	}
}

func generateNoTransformMetrics(startTime int64) []pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime+1000, startTime)
	mb1.addIntDataPoint(5, map[string]string{}, startTime+2000, startTime)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+3000, startTime+2000)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeDoubleSum, true)
	mb2.addDoubleDataPoint(3, map[string]string{}, startTime+6000, startTime)
	mb2.addDoubleDataPoint(4, map[string]string{}, startTime+7000, startTime)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeIntGauge, false)
	mb3.addIntDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addIntDataPoint(4, map[string]string{}, startTime+1000, 0)

	mb4 := b.addMetric("m4", pdata.MetricDataTypeDoubleGauge, false)
	mb4.addDoubleDataPoint(50000.2, map[string]string{}, startTime, 0)
	mb4.addDoubleDataPoint(11, map[string]string{}, startTime+1000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	return []pdata.Metrics{input}
}

func generateMultipleResourceInput(startTime int64) []pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(map[string]pdata.AttributeValue{
		"label1": pdata.NewAttributeValueString("value1"),
	})

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+1000, 0)

	b2 := rmb.addResourceMetrics(map[string]pdata.AttributeValue{
		"label1": pdata.NewAttributeValueString("value2"),
	})

	mb2 := b2.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb2.addIntDataPoint(5, map[string]string{}, startTime+2000, 0)
	mb2.addIntDataPoint(10, map[string]string{}, startTime+3000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	return []pdata.Metrics{input}
}

func generateMultipleResourceOutput(startTime int64) []pdata.Metrics {
	output := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(map[string]pdata.AttributeValue{
		"label1": pdata.NewAttributeValueString("value1"),
	})

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	// mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(1, map[string]string{}, startTime+1000, startTime)

	b2 := rmb.addResourceMetrics(map[string]pdata.AttributeValue{
		"label1": pdata.NewAttributeValueString("value2"),
	})

	mb2 := b2.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	// mb2.addIntDataPoint(5, map[string]string{}, startTime+2000, 0)
	mb2.addIntDataPoint(5, map[string]string{}, startTime+3000, startTime+2000)

	rmb.Build().CopyTo(output.ResourceMetrics())
	return []pdata.Metrics{output}
}

func generateLabelledInput(startTime int64) []pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb1.addIntDataPoint(0, map[string]string{"label": "val1"}, startTime, 0)
	mb1.addIntDataPoint(3, map[string]string{"label": "val2"}, startTime, 0)
	mb1.addIntDataPoint(12, map[string]string{"label": "val1"}, startTime+1000, 0)
	mb1.addIntDataPoint(5, map[string]string{"label": "val2"}, startTime+1000, 0)
	mb1.addIntDataPoint(15, map[string]string{"label": "val1"}, startTime+2000, 0)
	mb1.addIntDataPoint(1, map[string]string{"label": "val2"}, startTime+2000, 0)
	mb1.addIntDataPoint(22, map[string]string{"label": "val1"}, startTime+3000, 0)
	mb1.addIntDataPoint(11, map[string]string{"label": "val2"}, startTime+3000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	return []pdata.Metrics{input}
}

func generateLabelledOutput(startTime int64) []pdata.Metrics {
	output := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	// mb1.addIntDataPoint(1, map[string]string{"label": "val1"}, startTime, 0)
	// mb1.addIntDataPoint(1, map[string]string{"label": "val2"}, startTime, 0)
	mb1.addIntDataPoint(12, map[string]string{"label": "val1"}, startTime+1000, startTime)
	mb1.addIntDataPoint(2, map[string]string{"label": "val2"}, startTime+1000, startTime)
	mb1.addIntDataPoint(15, map[string]string{"label": "val1"}, startTime+2000, startTime)
	// mb1.addIntDataPoint(1, map[string]string{"label": "val2"}, startTime+2000, 1)
	mb1.addIntDataPoint(22, map[string]string{"label": "val1"}, startTime+3000, startTime)
	mb1.addIntDataPoint(10, map[string]string{"label": "val2"}, startTime+3000, startTime+2000)

	rmb.Build().CopyTo(output.ResourceMetrics())
	return []pdata.Metrics{output}
}

func generateSeparatedLabelledInput(startTime int64) []pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb1.addIntDataPoint(0, map[string]string{"label": "val1"}, startTime, 0)
	mb1.addIntDataPoint(12, map[string]string{"label": "val1"}, startTime+1000, 0)
	mb1.addIntDataPoint(15, map[string]string{"label": "val1"}, startTime+2000, 0)
	mb1.addIntDataPoint(22, map[string]string{"label": "val1"}, startTime+3000, 0)

	mb2 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb2.addIntDataPoint(3, map[string]string{"label": "val2"}, startTime, 0)
	mb2.addIntDataPoint(5, map[string]string{"label": "val2"}, startTime+1000, 0)
	mb2.addIntDataPoint(1, map[string]string{"label": "val2"}, startTime+2000, 0)
	mb2.addIntDataPoint(11, map[string]string{"label": "val2"}, startTime+3000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	return []pdata.Metrics{input}
}

func generateSeparatedLabelledOutput(startTime int64) []pdata.Metrics {
	output := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	// mb1.addIntDataPoint(1, map[string]string{"label": "val1"}, startTime, 0)
	mb1.addIntDataPoint(12, map[string]string{"label": "val1"}, startTime+1000, startTime)
	mb1.addIntDataPoint(15, map[string]string{"label": "val1"}, startTime+2000, startTime)
	mb1.addIntDataPoint(22, map[string]string{"label": "val1"}, startTime+3000, startTime)

	mb2 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	// mb2.addIntDataPoint(1, map[string]string{"label": "val2"}, startTime, 0)
	mb2.addIntDataPoint(2, map[string]string{"label": "val2"}, startTime+1000, startTime)
	// mb2.addIntDataPoint(1, map[string]string{"label": "val2"}, startTime+2000, 1)
	mb2.addIntDataPoint(10, map[string]string{"label": "val2"}, startTime+3000, startTime+2000)

	rmb.Build().CopyTo(output.ResourceMetrics())
	return []pdata.Metrics{output}
}

func generateRemoveInput(startTime int64) []pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeDoubleSum, true)
	mb2.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(4, map[string]string{}, startTime+1000, 0)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeDoubleGauge, false)
	mb3.addDoubleDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addDoubleDataPoint(6, map[string]string{}, startTime+1000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	return []pdata.Metrics{input}
}

func generateRemoveOutput(startTime int64) []pdata.Metrics {
	output := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeDoubleSum, true)
	// mb2.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(1, map[string]string{}, startTime+1000, startTime)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeDoubleGauge, false)
	mb3.addDoubleDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addDoubleDataPoint(6, map[string]string{}, startTime+1000, 0)

	rmb.Build().CopyTo(output.ResourceMetrics())
	return []pdata.Metrics{output}
}

func generateComplexInput(startTime int64) []pdata.Metrics {
	list := []pdata.Metrics{}
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+1000, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+2000, 0)
	mb1.addIntDataPoint(5, map[string]string{}, startTime+3000, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+4000, 0)
	mb1.addIntDataPoint(4, map[string]string{}, startTime+5000, 0)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeDoubleSum, true)
	mb2.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(4, map[string]string{}, startTime+1000, 0)
	mb2.addDoubleDataPoint(5, map[string]string{}, startTime+2000, 0)
	mb2.addDoubleDataPoint(2, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(8, map[string]string{}, startTime+3000, 0)
	mb2.addDoubleDataPoint(2, map[string]string{}, startTime+10000, 0)
	mb2.addDoubleDataPoint(6, map[string]string{}, startTime+120000, 0)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeDoubleGauge, false)
	mb3.addDoubleDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addDoubleDataPoint(6, map[string]string{}, startTime+1000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	list = append(list, input)

	input = pdata.NewMetrics()
	rmb = newResourceMetricsBuilder()
	b = rmb.addResourceMetrics(nil)

	mb1 = b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb1.addIntDataPoint(7, map[string]string{}, startTime+6000, 0)
	mb1.addIntDataPoint(9, map[string]string{}, startTime+7000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	list = append(list, input)

	return list
}

func generateComplexOutput(startTime int64) []pdata.Metrics {
	list := []pdata.Metrics{}
	output := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	// mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(1, map[string]string{}, startTime+1000, startTime)
	mb1.addIntDataPoint(1, map[string]string{}, startTime+2000, startTime)
	mb1.addIntDataPoint(4, map[string]string{}, startTime+3000, startTime)
	// mb1.addIntDataPoint(2, map[string]string{}, startTime+4000, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+5000, startTime+4000)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeDoubleSum, true)
	// mb2.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(1, map[string]string{}, startTime+1000, startTime)
	mb2.addDoubleDataPoint(2, map[string]string{}, startTime+2000, startTime)
	// mb2.addDoubleDataPoint(2, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(5, map[string]string{}, startTime+3000, startTime)
	// mb2.addDoubleDataPoint(2, map[string]string{}, startTime+10000, 0)
	mb2.addDoubleDataPoint(4, map[string]string{}, startTime+120000, startTime+10000)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeDoubleGauge, false)
	mb3.addDoubleDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addDoubleDataPoint(6, map[string]string{}, startTime+1000, 0)

	rmb.Build().CopyTo(output.ResourceMetrics())
	list = append(list, output)

	output = pdata.NewMetrics()

	rmb = newResourceMetricsBuilder()
	b = rmb.addResourceMetrics(nil)

	mb1 = b.addMetric("m1", pdata.MetricDataTypeIntSum, true)
	mb1.addIntDataPoint(5, map[string]string{}, startTime+6000, startTime+4000)
	mb1.addIntDataPoint(7, map[string]string{}, startTime+7000, startTime+4000)

	rmb.Build().CopyTo(output.ResourceMetrics())
	list = append(list, output)

	return list
}

// builders to generate test metrics

type resourceMetricsBuilder struct {
	rms pdata.ResourceMetricsSlice
}

func newResourceMetricsBuilder() resourceMetricsBuilder {
	return resourceMetricsBuilder{rms: pdata.NewResourceMetricsSlice()}
}

func (rmsb resourceMetricsBuilder) addResourceMetrics(resourceAttributes map[string]pdata.AttributeValue) metricsBuilder {
	rm := rmsb.rms.AppendEmpty()

	if resourceAttributes != nil {
		rm.Resource().Attributes().InitFromMap(resourceAttributes)
	}

	ilm := rm.InstrumentationLibraryMetrics().AppendEmpty()

	return metricsBuilder{metrics: ilm.Metrics()}
}

func (rmsb resourceMetricsBuilder) Build() pdata.ResourceMetricsSlice {
	return rmsb.rms
}

type metricsBuilder struct {
	metrics pdata.MetricSlice
}

func (msb metricsBuilder) addMetric(name string, t pdata.MetricDataType, isMonotonic bool) metricBuilder {
	metric := msb.metrics.AppendEmpty()
	metric.SetName(name)
	metric.SetDataType(t)

	switch t {
	case pdata.MetricDataTypeDoubleSum:
		sum := metric.DoubleSum()
		sum.SetIsMonotonic(isMonotonic)
		sum.SetAggregationTemporality(pdata.AggregationTemporalityCumulative)
	case pdata.MetricDataTypeIntSum:
		sum := metric.IntSum()
		sum.SetIsMonotonic(isMonotonic)
		sum.SetAggregationTemporality(pdata.AggregationTemporalityCumulative)
	}

	return metricBuilder{metric: metric}
}

type metricBuilder struct {
	metric pdata.Metric
}

func (mb metricBuilder) addDoubleDataPoint(value float64, labels map[string]string, timestamp int64, startTimestamp int64) {
	switch mb.metric.DataType() {
	case pdata.MetricDataTypeDoubleSum:
		ddp := mb.metric.DoubleSum().DataPoints().AppendEmpty()
		ddp.LabelsMap().InitFromMap(labels)
		ddp.SetValue(value)
		ddp.SetTimestamp(pdata.TimestampFromTime(time.Unix(timestamp, 0)))
		if startTimestamp > 0 {
			ddp.SetStartTimestamp(pdata.TimestampFromTime(time.Unix(startTimestamp, 0)))
		}
	case pdata.MetricDataTypeDoubleGauge:
		ddp := mb.metric.DoubleGauge().DataPoints().AppendEmpty()
		ddp.LabelsMap().InitFromMap(labels)
		ddp.SetValue(value)
		ddp.SetTimestamp(pdata.TimestampFromTime(time.Unix(timestamp, 0)))
		if startTimestamp > 0 {
			ddp.SetStartTimestamp(pdata.TimestampFromTime(time.Unix(startTimestamp, 0)))
		}
	}
}

func (mb metricBuilder) addIntDataPoint(value int64, labels map[string]string, timestamp int64, startTimestamp int64) {
	switch mb.metric.DataType() {
	case pdata.MetricDataTypeIntSum:
		ddp := mb.metric.IntSum().DataPoints().AppendEmpty()
		ddp.LabelsMap().InitFromMap(labels)
		ddp.SetValue(value)
		ddp.SetTimestamp(pdata.TimestampFromTime(time.Unix(timestamp, 0)))
		if startTimestamp > 0 {
			ddp.SetStartTimestamp(pdata.TimestampFromTime(time.Unix(startTimestamp, 0)))
		}
	case pdata.MetricDataTypeIntGauge:
		ddp := mb.metric.IntGauge().DataPoints().AppendEmpty()
		ddp.LabelsMap().InitFromMap(labels)
		ddp.SetValue(value)
		ddp.SetTimestamp(pdata.TimestampFromTime(time.Unix(timestamp, 0)))
		if startTimestamp > 0 {
			ddp.SetStartTimestamp(pdata.TimestampFromTime(time.Unix(startTimestamp, 0)))
		}
	}
}

// requireEqual is required because Attribute & Label Maps are not sorted by default
// and we don't provide any guarantees on the order of transformed metrics
func requireEqual(t *testing.T, expected, actual []pdata.Metrics) {
	require.Equal(t, len(expected), len(actual))

	for q := 0; q < len(actual); q++ {
		rmsAct := actual[q].ResourceMetrics()
		rmsExp := expected[q].ResourceMetrics()
		require.Equal(t, rmsExp.Len(), rmsAct.Len())
		for i := 0; i < rmsAct.Len(); i++ {
			rmAct := rmsAct.At(i)
			rmExp := rmsExp.At(i)

			// require equality of resource attributes
			require.Equal(t, rmExp.Resource().Attributes().Sort(), rmAct.Resource().Attributes().Sort())

			// require equality of IL metrics
			ilmsAct := rmAct.InstrumentationLibraryMetrics()
			ilmsExp := rmExp.InstrumentationLibraryMetrics()
			require.Equal(t, ilmsExp.Len(), ilmsAct.Len())
			for j := 0; j < ilmsAct.Len(); j++ {
				ilmAct := ilmsAct.At(j)
				ilmExp := ilmsExp.At(j)

				// require equality of metrics
				metricsAct := ilmAct.Metrics()
				metricsExp := ilmExp.Metrics()
				require.Equal(t, metricsExp.Len(), metricsAct.Len())

				// build a map of expected metrics
				metricsExpMap := make(map[string]pdata.Metric, metricsExp.Len())
				for k := 0; k < metricsExp.Len(); k++ {
					metricsExpMap[metricsExp.At(k).Name()] = metricsExp.At(k)
				}

				for k := 0; k < metricsAct.Len(); k++ {
					metricAct := metricsAct.At(k)
					metricExp := metricsExp.At(k)

					// require equality of descriptors
					require.Equal(t, metricExp.Name(), metricAct.Name())
					require.Equalf(t, metricExp.Description(), metricAct.Description(), "Metric %s", metricAct.Name())
					require.Equalf(t, metricExp.Unit(), metricAct.Unit(), "Metric %s", metricAct.Name())
					require.Equalf(t, metricExp.DataType(), metricAct.DataType(), "Metric %s", metricAct.Name())

					// require equality of aggregation info & data points
					switch ty := metricAct.DataType(); ty {
					case pdata.MetricDataTypeDoubleSum:
						require.Equal(t, metricAct.DoubleSum().AggregationTemporality(), metricExp.DoubleSum().AggregationTemporality(), "Metric %s", metricAct.Name())
						require.Equal(t, metricAct.DoubleSum().IsMonotonic(), metricExp.DoubleSum().IsMonotonic(), "Metric %s", metricAct.Name())
						requireEqualDoubleDataPointSlice(t, metricAct.Name(), metricAct.DoubleSum().DataPoints(), metricExp.DoubleSum().DataPoints())
					case pdata.MetricDataTypeIntSum:
						require.Equal(t, metricAct.IntSum().AggregationTemporality(), metricExp.IntSum().AggregationTemporality(), "Metric %s", metricAct.Name())
						require.Equal(t, metricAct.IntSum().IsMonotonic(), metricExp.IntSum().IsMonotonic(), "Metric %s", metricAct.Name())
						requireEqualIntDataPointSlice(t, metricAct.Name(), metricAct.IntSum().DataPoints(), metricExp.IntSum().DataPoints())
					case pdata.MetricDataTypeDoubleGauge:
						requireEqualDoubleDataPointSlice(t, metricAct.Name(), metricAct.DoubleGauge().DataPoints(), metricExp.DoubleGauge().DataPoints())
					case pdata.MetricDataTypeIntGauge:
						requireEqualIntDataPointSlice(t, metricAct.Name(), metricAct.IntGauge().DataPoints(), metricExp.IntGauge().DataPoints())
					default:
						require.Fail(t, "unexpected metric type", t)
					}
				}
			}
		}
	}
}

func requireEqualDoubleDataPointSlice(t *testing.T, metricName string, ddpsAct, ddpsExp pdata.DoubleDataPointSlice) {
	require.Equalf(t, ddpsExp.Len(), ddpsAct.Len(), "Metric %s", metricName)

	// build a map of expected data points
	ddpsExpMap := make(map[string]pdata.DoubleDataPoint, ddpsExp.Len())
	for k := 0; k < ddpsExp.Len(); k++ {
		ddpsExp := ddpsExp.At(k)
		ddpsExpMap[dataPointKey(metricName, ddpsExp.LabelsMap(), ddpsExp.Timestamp(), ddpsExp.StartTimestamp())] = ddpsExp
	}

	for l := 0; l < ddpsAct.Len(); l++ {
		ddpAct := ddpsAct.At(l)
		dpKey := dataPointKey(metricName, ddpAct.LabelsMap(), ddpAct.Timestamp(), ddpAct.StartTimestamp())

		ddpExp, ok := ddpsExpMap[dpKey]
		if !ok {
			require.Failf(t, fmt.Sprintf("no data point for %s", dpKey), "Metric %s", metricName)
		}

		require.Equalf(t, ddpExp.LabelsMap().Sort(), ddpAct.LabelsMap().Sort(), "Metric %s", metricName)
		require.Equalf(t, ddpExp.StartTimestamp(), ddpAct.StartTimestamp(), "Metric %s", metricName)
		require.Equalf(t, ddpExp.Timestamp(), ddpAct.Timestamp(), "Metric %s", metricName)
		require.InDeltaf(t, ddpExp.Value(), ddpAct.Value(), 0.00000001, "Metric %s", metricName)
	}
}

func requireEqualIntDataPointSlice(t *testing.T, metricName string, ddpsAct, ddpsExp pdata.IntDataPointSlice) {
	require.Equalf(t, ddpsExp.Len(), ddpsAct.Len(), "Metric %s", metricName)

	// build a map of expected data points
	ddpsExpMap := make(map[string]pdata.IntDataPoint, ddpsExp.Len())
	for k := 0; k < ddpsExp.Len(); k++ {
		ddpsExp := ddpsExp.At(k)
		ddpsExpMap[dataPointKey(metricName, ddpsExp.LabelsMap(), ddpsExp.Timestamp(), ddpsExp.StartTimestamp())] = ddpsExp
	}

	for l := 0; l < ddpsAct.Len(); l++ {
		ddpAct := ddpsAct.At(l)
		dpKey := dataPointKey(metricName, ddpAct.LabelsMap(), ddpAct.Timestamp(), ddpAct.StartTimestamp())

		ddpExp, ok := ddpsExpMap[dpKey]
		if !ok {
			require.Failf(t, fmt.Sprintf("no data point for %s", dpKey), "Metric %s", metricName)
		}

		require.Equalf(t, ddpExp.LabelsMap().Sort(), ddpAct.LabelsMap().Sort(), "Metric %s", metricName)
		require.Equalf(t, ddpExp.StartTimestamp(), ddpAct.StartTimestamp(), "Metric %s", metricName)
		require.Equalf(t, ddpExp.Timestamp(), ddpAct.Timestamp(), "Metric %s", metricName)
		require.InDeltaf(t, ddpExp.Value(), ddpAct.Value(), 0.00000001, "Metric %s", metricName)
	}
}

// dataPointKey returns a key representing the data point
func dataPointKey(metricName string, labelsMap pdata.StringMap, timestamp pdata.Timestamp, startTimestamp pdata.Timestamp) string {
	idx, otherLabels := 0, make([]string, labelsMap.Len())
	labelsMap.Range(func(k string, v string) bool {
		otherLabels[idx] = k + "=" + v
		idx++
		return true
	})
	// sort the slice so that we consider labelsets
	// the same regardless of order
	sort.StringSlice(otherLabels).Sort()
	return metricName + "/" + startTimestamp.String() + "-" + timestamp.String() + "/" + strings.Join(otherLabels, ";")
}
