// Copyright 2021, Google Inc.
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
	"strings"

	"go.opentelemetry.io/collector/consumer/pdata"
)

func cleanCpuNumber(rms pdata.ResourceMetricsSlice) error {
	for i := 0; i < rms.Len(); i++ {
		ilms := rms.At(i).InstrumentationLibraryMetrics()
		for j := 0; j < ilms.Len(); j++ {
			metrics := ilms.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)

				if err := forEachPoint(metric, cleanCpuNumberDataPoint); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

type labelsMapper interface {
	LabelsMap() pdata.StringMap
}

func forEachPoint(metric pdata.Metric, fn func(labelsMapper) error) error {
	switch t := metric.DataType(); t {
	case pdata.MetricDataTypeIntSum:
		dp := metric.IntSum().DataPoints()
		for i := 0; i < dp.Len(); i++ {
			if err := fn(dp.At(i)); err != nil {
				return err
			}
		}
	case pdata.MetricDataTypeDoubleSum:
		dp := metric.DoubleSum().DataPoints()
		for i := 0; i < dp.Len(); i++ {
			if err := fn(dp.At(i)); err != nil {
				return err
			}
		}
	case pdata.MetricDataTypeIntGauge:
		dp := metric.IntGauge().DataPoints()
		for i := 0; i < dp.Len(); i++ {
			if err := fn(dp.At(i)); err != nil {
				return err
			}
		}
	case pdata.MetricDataTypeDoubleGauge:
		dp := metric.DoubleGauge().DataPoints()
		for i := 0; i < dp.Len(); i++ {
			if err := fn(dp.At(i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func cleanCpuNumberDataPoint(lm labelsMapper) error {
	sm := lm.LabelsMap()
	if value, ok := sm.Get("cpu"); ok {
		sm.Update("cpu", strings.TrimPrefix(value, "cpu"))
	}
	return nil
}
