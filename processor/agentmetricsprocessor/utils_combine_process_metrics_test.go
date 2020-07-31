// Copyright 2020 OpenTelemetry Authors
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
	"go.opentelemetry.io/collector/consumer/pdatautil"
)

func generateProcessResourceMetricsInput() pdata.Metrics {
	input := pdatautil.MetricsToInternalMetrics(newInternalMetrics())

	rmb := newResourceMetricsBuilder()
	b1 := rmb.addResourceMetrics(nil)
	b1.addMetric("m1", pdata.MetricTypeInt64).addInt64DataPoint(1, map[string]string{"label1": "value1"})
	b1.addMetric("m2", pdata.MetricTypeDouble).addDoubleDataPoint(2, map[string]string{"label1": "value1"})

	b2 := rmb.addResourceMetrics(map[string]pdata.AttributeValue{
		"process.id":              pdata.NewAttributeValueInt(1),
		"process.executable.name": pdata.NewAttributeValueString("process1"),
		"process.executable.path": pdata.NewAttributeValueString("/path/to/process1"),
		"process.command":         pdata.NewAttributeValueString("to/process1"),
		"process.command_line":    pdata.NewAttributeValueString("to/process1 -arg arg"),
		"process.username":        pdata.NewAttributeValueString("username1"),
	})
	b2.addMetric("m3", pdata.MetricTypeInt64).addInt64DataPoint(3, map[string]string{"label1": "value1"})
	b2.addMetric("m4", pdata.MetricTypeDouble).addDoubleDataPoint(4, map[string]string{"label1": "value1"})

	b3 := rmb.addResourceMetrics(map[string]pdata.AttributeValue{
		"process.id":              pdata.NewAttributeValueInt(2),
		"process.executable.name": pdata.NewAttributeValueString("process2"),
		"process.executable.path": pdata.NewAttributeValueString("/path/to/process2"),
		"process.command":         pdata.NewAttributeValueString("to/process2"),
		"process.command_line":    pdata.NewAttributeValueString("to/process2 -arg arg"),
		"process.username":        pdata.NewAttributeValueString("username2"),
	})
	b3.addMetric("m3", pdata.MetricTypeInt64).addInt64DataPoint(5, map[string]string{"label1": "value2"})
	b3.addMetric("m4", pdata.MetricTypeDouble).addDoubleDataPoint(6, map[string]string{"label1": "value2"})

	rmb.Build().CopyTo(input.ResourceMetrics())
	return pdatautil.MetricsFromInternalMetrics(input)
}

func generateProcessResourceMetricsExpected() pdata.Metrics {
	expected := pdatautil.MetricsToInternalMetrics(newInternalMetrics())

	rmb := newResourceMetricsBuilder()
	b := rmb.addResourceMetrics(nil)

	b.addMetric("m1", pdata.MetricTypeInt64).addInt64DataPoint(1, map[string]string{"label1": "value1"})
	b.addMetric("m2", pdata.MetricTypeDouble).addDoubleDataPoint(2, map[string]string{"label1": "value1"})

	mb3 := b.addMetric("m3", pdata.MetricTypeInt64)
	mb3.addInt64DataPoint(3, map[string]string{
		"label1":       "value1",
		"pid":          "1",
		"command":      "process1",
		"command_line": "to/process1 -arg arg",
		"owner":        "username1",
	})
	mb3.addInt64DataPoint(5, map[string]string{
		"label1":       "value2",
		"pid":          "2",
		"command":      "process2",
		"command_line": "to/process2 -arg arg",
		"owner":        "username2",
	})

	mb4 := b.addMetric("m4", pdata.MetricTypeDouble)
	mb4.addDoubleDataPoint(4, map[string]string{
		"label1":       "value1",
		"pid":          "1",
		"command":      "process1",
		"command_line": "to/process1 -arg arg",
		"owner":        "username1",
	})
	mb4.addDoubleDataPoint(6, map[string]string{
		"label1":       "value2",
		"pid":          "2",
		"command":      "process2",
		"command_line": "to/process2 -arg arg",
		"owner":        "username2",
	})

	rmb.Build().CopyTo(expected.ResourceMetrics())
	return pdatautil.MetricsFromInternalMetrics(expected)
}
