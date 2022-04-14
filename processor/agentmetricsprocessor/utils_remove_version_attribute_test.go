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

package agentmetricsprocessor

import (
	"go.opentelemetry.io/collector/model/pdata"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func generateVersionInput() pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pmetric.MetricDataTypeSum, true)
	mb1.addIntDataPoint(2, map[string]string{"service_version": "value2"})

	mb2 := b.addMetric("m2", pmetric.MetricDataTypeGauge, false)
	mb2.addDoubleDataPoint(3, map[string]string{"service_version": "value1"})

	rmb.Build().CopyTo(input.ResourceMetrics())
	return input
}

func generateVersionExpected() pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pmetric.MetricDataTypeSum, true)
	mb1.addIntDataPoint(2, map[string]string{})

	mb2 := b.addMetric("m2", pmetric.MetricDataTypeGauge, false)
	mb2.addDoubleDataPoint(3, map[string]string{})

	rmb.Build().CopyTo(input.ResourceMetrics())
	return input
}

func generateMultiAttrVersionInput() pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pmetric.MetricDataTypeSum, true)
	mb1.addIntDataPoint(2, map[string]string{"service_version": "value2", "other_attr": "value2"})

	mb2 := b.addMetric("m2", pmetric.MetricDataTypeGauge, false)
	mb2.addDoubleDataPoint(3, map[string]string{"service_version": "value1", "other_attr": "value1"})

	rmb.Build().CopyTo(input.ResourceMetrics())
	return input
}

func generateMultiAttrVersionExpected() pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("m1", pmetric.MetricDataTypeSum, true)
	mb1.addIntDataPoint(2, map[string]string{"other_attr": "value2"})

	mb2 := b.addMetric("m2", pmetric.MetricDataTypeGauge, false)
	mb2.addDoubleDataPoint(3, map[string]string{"other_attr": "value1"})

	rmb.Build().CopyTo(input.ResourceMetrics())
	return input
}
