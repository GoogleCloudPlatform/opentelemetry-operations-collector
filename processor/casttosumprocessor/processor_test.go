// Copyright 2022 Google LLC
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

package casttosumprocessor

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
	"go.opentelemetry.io/collector/model/otlp"
	"go.opentelemetry.io/collector/model/pdata"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.uber.org/zap"
)

type testCase struct {
	name     string
	inputs   []pdata.Metrics
	expected []pdata.Metrics
}

func TestCastToSumProcessor(t *testing.T) {
	testStart := time.Now().Unix()
	tests := []testCase{
		{
			name:     "no-transform-case",
			inputs:   generateNoTransformMetrics(testStart),
			expected: generateNoTransformMetrics(testStart),
		},
		{
			name:     "non-monotonic-metric-case",
			inputs:   generateNonMonotonicInput(testStart),
			expected: generateNonMonotonicOutput(testStart),
		},
		{
			name:     "labeled-input-mixed-case",
			inputs:   generateLabeledInput(testStart),
			expected: generateLabeledOutput(testStart),
		},
		{
			name:     "labeled-input-separated-case",
			inputs:   generateSeparatedLabeledInput(testStart),
			expected: generateSeparatedLabeledOutput(testStart),
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
		{
			name:     "gauge-metric-case",
			inputs:   generateIncludedGaugeInput(testStart),
			expected: generateIncludedGaugeOutput(testStart),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := config.NewProcessorSettings(config.NewComponentID(typeStr))
			cfg := &Config{
				ProcessorSettings: &settings,
				Metrics:           []string{"m1", "m2"},
			}
			nsp := newCastToSumProcessor(cfg, zap.NewExample())

			tmn := &consumertest.MetricsSink{}
			rmp, err := processorhelper.NewMetricsProcessor(
				cfg,
				tmn,
				nsp.ProcessMetrics,
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

	mb1 := b.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime+1000, startTime)
	mb1.addIntDataPoint(5, map[string]string{}, startTime+2000, startTime)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+3000, startTime+2000)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeSum, true)
	mb2.addDoubleDataPoint(3, map[string]string{}, startTime+6000, startTime)
	mb2.addDoubleDataPoint(4, map[string]string{}, startTime+7000, startTime)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeGauge, false)
	mb3.addIntDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addIntDataPoint(4, map[string]string{}, startTime+1000, 0)

	mb4 := b.addMetric("m4", pdata.MetricDataTypeGauge, false)
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

	mb1 := b.addMetric("m1", pdata.MetricDataTypeGauge, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+1000, 0)

	b2 := rmb.addResourceMetrics(map[string]pdata.AttributeValue{
		"label1": pdata.NewAttributeValueString("value2"),
	})

	mb2 := b2.addMetric("m1", pdata.MetricDataTypeGauge, true)
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

	mb1 := b.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+1000, 0)

	b2 := rmb.addResourceMetrics(map[string]pdata.AttributeValue{
		"label1": pdata.NewAttributeValueString("value2"),
	})

	mb2 := b2.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb2.addIntDataPoint(5, map[string]string{}, startTime+2000, 0)
	mb2.addIntDataPoint(10, map[string]string{}, startTime+3000, 0)

	rmb.Build().CopyTo(output.ResourceMetrics())
	return []pdata.Metrics{output}
}

func generateLabeledInput(startTime int64) []pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeGauge, true)
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

func generateLabeledOutput(startTime int64) []pdata.Metrics {
	output := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb1.addIntDataPoint(0, map[string]string{"label": "val1"}, startTime, 0)
	mb1.addIntDataPoint(3, map[string]string{"label": "val2"}, startTime, 0)
	mb1.addIntDataPoint(12, map[string]string{"label": "val1"}, startTime+1000, 0)
	mb1.addIntDataPoint(5, map[string]string{"label": "val2"}, startTime+1000, 0)
	mb1.addIntDataPoint(15, map[string]string{"label": "val1"}, startTime+2000, 0)
	mb1.addIntDataPoint(1, map[string]string{"label": "val2"}, startTime+2000, 0)
	mb1.addIntDataPoint(22, map[string]string{"label": "val1"}, startTime+3000, 0)
	mb1.addIntDataPoint(11, map[string]string{"label": "val2"}, startTime+3000, 0)

	rmb.Build().CopyTo(output.ResourceMetrics())
	return []pdata.Metrics{output}
}

func generateSeparatedLabeledInput(startTime int64) []pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeGauge, true)
	mb1.addIntDataPoint(0, map[string]string{"label": "val1"}, startTime, 0)
	mb1.addIntDataPoint(12, map[string]string{"label": "val1"}, startTime+1000, 0)
	mb1.addIntDataPoint(15, map[string]string{"label": "val1"}, startTime+2000, 0)
	mb1.addIntDataPoint(22, map[string]string{"label": "val1"}, startTime+3000, 0)

	mb2 := b.addMetric("m1", pdata.MetricDataTypeGauge, true)
	mb2.addIntDataPoint(3, map[string]string{"label": "val2"}, startTime, 0)
	mb2.addIntDataPoint(5, map[string]string{"label": "val2"}, startTime+1000, 0)
	mb2.addIntDataPoint(1, map[string]string{"label": "val2"}, startTime+2000, 0)
	mb2.addIntDataPoint(11, map[string]string{"label": "val2"}, startTime+3000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	return []pdata.Metrics{input}
}

func generateSeparatedLabeledOutput(startTime int64) []pdata.Metrics {
	output := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb1.addIntDataPoint(0, map[string]string{"label": "val1"}, startTime, 0)
	mb1.addIntDataPoint(12, map[string]string{"label": "val1"}, startTime+1000, 0)
	mb1.addIntDataPoint(15, map[string]string{"label": "val1"}, startTime+2000, 0)
	mb1.addIntDataPoint(22, map[string]string{"label": "val1"}, startTime+3000, 0)

	mb2 := b.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb2.addIntDataPoint(3, map[string]string{"label": "val2"}, startTime, 0)
	mb2.addIntDataPoint(5, map[string]string{"label": "val2"}, startTime+1000, 0)
	mb2.addIntDataPoint(1, map[string]string{"label": "val2"}, startTime+2000, 0)
	mb2.addIntDataPoint(11, map[string]string{"label": "val2"}, startTime+3000, 0)

	rmb.Build().CopyTo(output.ResourceMetrics())
	return []pdata.Metrics{output}
}

func generateNonMonotonicInput(startTime int64) []pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeSum, false)
	mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(5, map[string]string{}, startTime+1000, 0)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeGauge, false)
	mb2.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(4, map[string]string{}, startTime+1000, 0)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeGauge, false)
	mb3.addDoubleDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addDoubleDataPoint(6, map[string]string{}, startTime+1000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	return []pdata.Metrics{input}
}

func generateNonMonotonicOutput(startTime int64) []pdata.Metrics {
	output := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(5, map[string]string{}, startTime+1000, 0)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeSum, true)
	mb2.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(4, map[string]string{}, startTime+1000, 0)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeGauge, false)
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

	mb1 := b.addMetric("m1", pdata.MetricDataTypeGauge, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+1000, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+2000, 0)
	mb1.addIntDataPoint(5, map[string]string{}, startTime+3000, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+4000, 0)
	mb1.addIntDataPoint(4, map[string]string{}, startTime+5000, 0)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeSum, false)
	mb2.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(4, map[string]string{}, startTime+1000, 0)
	mb2.addDoubleDataPoint(5, map[string]string{}, startTime+2000, 0)
	mb2.addDoubleDataPoint(2, map[string]string{}, startTime+500, 0)
	mb2.addDoubleDataPoint(8, map[string]string{}, startTime+3000, 0)
	mb2.addDoubleDataPoint(2, map[string]string{}, startTime+10000, 0)
	mb2.addDoubleDataPoint(6, map[string]string{}, startTime+120000, 0)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeGauge, false)
	mb3.addDoubleDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addDoubleDataPoint(6, map[string]string{}, startTime+1000, 0)

	mb4 := b.addMetric("m4", pdata.MetricDataTypeSum, false)
	mb4.addDoubleDataPoint(12, map[string]string{}, startTime, 0)
	mb4.addDoubleDataPoint(13, map[string]string{}, startTime+2000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	list = append(list, input)

	input = pdata.NewMetrics()
	rmb = newResourceMetricsBuilder()
	b = rmb.addResourceMetrics(nil)

	mb1 = b.addMetric("m1", pdata.MetricDataTypeGauge, true)
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

	mb1 := b.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb1.addIntDataPoint(1, map[string]string{}, startTime, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+1000, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+2000, 0)
	mb1.addIntDataPoint(5, map[string]string{}, startTime+3000, 0)
	mb1.addIntDataPoint(2, map[string]string{}, startTime+4000, 0)
	mb1.addIntDataPoint(4, map[string]string{}, startTime+5000, 0)

	mb2 := b.addMetric("m2", pdata.MetricDataTypeSum, true)
	mb2.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb2.addDoubleDataPoint(4, map[string]string{}, startTime+1000, 0)
	mb2.addDoubleDataPoint(5, map[string]string{}, startTime+2000, 0)
	mb2.addDoubleDataPoint(2, map[string]string{}, startTime+500, 0)
	mb2.addDoubleDataPoint(8, map[string]string{}, startTime+3000, 0)
	mb2.addDoubleDataPoint(2, map[string]string{}, startTime+10000, 0)
	mb2.addDoubleDataPoint(6, map[string]string{}, startTime+120000, 0)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeGauge, false)
	mb3.addDoubleDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addDoubleDataPoint(6, map[string]string{}, startTime+1000, 0)

	mb4 := b.addMetric("m4", pdata.MetricDataTypeSum, false)
	mb4.addDoubleDataPoint(12, map[string]string{}, startTime, 0)
	mb4.addDoubleDataPoint(13, map[string]string{}, startTime+2000, 0)

	rmb.Build().CopyTo(output.ResourceMetrics())
	list = append(list, output)

	output = pdata.NewMetrics()

	rmb = newResourceMetricsBuilder()
	b = rmb.addResourceMetrics(nil)

	mb1 = b.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb1.addIntDataPoint(7, map[string]string{}, startTime+6000, 0)
	mb1.addIntDataPoint(9, map[string]string{}, startTime+7000, 0)

	rmb.Build().CopyTo(output.ResourceMetrics())
	list = append(list, output)

	return list
}

func generateIncludedGaugeInput(startTime int64) []pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeGauge, false)
	mb1.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb1.addDoubleDataPoint(4, map[string]string{}, startTime+1000, 0)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeGauge, false)
	mb3.addDoubleDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addDoubleDataPoint(6, map[string]string{}, startTime+1000, 0)

	rmb.Build().CopyTo(input.ResourceMetrics())
	return []pdata.Metrics{input}
}

func generateIncludedGaugeOutput(startTime int64) []pdata.Metrics {
	output := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pdata.MetricDataTypeSum, true)
	mb1.addDoubleDataPoint(3, map[string]string{}, startTime, 0)
	mb1.addDoubleDataPoint(4, map[string]string{}, startTime+1000, 0)

	mb3 := b.addMetric("m3", pdata.MetricDataTypeGauge, false)
	mb3.addDoubleDataPoint(5, map[string]string{}, startTime, 0)
	mb3.addDoubleDataPoint(6, map[string]string{}, startTime+1000, 0)

	rmb.Build().CopyTo(output.ResourceMetrics())
	return []pdata.Metrics{output}
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

	for k, v := range resourceAttributes {
		rm.Resource().Attributes().Insert(k, v)
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
	case pdata.MetricDataTypeSum:
		sum := metric.Sum()
		sum.SetIsMonotonic(isMonotonic)
		sum.SetAggregationTemporality(pdata.MetricAggregationTemporalityCumulative)
	case pdata.MetricDataTypeGauge:
		metric.Gauge()
	}

	return metricBuilder{metric: metric}
}

type metricBuilder struct {
	metric pdata.Metric
}

func (mb metricBuilder) addDoubleDataPoint(value float64, labels map[string]string, timestamp int64, startTimestamp int64) {
	var ddp pdata.NumberDataPoint
	switch mb.metric.DataType() {
	case pdata.MetricDataTypeSum:
		ddp = mb.metric.Sum().DataPoints().AppendEmpty()
	case pdata.MetricDataTypeGauge:
		ddp = mb.metric.Gauge().DataPoints().AppendEmpty()
	}
	for k, v := range labels {
		ddp.Attributes().InsertString(k, v)
	}
	ddp.SetDoubleVal(value)
	ddp.SetTimestamp(pdata.NewTimestampFromTime(time.Unix(timestamp, 0)))
	if startTimestamp > 0 {
		ddp.SetStartTimestamp(pdata.NewTimestampFromTime(time.Unix(startTimestamp, 0)))
	}
}

func (mb metricBuilder) addIntDataPoint(value int64, labels map[string]string, timestamp int64, startTimestamp int64) {
	var idp pdata.NumberDataPoint
	switch mb.metric.DataType() {
	case pdata.MetricDataTypeSum:
		idp = mb.metric.Sum().DataPoints().AppendEmpty()
	case pdata.MetricDataTypeGauge:
		idp = mb.metric.Gauge().DataPoints().AppendEmpty()
	}
	for k, v := range labels {
		idp.Attributes().InsertString(k, v)
	}
	idp.SetIntVal(value)
	idp.SetTimestamp(pdata.NewTimestampFromTime(time.Unix(timestamp, 0)))
	if startTimestamp > 0 {
		idp.SetStartTimestamp(pdata.NewTimestampFromTime(time.Unix(startTimestamp, 0)))
	}
}

// requireEqual is required because Attribute & Label Maps are not sorted by default
// and we don't provide any guarantees on the order of transformed metrics
func requireEqual(t *testing.T, expected, actual []pdata.Metrics) {
	require.Equal(t, len(expected), len(actual))

	marshaler := otlp.NewJSONMetricsMarshaler()

	for q := 0; q < len(actual); q++ {
		outJSON, err := marshaler.MarshalMetrics(actual[q])
		require.NoError(t, err)
		t.Logf("actual metrics %d: %s", q, outJSON)
		outJSON, err = marshaler.MarshalMetrics(expected[q])
		require.NoError(t, err)
		t.Logf("expected metrics %d: %s", q, outJSON)

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
					case pdata.MetricDataTypeSum:
						require.Equal(t, metricAct.Sum().AggregationTemporality(), metricExp.Sum().AggregationTemporality(), "Metric %s", metricAct.Name())
						require.Equal(t, metricAct.Sum().IsMonotonic(), metricExp.Sum().IsMonotonic(), "Metric %s", metricAct.Name())
						requireEqualNumberDataPointSlice(t, metricAct.Name(), metricAct.Sum().DataPoints(), metricExp.Sum().DataPoints())
					case pdata.MetricDataTypeGauge:
						requireEqualNumberDataPointSlice(t, metricAct.Name(), metricAct.Gauge().DataPoints(), metricExp.Gauge().DataPoints())
					default:
						require.Fail(t, "unexpected metric type", t)
					}
				}
			}
		}
	}
}

func requireEqualNumberDataPointSlice(t *testing.T, metricName string, ndpsAct, ndpsExp pdata.NumberDataPointSlice) {
	require.Equalf(t, ndpsExp.Len(), ndpsAct.Len(), "Metric %s", metricName)

	// build a map of expected data points
	ndpsExpMap := make(map[string]pdata.NumberDataPoint, ndpsExp.Len())
	for k := 0; k < ndpsExp.Len(); k++ {
		ndpExp := ndpsExp.At(k)
		ndpsExpMap[dataPointKey(metricName, ndpExp.Attributes(), ndpExp.Timestamp(), ndpExp.StartTimestamp())] = ndpExp
	}

	for l := 0; l < ndpsAct.Len(); l++ {
		ndpAct := ndpsAct.At(l)
		dpKey := dataPointKey(metricName, ndpAct.Attributes(), ndpAct.Timestamp(), ndpAct.StartTimestamp())

		ndpExp, ok := ndpsExpMap[dpKey]
		if !ok {
			require.Failf(t, fmt.Sprintf("no data point for %s", dpKey), "Metric %s", metricName)
		}

		require.Equalf(t, ndpExp.Attributes().Sort(), ndpAct.Attributes().Sort(), "Metric %s", metricName)
		require.Equalf(t, ndpExp.StartTimestamp(), ndpAct.StartTimestamp(), "Metric %s", metricName)
		require.Equalf(t, ndpExp.Timestamp(), ndpAct.Timestamp(), "Metric %s", metricName)
		require.Equalf(t, ndpExp.Type(), ndpAct.Type(), "Metric %s", metricName)
		switch ndpExp.Type() {
		case pdata.MetricValueTypeInt:
			require.Equalf(t, ndpExp.IntVal(), ndpAct.IntVal(), "Metric %s", metricName)
		case pdata.MetricValueTypeDouble:
			require.Equalf(t, ndpExp.DoubleVal(), ndpAct.DoubleVal(), "Metric %s", metricName)
		}
	}
}

// dataPointKey returns a key representing the data point
func dataPointKey(metricName string, labelsMap pdata.AttributeMap, timestamp pdata.Timestamp, startTimestamp pdata.Timestamp) string {
	idx, otherLabels := 0, make([]string, labelsMap.Len())
	labelsMap.Range(func(k string, v pdata.AttributeValue) bool {
		otherLabels[idx] = k + "=" + v.AsString()
		idx++
		return true
	})
	// sort the slice so that we consider labelsets
	// the same regardless of order
	sort.StringSlice(otherLabels).Sort()
	return metricName + "/" + startTimestamp.String() + "-" + timestamp.String() + "/" + strings.Join(otherLabels, ";")
}
