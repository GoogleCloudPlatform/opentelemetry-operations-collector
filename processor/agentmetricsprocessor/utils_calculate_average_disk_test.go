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
	"go.opentelemetry.io/collector/consumer/pdata"
)

func generateAverageDiskInput() pdata.Metrics {
	input := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("system.disk.operation_time", pdata.MetricDataTypeDoubleSum, true)
	mb1.addDoubleDataPoint(200, map[string]string{"device": "hda", "direction": "read"})
	mb1.addDoubleDataPoint(400, map[string]string{"device": "hda", "direction": "write"})
	mb1.addDoubleDataPoint(100, map[string]string{"device": "hdb", "direction": "read"})
	mb1.addDoubleDataPoint(100, map[string]string{"device": "hdb", "direction": "write"})

	mb2 := b.addMetric("system.disk.operations", pdata.MetricDataTypeIntSum, true)
	mb2.addIntDataPoint(5, map[string]string{"device": "hda", "direction": "read"})
	mb2.addIntDataPoint(4, map[string]string{"device": "hda", "direction": "write"})
	mb2.addIntDataPoint(2, map[string]string{"device": "hdb", "direction": "read"})
	mb2.addIntDataPoint(20, map[string]string{"device": "hdb", "direction": "write"})

	rmb.Build().CopyTo(input.ResourceMetrics())
	return input
}

func generateAverageDiskPrevOpInput() map[opKey]opData {
	return map[opKey]opData{}
}

func generateAverageDiskExpected() pdata.Metrics {
	expected := pdata.NewMetrics()

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	mb1 := b.addMetric("system.disk.operation_time", pdata.MetricDataTypeDoubleSum, true)
	mb1.addDoubleDataPoint(200, map[string]string{"device": "hda", "direction": "read"})
	mb1.addDoubleDataPoint(400, map[string]string{"device": "hda", "direction": "write"})
	mb1.addDoubleDataPoint(100, map[string]string{"device": "hdb", "direction": "read"})
	mb1.addDoubleDataPoint(100, map[string]string{"device": "hdb", "direction": "write"})

	mb2 := b.addMetric("system.disk.operations", pdata.MetricDataTypeIntSum, true)
	mb2.addIntDataPoint(5, map[string]string{"device": "hda", "direction": "read"})
	mb2.addIntDataPoint(4, map[string]string{"device": "hda", "direction": "write"})
	mb2.addIntDataPoint(2, map[string]string{"device": "hdb", "direction": "read"})
	mb2.addIntDataPoint(20, map[string]string{"device": "hdb", "direction": "write"})

	mb3 := b.addMetric("system.disk.average_operation_time", pdata.MetricDataTypeDoubleSum, true)
	mb3.addDoubleDataPoint(40, map[string]string{"device": "hda", "direction": "read"})
	mb3.addDoubleDataPoint(100, map[string]string{"device": "hda", "direction": "write"})
	mb3.addDoubleDataPoint(50, map[string]string{"device": "hdb", "direction": "read"})
	mb3.addDoubleDataPoint(5, map[string]string{"device": "hdb", "direction": "write"})

	rmb.Build().CopyTo(expected.ResourceMetrics())
	return expected
}
