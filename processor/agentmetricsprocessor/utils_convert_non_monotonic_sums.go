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
	"go.opentelemetry.io/collector/consumer/pdata"
)

// The following code converts metrics of OpenTelemetry Counter (sum) type
// that are non-Monotonic to Gauges.

func convertNonMonotonicSumsToGauges(metrics pdata.Metrics) {
	rms := metrics.ResourceMetrics()

	for i := 0; i < rms.Len(); i++ {
		ilms := rms.At(i).InstrumentationLibraryMetrics()
		for j := 0; j < ilms.Len(); j++ {
			metrics := ilms.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)

				// ignore all metrics that are not counter (sum) types
				var isMonotonic bool
				switch t := metric.DataType(); t {
				case pdata.MetricDataTypeIntSum:
					isMonotonic = metric.IntSum().IsMonotonic()
				case pdata.MetricDataTypeDoubleSum:
					isMonotonic = metric.DoubleSum().IsMonotonic()
				default:
					continue
				}

				// ignore all metrics that are monotonic
				if isMonotonic {
					continue
				}

				// convert to gauge
				convertToGauge(metric)
			}
		}
	}
}

func convertToGauge(metric pdata.Metric) {
	switch t := metric.DataType(); t {
	case pdata.MetricDataTypeIntSum:
		idps := metric.IntSum().DataPoints()
		metric.SetDataType(pdata.MetricDataTypeIntGauge)
		idps.CopyTo(metric.IntGauge().DataPoints())
	case pdata.MetricDataTypeDoubleSum:
		ddps := metric.DoubleSum().DataPoints()
		metric.SetDataType(pdata.MetricDataTypeDoubleGauge)
		ddps.CopyTo(metric.DoubleGauge().DataPoints())
	}
}
