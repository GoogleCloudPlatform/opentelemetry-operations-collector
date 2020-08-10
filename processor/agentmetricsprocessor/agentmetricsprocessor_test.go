// Copyright 2020, Google Inc.
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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer/pdata"
	"go.opentelemetry.io/collector/consumer/pdatautil"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/receiver/hostmetricsreceiver"
	"go.uber.org/zap"
)

type testCase struct {
	name                      string
	input                     pdata.Metrics
	expected                  pdata.Metrics
	prevCPUTimeValuesInput    map[string]float64
	prevCPUTimeValuesExpected map[string]float64
}

func TestAgentMetricsProcessor(t *testing.T) {
	tests := []testCase{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := &Factory{}
			tmn := &exportertest.SinkMetricsExporter{}
			rmp, err := factory.CreateMetricsProcessor(context.Background(), component.ProcessorCreateParams{Logger: zap.NewNop()}, tmn, &Config{})
			require.NoError(t, err)

			assert.True(t, rmp.GetCapabilities().MutatesConsumedData)

			rmp.(*agentMetricsProcessor).prevCPUTimeValues = tt.prevCPUTimeValuesInput
			require.NoError(t, rmp.Start(context.Background(), componenttest.NewNopHost()))
			defer func() { assert.NoError(t, rmp.Shutdown(context.Background())) }()

			err = rmp.ConsumeMetrics(context.Background(), tt.input)
			require.NoError(t, err)

			assertEqual(t, tt.expected, tmn.AllMetrics()[0])
			assert.Equal(t, tt.prevCPUTimeValuesExpected, rmp.(*agentMetricsProcessor).prevCPUTimeValues)
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
	rm.InitEmpty()

	if resourceAttributes != nil {
		rm.Resource().InitEmpty()
		rm.Resource().Attributes().InitFromMap(resourceAttributes)
	}

	rm.InstrumentationLibraryMetrics().Resize(1)
	ilm := rm.InstrumentationLibraryMetrics().At(0)
	ilm.InitEmpty()

	rmsb.rms.Append(&rm)
	return metricsBuilder{metrics: ilm.Metrics()}
}

func (rmsb resourceMetricsBuilder) Build() pdata.ResourceMetricsSlice {
	return rmsb.rms
}

type metricsBuilder struct {
	metrics pdata.MetricSlice
}

func (msb metricsBuilder) addMetric(name string, t pdata.MetricType) metricBuilder {
	metric := pdata.NewMetric()
	metric.InitEmpty()
	metric.MetricDescriptor().InitEmpty()
	metric.MetricDescriptor().SetName(name)
	metric.MetricDescriptor().SetType(t)

	msb.metrics.Append(&metric)
	return metricBuilder{metric: metric}
}

type metricBuilder struct {
	metric pdata.Metric
}

func (mb metricBuilder) addInt64DataPoint(value int64, labels map[string]string) metricBuilder {
	idp := pdata.NewInt64DataPoint()
	idp.InitEmpty()
	idp.LabelsMap().InitFromMap(labels)
	idp.SetValue(value)

	mb.metric.Int64DataPoints().Append(&idp)
	return mb
}

func (mb metricBuilder) addDoubleDataPoint(value float64, labels map[string]string) metricBuilder {
	idp := pdata.NewDoubleDataPoint()
	idp.InitEmpty()
	idp.LabelsMap().InitFromMap(labels)
	idp.SetValue(value)

	mb.metric.DoubleDataPoints().Append(&idp)
	return mb
}

// assertEqual is required because Attribute & Label Maps are not sorted by default
// and we don't provide any guarantees on the order of transformed metrics
func assertEqual(t *testing.T, expected, actual pdata.Metrics) {
	rmsAct := pdatautil.MetricsToInternalMetrics(actual).ResourceMetrics()
	rmsExp := pdatautil.MetricsToInternalMetrics(expected).ResourceMetrics()
	require.Equal(t, rmsExp.Len(), rmsAct.Len())
	for i := 0; i < rmsAct.Len(); i++ {
		rmAct := rmsAct.At(i)
		rmExp := rmsExp.At(i)

		// assert equality of resource attributes
		assert.Equal(t, rmExp.Resource().IsNil(), rmAct.Resource().IsNil())
		if !rmExp.Resource().IsNil() {
			assert.Equal(t, rmExp.Resource().Attributes().Sort(), rmAct.Resource().Attributes().Sort())
		}

		// assert equality of IL metrics
		ilmsAct := rmAct.InstrumentationLibraryMetrics()
		ilmsExp := rmExp.InstrumentationLibraryMetrics()
		require.Equal(t, ilmsExp.Len(), ilmsAct.Len())
		for j := 0; j < ilmsAct.Len(); j++ {
			ilmAct := ilmsAct.At(j)
			ilmExp := ilmsExp.At(j)

			// currently expect IL to always be nil
			assert.True(t, ilmAct.InstrumentationLibrary().IsNil())
			assert.True(t, ilmExp.InstrumentationLibrary().IsNil())

			// assert equality of metrics
			metricsAct := ilmAct.Metrics()
			metricsExp := ilmExp.Metrics()
			require.Equal(t, metricsExp.Len(), metricsAct.Len())

			// build a map of expected metrics
			metricsExpMap := make(map[string]pdata.Metric, metricsExp.Len())
			for k := 0; k < metricsExp.Len(); k++ {
				metricsExpMap[metricsExp.At(k).MetricDescriptor().Name()] = metricsExp.At(k)
			}

			for k := 0; k < metricsAct.Len(); k++ {
				metricAct := metricsAct.At(k)
				metricExp, ok := metricsExpMap[metricAct.MetricDescriptor().Name()]
				if !ok {
					require.Fail(t, fmt.Sprintf("unexpected metric %v", metricAct.MetricDescriptor().Name()))
				}

				// assert equality of descriptors
				assert.Equal(t, metricExp.MetricDescriptor(), metricAct.MetricDescriptor())

				// assert equality of int data points
				idpsAct := metricAct.Int64DataPoints()
				idpsExp := metricExp.Int64DataPoints()
				require.Equal(t, idpsExp.Len(), idpsAct.Len())
				for l := 0; l < idpsAct.Len(); l++ {
					idpAct := idpsAct.At(l)
					idpExp := idpsExp.At(l)

					assert.Equal(t, idpExp.LabelsMap().Sort(), idpAct.LabelsMap().Sort())
					assert.Equal(t, idpExp.StartTime(), idpAct.StartTime())
					assert.Equal(t, idpExp.Timestamp(), idpAct.Timestamp())
					assert.Equal(t, idpExp.Value(), idpAct.Value())
				}

				// assert equality of double data points
				ddpsAct := metricAct.DoubleDataPoints()
				ddpsExp := metricExp.DoubleDataPoints()
				require.Equal(t, ddpsExp.Len(), ddpsAct.Len())
				for l := 0; l < ddpsAct.Len(); l++ {
					ddpAct := ddpsAct.At(l)
					ddpExp := ddpsExp.At(l)

					assert.Equal(t, ddpExp.LabelsMap().Sort(), ddpAct.LabelsMap().Sort())
					assert.Equal(t, ddpExp.StartTime(), ddpAct.StartTime())
					assert.Equal(t, ddpExp.Timestamp(), ddpAct.Timestamp())
					assert.InDelta(t, ddpExp.Value(), ddpAct.Value(), 0.00000001)
				}

				// currently expect other kinds of data points to always be empty
				assert.True(t, metricAct.HistogramDataPoints().Len() == 0)
				assert.True(t, metricExp.HistogramDataPoints().Len() == 0)
				assert.True(t, metricAct.SummaryDataPoints().Len() == 0)
				assert.True(t, metricExp.SummaryDataPoints().Len() == 0)
			}
		}
	}
}

var cached *pdata.Metrics

// a very dirty hack to get an internal.Data object since we shouldn't be using it yet,
// but its needed to construct the resource tests
func newInternalMetrics() pdata.Metrics {
	if cached == nil {
		f := hostmetricsreceiver.NewFactory()
		c := &hostmetricsreceiver.Config{CollectionInterval: 3 * time.Millisecond}
		s := &exportertest.SinkMetricsExporter{}
		r, _ := f.CreateMetricsReceiver(context.Background(), component.ReceiverCreateParams{Logger: zap.NewNop()}, c, s)
		_ = r.Start(context.Background(), componenttest.NewNopHost())
		time.Sleep(10 * time.Millisecond)
		_ = r.Shutdown(context.Background())
		md := s.AllMetrics()[0]
		cached = &md
	}

	return pdatautil.MetricsFromInternalMetrics(pdatautil.MetricsToInternalMetrics(*cached).Clone())
}
