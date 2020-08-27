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
	"fmt"

	"go.opentelemetry.io/collector/consumer/pdata"
)

// The following code splits metrics with read/write direction labels into
// two separate metrics.
//
// Starting format:
//
// +-----------------------------------------------------+
// |                     metric                          |
// +---------+---+---------+------------+---+------------+
// |dp1{read}|...|dpN{read}|dpN+1{write}|...|dpN+N{write}|
// +---------+---+---------+------------+---+------------+
//
// Converted format:
//
// +-----------+ +---------------+
// |read_metric| | write_metric  |
// +---+---+---+ +-----+---+-----+
// |dp1|...|dpN| |dpN+1|...|dpN+N|
// +---+---+---+ +-----+---+-----+

const (
	hostDiskBytes    = "system.disk.io"
	processDiskBytes = "process.disk.io"
)

var metricsToSplit = map[string]bool{
	hostDiskBytes:    true,
	processDiskBytes: true,
}

func splitReadWriteBytesMetrics(rms pdata.ResourceMetricsSlice) error {
	for i := 0; i < rms.Len(); i++ {
		ilms := rms.At(i).InstrumentationLibraryMetrics()
		for j := 0; j < ilms.Len(); j++ {
			metrics := ilms.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)

				// ignore all metrics except "disk.io" metrics
				metricName := metric.Name()
				if _, ok := metricsToSplit[metricName]; !ok {
					continue
				}

				// split into read and write metrics
				read, write, err := splitReadWriteBytesMetric(metric)
				if err != nil {
					return err
				}

				// append the new metrics to the collection, overwriting the old one
				metric.InitEmpty() // reset original metric to avoid effecting data points that are re-used in new metrics
				read.CopyTo(metric)
				metrics.Append(&write)
			}
		}
	}

	return nil
}

const (
	directionLabel = "direction"
	readDirection  = "read"
	writeDirection = "write"
)

func splitReadWriteBytesMetric(metric pdata.Metric) (read pdata.Metric, write pdata.Metric, err error) {
	// create new read & write metrics with descriptor & name including "read_" & "write_" prefix respectively
	read = newMetricWithName(metric, metricPostfixRegex.ReplaceAllString(metric.Name(), "read_$1"))
	write = newMetricWithName(metric, metricPostfixRegex.ReplaceAllString(metric.Name(), "write_$1"))

	// append data points to the read or write metric as appropriate
	switch t := metric.DataType(); t {
	case pdata.MetricDataTypeIntSum:
		err = appendInt64DataPoints(metric.Name(), metric.IntSum().DataPoints(), read.IntSum().DataPoints(), write.IntSum().DataPoints())
	case pdata.MetricDataTypeDoubleSum:
		err = appendDoubleDataPoints(metric.Name(), metric.DoubleSum().DataPoints(), read.DoubleSum().DataPoints(), write.DoubleSum().DataPoints())
	case pdata.MetricDataTypeIntGauge:
		err = appendInt64DataPoints(metric.Name(), metric.IntGauge().DataPoints(), read.IntGauge().DataPoints(), write.IntGauge().DataPoints())
	case pdata.MetricDataTypeDoubleGauge:
		err = appendDoubleDataPoints(metric.Name(), metric.DoubleGauge().DataPoints(), read.DoubleGauge().DataPoints(), write.DoubleGauge().DataPoints())
	default:
		return read, write, fmt.Errorf("unsupported metric data type: %v", t)
	}

	return read, write, err
}

func appendInt64DataPoints(metricName string, idps, read, write pdata.IntDataPointSlice) error {
	for i := 0; i < idps.Len(); i++ {
		idp := idps.At(i)
		labels := idp.LabelsMap()

		dir, ok := labels.Get(directionLabel)
		if !ok {
			return fmt.Errorf("metric %v did not contain %v label as expected", metricName, directionLabel)
		}
		labels.Delete(directionLabel)

		switch d := dir.Value(); d {
		case readDirection:
			read.Append(&idp)
		case writeDirection:
			write.Append(&idp)
		default:
			return fmt.Errorf("metric %v label %v contained unexpected value %v", metricName, directionLabel, d)
		}
	}

	return nil
}

func appendDoubleDataPoints(metricName string, ddps, read, write pdata.DoubleDataPointSlice) error {
	for i := 0; i < ddps.Len(); i++ {
		ddp := ddps.At(i)
		labels := ddp.LabelsMap()

		dir, ok := labels.Get(directionLabel)
		if !ok {
			return fmt.Errorf("metric %v did not contain %v label as expected", metricName, directionLabel)
		}
		labels.Delete(directionLabel)

		switch d := dir.Value(); d {
		case readDirection:
			read.Append(&ddp)
		case writeDirection:
			write.Append(&ddp)
		default:
			return fmt.Errorf("metric %v label %v contained unexpected value %v", metricName, directionLabel, d)
		}
	}

	return nil
}
