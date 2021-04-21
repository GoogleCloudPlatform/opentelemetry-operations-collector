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
				metricName := metric.Name()
				if !metricsToComputeUtilizationFor[metricName] {
					continue
				}

				// calculate new utilization metric and append it
				utilizationMetric, err := mtp.calculateUtilizationMetric(metric)
				if err != nil {
					return err
				}

				metrics.Append(utilizationMetric)
			}
		}
	}

	return nil
}

func (mtp *agentMetricsProcessor) calculateUtilizationMetric(usageMetric pdata.Metric) (pdata.Metric, error) {
	utilizationMetric := pdata.NewMetric()
	usageMetric.CopyTo(utilizationMetric)

	utilizationMetric.SetName(metricPostfixRegex.ReplaceAllString(usageMetric.Name(), "utilization"))
	utilizationMetric.SetDataType(pdata.MetricDataTypeDoubleGauge)
	utilizationMetric.DoubleGauge()

	metric := usageMetric

	// for "cpu.time", we need to convert cumulative values to delta values before
	// computing utilization of the deltas
	isCPUTime := usageMetric.Name() == cpuTime
	if isCPUTime {
		delta := pdata.NewMetric()
		usageMetric.CopyTo(delta)
		mtp.convertPrevCPUTimeToDelta(delta)
		metric = delta
	}

	var err error
	switch t := metric.DataType(); t {
	case pdata.MetricDataTypeIntSum, pdata.MetricDataTypeIntGauge:
		err = calculateUtilizationFromIntDataPoints(metric, utilizationMetric)
	case pdata.MetricDataTypeDoubleSum, pdata.MetricDataTypeDoubleGauge:
		err = calculateUtilizationFromDoubleDataPoints(metric, utilizationMetric)
	default:
		return pdata.NewMetric(), fmt.Errorf("unsupported metric data type: %v", t)
	}

	if err != nil {
		return pdata.NewMetric(), err
	}

	// persist the values of "cpu.time" so we can compute deltas on the next cycle
	if isCPUTime {
		mtp.setPrevCPUTimes(usageMetric)
	}

	return utilizationMetric, nil
}

// convertPrevCPUTimeToDelta converts the cpu.time values to delta values using the
// values persisted in the previous snapshot
func (mtp *agentMetricsProcessor) convertPrevCPUTimeToDelta(cpuTimeMetric pdata.Metric) {
	mtp.mutex.Lock()
	defer mtp.mutex.Unlock()

	ddps := cpuTimeMetric.DoubleSum().DataPoints()
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
	pts []pdata.IntDataPoint
	sum float64
}

func calculateUtilizationFromIntDataPoints(metric, utilizationMetric pdata.Metric) error {
	var idps pdata.IntDataPointSlice
	switch t := metric.DataType(); t {
	case pdata.MetricDataTypeIntSum:
		idps = metric.IntSum().DataPoints()
	case pdata.MetricDataTypeIntGauge:
		idps = metric.IntGauge().DataPoints()
	}

	pointCount := idps.Len()
	groupedPoints := make(map[string]*int64Points, pointCount) // overallocate to ensure no resizes are required
	for i := 0; i < pointCount; i++ {
		idp := idps.At(i)

		key, err := otherLabelsAsKey(idp.LabelsMap(), stateLabel)
		if err != nil {
			return fmt.Errorf("metric %v: %w", metric.Name(), err)
		}

		points, ok := groupedPoints[key]
		if !ok {
			points = &int64Points{}
			groupedPoints[key] = points
		}

		points.sum += float64(idp.Value())
		points.pts = append(points.pts, idp)
	}

	ddps := utilizationMetric.DoubleGauge().DataPoints()
	ddps.Resize(pointCount)
	index := 0
	for _, points := range groupedPoints {
		for _, point := range points.pts {
			ddp := ddps.At(index)

			// copy dp, setting the value based on utilization calculation
			point.LabelsMap().CopyTo(ddp.LabelsMap())
			ddp.SetStartTimestamp(point.StartTimestamp())
			ddp.SetTimestamp(point.Timestamp())
			ddp.SetValue(float64(point.Value()) / points.sum * 100)
			index++
		}
	}

	return nil
}

type doublePoints struct {
	pts []pdata.DoubleDataPoint
	sum float64
}

func calculateUtilizationFromDoubleDataPoints(metric, utilizationMetric pdata.Metric) error {
	var ddps pdata.DoubleDataPointSlice
	switch t := metric.DataType(); t {
	case pdata.MetricDataTypeDoubleSum:
		ddps = metric.DoubleSum().DataPoints()
	case pdata.MetricDataTypeDoubleGauge:
		ddps = metric.DoubleGauge().DataPoints()
	}

	pointCount := ddps.Len()
	groupedPoints := make(map[string]*doublePoints, pointCount) // overallocate to ensure no resizes are required
	for i := 0; i < pointCount; i++ {
		ddp := ddps.At(i)

		key, err := otherLabelsAsKey(ddp.LabelsMap(), stateLabel)
		if err != nil {
			return fmt.Errorf("metric %v: %w", metric.Name(), err)
		}

		points, ok := groupedPoints[key]
		if !ok {
			points = &doublePoints{}
			groupedPoints[key] = points
		}

		points.sum += ddp.Value()
		points.pts = append(points.pts, ddp)
	}

	ddps = utilizationMetric.DoubleGauge().DataPoints()
	ddps.Resize(pointCount)
	index := 0
	for _, points := range groupedPoints {
		for _, point := range points.pts {
			ddp := ddps.At(index)

			// copy dp, setting the value based on utilization calculation
			point.LabelsMap().CopyTo(ddp.LabelsMap())
			ddp.SetStartTimestamp(point.StartTimestamp())
			ddp.SetTimestamp(point.Timestamp())
			ddp.SetValue(point.Value() / points.sum * 100)
			index++
		}
	}

	return nil
}

// doubleDataPointsToMap converts the double data points in the provided metric
// to a map of labels to values
func doubleDataPointsToMap(metric pdata.Metric) map[string]float64 {
	ddps := metric.DoubleSum().DataPoints()
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
	labels.Range(func(k string, v string) bool {
		otherLabels[idx] = k + "=" + v
		idx++
		return true
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
	labels.Range(func(k string, v string) bool {
		// ignore any keys specified in excluding
		for _, e := range excluding {
			if k == e {
				return true
			}
		}

		otherLabels = append(otherLabels, fmt.Sprintf("%s=%s", k, v))

		return true
	})

	if len(otherLabels) > otherLabelsLen {
		return "", fmt.Errorf("label set did not include all expected labels: %v", excluding)
	}

	// sort the slice so that we consider labelsets
	// the same regardless of order
	sort.StringSlice(otherLabels).Sort()
	return strings.Join(otherLabels, ";"), nil
}
