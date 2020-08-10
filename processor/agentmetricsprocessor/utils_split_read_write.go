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
				metricName := metric.MetricDescriptor().Name()
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
	// create new read metric with descriptor & name including "read_" prefix
	read = pdata.NewMetric()
	read.InitEmpty()
	metric.MetricDescriptor().CopyTo(read.MetricDescriptor())
	read.MetricDescriptor().SetName(metricPostfixRegex.ReplaceAllString(metric.MetricDescriptor().Name(), "read_$1"))

	// create new write metric with descriptor & name including "write_" prefix
	write = pdata.NewMetric()
	write.InitEmpty()
	metric.MetricDescriptor().CopyTo(write.MetricDescriptor())
	write.MetricDescriptor().SetName(metricPostfixRegex.ReplaceAllString(metric.MetricDescriptor().Name(), "write_$1"))

	// append int64 data points to the read or write metric as appropriate
	if err = appendInt64DataPoints(metric, read, write); err != nil {
		return read, write, err
	}

	// append double data points to the read or write metric as appropriate
	if err = appendDoubleDataPoints(metric, read, write); err != nil {
		return read, write, err
	}

	return read, write, err
}

func appendInt64DataPoints(metric, read, write pdata.Metric) error {
	idps := metric.Int64DataPoints()
	for i := 0; i < idps.Len(); i++ {
		idp := idps.At(i)
		labels := idp.LabelsMap()

		dir, ok := labels.Get(directionLabel)
		if !ok {
			return fmt.Errorf("metric %v did not contain %v label as expected", metric.MetricDescriptor().Name(), directionLabel)
		}
		labels.Delete(directionLabel)

		switch d := dir.Value(); d {
		case readDirection:
			read.Int64DataPoints().Append(&idp)
		case writeDirection:
			write.Int64DataPoints().Append(&idp)
		default:
			return fmt.Errorf("metric %v label %v contained unexpected value %v", metric.MetricDescriptor().Name(), directionLabel, d)
		}
	}

	return nil
}

func appendDoubleDataPoints(metric, read, write pdata.Metric) error {
	ddps := metric.DoubleDataPoints()
	for i := 0; i < ddps.Len(); i++ {
		ddp := ddps.At(i)
		labels := ddp.LabelsMap()

		dir, ok := labels.Get(directionLabel)
		if !ok {
			return fmt.Errorf("metric %v did not contain %v label as expected", metric.MetricDescriptor().Name(), directionLabel)
		}
		labels.Delete(directionLabel)

		switch d := dir.Value(); d {
		case readDirection:
			read.DoubleDataPoints().Append(&ddp)
		case writeDirection:
			write.DoubleDataPoints().Append(&ddp)
		default:
			return fmt.Errorf("metric %v label %v contained unexpected value %v", metric.MetricDescriptor().Name(), directionLabel, d)
		}
	}

	return nil
}
