// Copyright 2020, OpenTelemetry Authors
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

package normalizesumsprocessor

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/consumer/pdata"
	"go.uber.org/zap"
)

type NormalizeSumsProcessor struct {
	logger     *zap.Logger
	transforms []SumMetrics

	history map[string]*startPoint
}

type startPoint struct {
	dataType pdata.MetricDataType

	intDataPoint    *pdata.IntDataPoint
	doubleDataPoint *pdata.DoubleDataPoint
	lastIntValue    int64
	lastDoubleValue float64
}

func newNormalizeSumsProcessor(logger *zap.Logger, transforms []SumMetrics) *NormalizeSumsProcessor {
	return &NormalizeSumsProcessor{
		logger:     logger,
		transforms: transforms,
		history:    make(map[string]*startPoint),
	}
}

// ProcessMetrics implements the MProcessor interface.
func (nsp *NormalizeSumsProcessor) ProcessMetrics(ctx context.Context, metrics pdata.Metrics) (pdata.Metrics, error) {
	var errors []error

	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		rms := metrics.ResourceMetrics().At(i)
		for _, transform := range nsp.transforms {
			metric, slice := findMetric(transform.MetricName, rms)

			if metric == nil {
				continue
			}

			switch t := metric.DataType(); t {
			case pdata.MetricDataTypeIntSum:
				processingErr := nsp.processIntSumMetric(slice, transform, metric)
				if processingErr != nil {
					errors = append(errors, processingErr)
				}
			case pdata.MetricDataTypeDoubleSum:
				processingErr := nsp.processDoubleSumMetric(slice, transform, metric)
				if processingErr != nil {
					errors = append(errors, processingErr)
				}
			default:
				errors = append(errors, fmt.Errorf("Data Type not supported %s", metric.DataType()))
			}
		}
	}

	if len(errors) > 0 {
		return metrics, consumererror.Combine(errors)
	}

	return metrics, nil
}

func (nsp *NormalizeSumsProcessor) processIntSumMetric(slice *pdata.MetricSlice, transform SumMetrics, metric *pdata.Metric) error {
	// TODO - Generate a unique identifier that combines resource, metrics & labels
	// to appropriately track unique data points in the case that this processor
	// receives batched data or data with multiple resources
	metricIdentifier := transform.MetricName

	if transform.NewName != "" {
		newMetric := pdata.NewMetric()
		metric.CopyTo(newMetric)
		newMetric.SetName(transform.NewName)

		metric = &newMetric
	}

	start := nsp.history[metricIdentifier]
	if start == nil {
		dps := metric.IntSum().DataPoints()
		newDP := pdata.NewIntDataPoint()
		dps.At(0).CopyTo(newDP)

		newStart := startPoint{
			dataType:     pdata.MetricDataTypeIntSum,
			intDataPoint: &newDP,
			lastIntValue: newDP.Value(),
		}
		nsp.history[metricIdentifier] = &newStart
		start = &newStart

		// remove data point from source so we can examine the other points
		intRemoveAt(&dps, 0)
	}

	dps := metric.IntSum().DataPoints()
	i := 0
	for i < dps.Len() {
		dp := dps.At(i)
		// If a data point is older than the stored start point,
		// we cannot use it to calculate meaningful information
		// and it should not be reported
		if dp.Timestamp() <= start.intDataPoint.Timestamp() {
			intRemoveAt(&dps, i)
			continue
		}

		// If data has rolled over or the counter has been restarted
		// for any other reason, grab a new start point and do not report this
		// data
		if dp.Value() < start.lastIntValue {
			dp.CopyTo(*start.intDataPoint)
			start.lastIntValue = dp.Value()

			intRemoveAt(&dps, i)
			continue
		}

		start.lastIntValue = dp.Value()
		dp.SetValue(dp.Value() - start.intDataPoint.Value())
		dp.SetStartTimestamp(start.intDataPoint.Timestamp())

		i++
	}

	// If there is meaningful data to send and we are renaming the metric,
	// add it to the slice
	if dps.Len() > 0 && transform.NewName != "" {
		slice.Append(*metric)
	}

	// If there are no remaining data points after removing restart/start
	// points, or this metric was renamed, remove this metric from the slice
	if dps.Len() == 0 || transform.NewName != "" {
		metricSliceRemoveElement(slice, transform.MetricName)
	}

	return nil
}

func (nsp *NormalizeSumsProcessor) processDoubleSumMetric(slice *pdata.MetricSlice, transform SumMetrics, metric *pdata.Metric) error {
	// TODO - Generate a unique identifier that combines resource, metrics & labels
	// to appropriately track unique data points in the case that this processor
	// receives batched data or data with multiple resources
	metricIdentifier := transform.MetricName

	if transform.NewName != "" {
		newMetric := pdata.NewMetric()
		metric.CopyTo(newMetric)
		newMetric.SetName(transform.NewName)

		metric = &newMetric
	}

	start := nsp.history[metricIdentifier]
	if start == nil {
		dps := metric.DoubleSum().DataPoints()
		newDP := pdata.NewDoubleDataPoint()
		dps.At(0).CopyTo(newDP)

		newStart := startPoint{
			dataType:        pdata.MetricDataTypeDoubleSum,
			doubleDataPoint: &newDP,
			lastDoubleValue: newDP.Value(),
		}
		nsp.history[metricIdentifier] = &newStart
		start = &newStart

		// remove data point from source so we can examine the other points
		doubleRemoveAt(&dps, 0)
	}

	dps := metric.DoubleSum().DataPoints()
	i := 0
	for i < dps.Len() {
		dp := dps.At(i)
		// If a data point is older than the stored start point,
		// we cannot use it to calculate meaningful information
		// and it should not be reported
		if dp.Timestamp() <= start.doubleDataPoint.Timestamp() {
			doubleRemoveAt(&dps, i)
			continue
		}

		// If data has rolled over or the counter has been restarted
		// for any other reason, grab a new start point and do not report this
		// data
		if dp.Value() < start.lastDoubleValue {
			dp.CopyTo(*start.doubleDataPoint)
			start.lastDoubleValue = dp.Value()

			doubleRemoveAt(&dps, i)
			continue
		}

		start.lastDoubleValue = dp.Value()
		dp.SetValue(dp.Value() - start.doubleDataPoint.Value())
		dp.SetStartTimestamp(start.doubleDataPoint.Timestamp())
		i++
	}

	// If there is meaningful data to send and we are renaming the metric,
	// add it to the slice
	if dps.Len() > 0 && transform.NewName != "" {
		slice.Append(*metric)
	}

	// If there are no remaining data points after removing restart/start
	// points, or this metric was renamed, remove this metric from the slice
	if dps.Len() == 0 || transform.NewName != "" {
		metricSliceRemoveElement(slice, transform.MetricName)
	}

	return nil
}

func findMetric(name string, rms pdata.ResourceMetrics) (*pdata.Metric, *pdata.MetricSlice) {
	ilms := rms.InstrumentationLibraryMetrics()
	for j := 0; j < ilms.Len(); j++ {
		ilm := ilms.At(j).Metrics()
		for k := 0; k < ilm.Len(); k++ {
			metric := ilm.At(k)
			if name == metric.Name() {
				return &metric, &ilm
			}
		}
	}

	return nil, nil
}

func intRemoveAt(slice *pdata.IntDataPointSlice, idx int) {
	newSlice := pdata.NewIntDataPointSlice()
	newSlice.Resize(slice.Len() - 1)
	j := 0
	for i := 0; i < slice.Len(); i++ {
		if i != idx {
			slice.At(i).CopyTo(newSlice.At(j))
			j++
		}
	}

	newSlice.CopyTo(*slice)
}

func doubleRemoveAt(slice *pdata.DoubleDataPointSlice, idx int) {
	newSlice := pdata.NewDoubleDataPointSlice()
	newSlice.Resize(slice.Len() - 1)
	j := 0
	for i := 0; i < slice.Len(); i++ {
		if i != idx {
			slice.At(i).CopyTo(newSlice.At(j))
			j++
		}
	}
	newSlice.CopyTo(*slice)
}

func metricSliceRemoveElement(slice *pdata.MetricSlice, name string) {
	newSlice := pdata.NewMetricSlice()
	newSlice.Resize(slice.Len() - 1)
	j := 0
	for i := 0; i < slice.Len(); i++ {
		if slice.At(i).Name() != name {
			slice.At(i).CopyTo(newSlice.At(j))
			j++
		}
	}
	newSlice.CopyTo(*slice)
}
