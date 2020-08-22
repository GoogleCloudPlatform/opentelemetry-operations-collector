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
	"sort"
	"strings"

	"go.opentelemetry.io/collector/consumer/pdata"
)

// The following code calculates a new utilization metric from
// a usage metric across one label (dimension) using the formula:
//
// value{l1=v1,...} = value{l1=v1,...} / sum(value{l1=vx,...}) over x=1..N

const (
	cpuTime         = "system.cpu.time"
	memoryUsage     = "system.memory.usage"
	fileSystemUsage = "system.filesystem.usage"
	swapUsage       = "system.swap.usage"
)

var metricsToComputeUtilizationFor = map[string]bool{
	cpuTime:         true,
	memoryUsage:     true,
	fileSystemUsage: true,
	swapUsage:       true,
}

const stateLabel = "state"

func (mtp *agentMetricsProcessor) appendUtilizationMetrics(rms pdata.ResourceMetricsSlice) error {
	for i := 0; i < rms.Len(); i++ {
		ilms := rms.At(i).InstrumentationLibraryMetrics()
		for j := 0; j < ilms.Len(); j++ {
			metrics := ilms.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)

				// ignore all metrics except the ones we want to compute utilizations for
				metricName := metric.MetricDescriptor().Name()
				if !metricsToComputeUtilizationFor[metricName] {
					continue
				}

				// calculate new utilization metric and append it
				utilizationMetric, err := mtp.calculateUtilizationMetric(metric)
				if err != nil {
					return err
				}

				metrics.Append(&utilizationMetric)
			}
		}
	}

	return nil
}

func (mtp *agentMetricsProcessor) calculateUtilizationMetric(metric pdata.Metric) (pdata.Metric, error) {
	utilizationMetric := pdata.NewMetric()
	metric.CopyTo(utilizationMetric)
	utilizationMetric.MetricDescriptor().SetName(metricPostfixRegex.ReplaceAllString(metric.MetricDescriptor().Name(), "utilization"))
	utilizationMetric.MetricDescriptor().SetType(pdata.MetricTypeDouble)

	// for "cpu.time", we need to convert cumulative values to delta values before
	// computing utilization of the deltas
	isCPUTime := metric.MetricDescriptor().Name() == cpuTime
	if isCPUTime {
		mtp.convertPrevCPUTimeToDelta(utilizationMetric)
	}

	if err := calculateUtilizationFromDoubleDataPoints(utilizationMetric); err != nil {
		return pdata.NewMetric(), err
	}

	if err := calculateUtilizationFromInt64DataPoints(utilizationMetric); err != nil {
		return pdata.NewMetric(), err
	}

	// persist the values of "cpu.time" so we can compute deltas on the next cycle
	if isCPUTime {
		mtp.setPrevCPUTimes(metric)
	}

	return utilizationMetric, nil
}

// convertPrevCPUTimeToDelta converts the cpu.time values to delta values using the
// values persisted in the previous snapshot
func (mtp *agentMetricsProcessor) convertPrevCPUTimeToDelta(cpuTimeMetric pdata.Metric) {
	mtp.mutex.Lock()
	defer mtp.mutex.Unlock()

	ddps := cpuTimeMetric.DoubleDataPoints()
	for i := 0; i < ddps.Len(); {
		ddp := ddps.At(i)

		// if we have no previous value for this cpu/state combination,
		// remove the data point as we cannot calculate a utilization
		prevValue, ok := mtp.prevCPUTimeValues[labelsAsKey(ddp.LabelsMap())]
		if !ok {
			removeElementAt(ddps, i)
			continue
		}

		// delta value = current cumulative value - previous cumulative value
		ddp.SetValue(ddp.Value() - prevValue)
		i++
	}
}

// setPrevCPUTimes persists the cpu.time cumulative values as a map so they can
// be used to calculate deltas in the next snapshot
func (mtp *agentMetricsProcessor) setPrevCPUTimes(cpuTimeMetric pdata.Metric) {
	mtp.mutex.Lock()
	defer mtp.mutex.Unlock()

	mtp.prevCPUTimeValues = doubleDataPointsToMap(cpuTimeMetric)
}

type int64Points struct {
	pts []pdata.Int64DataPoint
	sum float64
}

func calculateUtilizationFromInt64DataPoints(metric pdata.Metric) error {
	idps := metric.Int64DataPoints()
	groupedPoints := make(map[string]*int64Points, idps.Len()) // overallocate to ensure no resizes are required
	for i := 0; i < idps.Len(); i++ {
		idp := idps.At(i)

		key, err := otherLabelsAsKey(idp.LabelsMap(), stateLabel)
		if err != nil {
			return fmt.Errorf("metric %v: %w", metric.MetricDescriptor().Name(), err)
		}

		points, ok := groupedPoints[key]
		if !ok {
			points = &int64Points{}
			groupedPoints[key] = points
		}

		points.sum += float64(idp.Value())
		points.pts = append(points.pts, idp)
	}

	ddps := metric.DoubleDataPoints()
	startIndex, index := ddps.Len(), 0
	ddps.Resize(startIndex + idps.Len())
	for _, points := range groupedPoints {
		for _, point := range points.pts {
			ddp := ddps.At(startIndex + index)

			// copy idp to ddp, setting the value based on utilization calculation
			point.LabelsMap().CopyTo(ddp.LabelsMap())
			ddp.SetStartTime(point.StartTime())
			ddp.SetTimestamp(point.Timestamp())
			ddp.SetValue(float64(point.Value()) / points.sum * 100)
			index++
		}
	}
	idps.Resize(0)

	return nil
}

type doublePoints struct {
	pts []pdata.DoubleDataPoint
	sum float64
}

func calculateUtilizationFromDoubleDataPoints(metric pdata.Metric) error {
	ddps := metric.DoubleDataPoints()
	groupedPoints := make(map[string]*doublePoints, ddps.Len()) // overallocate to ensure no resizes are required
	for i := 0; i < ddps.Len(); i++ {
		ddp := ddps.At(i)

		key, err := otherLabelsAsKey(ddp.LabelsMap(), stateLabel)
		if err != nil {
			return fmt.Errorf("metric %v: %w", metric.MetricDescriptor().Name(), err)
		}

		points, ok := groupedPoints[key]
		if !ok {
			points = &doublePoints{}
			groupedPoints[key] = points
		}

		points.sum += ddp.Value()
		points.pts = append(points.pts, ddp)
	}

	for _, points := range groupedPoints {
		for _, point := range points.pts {
			// update the value based on utilization calculation
			point.SetValue(point.Value() / points.sum * 100)
		}
	}

	return nil
}

// doubleDataPointsToMap converts the double data points in the provided metric
// to a map of labels to values
func doubleDataPointsToMap(metric pdata.Metric) map[string]float64 {
	ddps := metric.DoubleDataPoints()
	labelToValuesMap := make(map[string]float64, ddps.Len())
	for i := 0; i < ddps.Len(); i++ {
		ddp := ddps.At(i)
		key, _ := otherLabelsAsKey(ddp.LabelsMap())
		labelToValuesMap[key] = ddp.Value()
	}
	return labelToValuesMap
}

// removeElementAt removes the element at the specified index. This operation
// does not preserve the order of elements in the data point slice
func removeElementAt(ddps pdata.DoubleDataPointSlice, index int) {
	ddps.At(ddps.Len() - 1).CopyTo(ddps.At(index))
	ddps.Resize(ddps.Len() - 1)
}

// labelsAsKey returns a key representing the labels in the provided labelset.
func labelsAsKey(labels pdata.StringMap) string {
	otherLabelsLen := labels.Len()

	idx, otherLabels := 0, make([]string, otherLabelsLen)
	labels.ForEach(func(k string, v pdata.StringValue) {
		otherLabels[idx] = k + "=" + v.Value()
		idx++
	})

	// sort the slice so that we consider labelsets
	// the same regardless of order
	sort.StringSlice(otherLabels).Sort()
	return strings.Join(otherLabels, ";")
}

// otherLabelsAsKey returns a key representing the other labels in the provided
// labelset excluding the specified label keys. An error is returned if any of the
// specified labels to exclude do not exist in the labelset.
func otherLabelsAsKey(labels pdata.StringMap, excluding ...string) (string, error) {
	otherLabelsLen := labels.Len() - len(excluding)

	otherLabels := make([]string, 0, otherLabelsLen)
	labels.ForEach(func(k string, v pdata.StringValue) {
		// ignore any keys specified in excluding
		for _, e := range excluding {
			if k == e {
				return
			}
		}

		otherLabels = append(otherLabels, fmt.Sprintf("%s=%s", k, v.Value()))
	})

	if len(otherLabels) > otherLabelsLen {
		return "", fmt.Errorf("label set did not include all expected labels: %v", excluding)
	}

	// sort the slice so that we consider labelsets
	// the same regardless of order
	sort.StringSlice(otherLabels).Sort()
	return strings.Join(otherLabels, ";"), nil
}
