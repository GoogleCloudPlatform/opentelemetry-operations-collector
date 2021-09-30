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

package normalizesumsprocessor

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/model/pdata"
	"go.uber.org/zap"
)

// TODO - This processor shares a lot of similar intent with the MetricsAdjuster present in the
// prometheus receiver. The relevant code should be merged together and made available in a way
// where it is available to all receivers.
// see: https://github.com/open-telemetry/opentelemetry-collector/blob/6e5beaf43b325e63ec6f1e864d9746a0d051cc35/receiver/prometheusreceiver/internal/metrics_adjuster.go#L187
type NormalizeSumsProcessor struct {
	logger *zap.Logger

	history map[string]*startPoint
}

type startPoint struct {
	dataType        pdata.MetricDataType
	intDataPoint    *pdata.IntDataPoint
	doubleDataPoint *pdata.DoubleDataPoint
	lastIntValue    int64
	lastDoubleValue float64
}

func newNormalizeSumsProcessor(logger *zap.Logger) *NormalizeSumsProcessor {
	return &NormalizeSumsProcessor{
		logger:  logger,
		history: make(map[string]*startPoint),
	}
}

// ProcessMetrics implements the MProcessor interface.
func (nsp *NormalizeSumsProcessor) ProcessMetrics(ctx context.Context, metrics pdata.Metrics) (pdata.Metrics, error) {
	var errors []error

	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		rms := metrics.ResourceMetrics().At(i)
		processingErrors := nsp.transformMetrics(rms)
		errors = append(errors, processingErrors...)
	}

	if len(errors) > 0 {
		return metrics, consumererror.Combine(errors)
	}

	return metrics, nil
}

func (nsp *NormalizeSumsProcessor) transformMetrics(rms pdata.ResourceMetrics) []error {
	var errors []error

	ilms := rms.InstrumentationLibraryMetrics()
	for j := 0; j < ilms.Len(); j++ {
		ilm := ilms.At(j).Metrics()
		newSlice := pdata.NewMetricSlice()
		for k := 0; k < ilm.Len(); k++ {
			metric := ilm.At(k)
			if metric.DataType() == pdata.MetricDataTypeIntSum || metric.DataType() == pdata.MetricDataTypeDoubleSum {
				keepMetric, err := nsp.processMetric(rms.Resource(), metric)
				if err != nil {
					errors = append(errors, err)
				}
				if keepMetric {
					newMetric := newSlice.AppendEmpty()
					metric.CopyTo(newMetric)
				}
			} else {
				newMetric := newSlice.AppendEmpty()
				metric.CopyTo(newMetric)
			}
		}

		newSlice.CopyTo(ilm)
	}

	return errors
}

func (nsp *NormalizeSumsProcessor) processMetric(resource pdata.Resource, metric pdata.Metric) (bool, error) {
	switch t := metric.DataType(); t {
	case pdata.MetricDataTypeDoubleSum:
		return nsp.processDoubleSumMetric(resource, metric) > 0, nil
	case pdata.MetricDataTypeIntSum:
		return nsp.processIntSumMetric(resource, metric) > 0, nil
	default:
		return false, fmt.Errorf("data type not supported %s", t)
	}
}

func (nsp *NormalizeSumsProcessor) processDoubleSumMetric(resource pdata.Resource, metric pdata.Metric) int {
	dps := metric.DoubleSum().DataPoints()

	// Only transform data when the StartTimestamp was not set
	if dps.Len() > 0 && dps.At(0).StartTimestamp() == 0 {
		for i := 0; i < dps.Len(); {
			reportData := nsp.processDoubleSumDataPoint(dps.At(i), resource, metric)

			if !reportData {
				removeDoubleDataPointAt(dps, i)
				continue
			}
			i++
		}
	}

	return dps.Len()
}

func (nsp *NormalizeSumsProcessor) processIntSumMetric(resource pdata.Resource, metric pdata.Metric) int {
	dps := metric.IntSum().DataPoints()

	// Only transform data when the StartTimestamp was not set
	if dps.Len() > 0 && dps.At(0).StartTimestamp() == 0 {
		for i := 0; i < dps.Len(); {
			reportData := nsp.processIntSumDataPoint(dps.At(i), resource, metric)

			if !reportData {
				removeIntDataPointAt(dps, i)
				continue
			}
			i++
		}
	}

	return dps.Len()
}

func (nsp *NormalizeSumsProcessor) processDoubleSumDataPoint(dp pdata.DoubleDataPoint, resource pdata.Resource, metric pdata.Metric) bool {
	metricIdentifier := dataPointIdentifier(resource, metric, dp.Attributes())

	start := nsp.history[metricIdentifier]
	// If this is the first time we've observed this unique metric,
	// record it as the start point and do not report this data point
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

		return false
	}

	// If this data is older than the start point, we can't meaningfully report this point
	// TODO - consider resetting on two subsequent data points older than current start timestamp.
	// This could signify a permanent clock change.
	if dp.Timestamp() <= start.doubleDataPoint.Timestamp() {
		nsp.logger.Info(
			"data point being processed older than last recorded reset, will not be emitted",
			zap.String("lastRecordedReset", start.doubleDataPoint.Timestamp().String()),
			zap.String("dataPoint", dp.Timestamp().String()),
		)
		return false
	}

	// If data has rolled over or the counter has been restarted for
	// any other reason, grab a new start point and do not report this data
	if dp.Value() < start.lastDoubleValue {
		dp.CopyTo(*start.doubleDataPoint)
		start.lastDoubleValue = dp.Value()

		return false
	}

	start.lastDoubleValue = dp.Value()
	dp.SetValue(dp.Value() - start.doubleDataPoint.Value())
	dp.SetStartTimestamp(start.doubleDataPoint.Timestamp())

	return true
}

func (nsp *NormalizeSumsProcessor) processIntSumDataPoint(dp pdata.IntDataPoint, resource pdata.Resource, metric pdata.Metric) bool {
	metricIdentifier := dataPointIdentifier(resource, metric, dp.Attributes())

	start := nsp.history[metricIdentifier]
	// If this is the first time we've observed this unique metric,
	// record it as the start point and do not report this data point
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

		return false
	}

	// If this data is older than the start point, we can't meaningfully report this point
	// TODO - consider resetting on two subsequent data points older than current start timestamp.
	// This could signify a permanent clock change.
	if dp.Timestamp() <= start.intDataPoint.Timestamp() {
		nsp.logger.Info(
			"data point being processed older than last recorded reset, will not be emitted",
			zap.String("lastRecordedReset", start.intDataPoint.Timestamp().String()),
			zap.String("dataPoint", dp.Timestamp().String()),
		)
		return false
	}

	// If data has rolled over or the counter has been restarted for
	// any other reason, grab a new start point and do not report this data
	if dp.Value() < start.lastIntValue {
		dp.CopyTo(*start.intDataPoint)
		start.lastIntValue = dp.Value()

		return false
	}

	start.lastIntValue = dp.Value()
	dp.SetValue(dp.Value() - start.intDataPoint.Value())
	dp.SetStartTimestamp(start.intDataPoint.Timestamp())

	return true
}

func dataPointIdentifier(resource pdata.Resource, metric pdata.Metric, labels pdata.StringMap) string {
	var b strings.Builder

	// Resource identifiers
	resource.Attributes().Sort().Range(func(k string, v pdata.AttributeValue) bool {
		fmt.Fprintf(&b, "%s=", k)
		addAttributeToIdentityBuilder(&b, v)
		b.WriteString("|")
		return true
	})

	// Metric identifiers
	fmt.Fprintf(&b, " - %s", metric.Name())
	labels.Sort().Range(func(k, v string) bool {
		fmt.Fprintf(&b, " %s=%s", k, v)
		return true
	})
	return b.String()
}

func addAttributeToIdentityBuilder(b *strings.Builder, v pdata.AttributeValue) {
	switch v.Type() {
	case pdata.AttributeValueTypeArray:
		b.WriteString("[")
		arr := v.ArrayVal()
		for i := 0; i < arr.Len(); i++ {
			addAttributeToIdentityBuilder(b, arr.At(i))
			b.WriteString(",")
		}
		b.WriteString("]")
	case pdata.AttributeValueTypeBool:
		fmt.Fprintf(b, "%t", v.BoolVal())
	case pdata.AttributeValueTypeDouble:
		// TODO - Double attribute values could be problematic for use in
		// forming an identify due to floating point math. Consider how to best
		// handle this case
		fmt.Fprintf(b, "%f", v.DoubleVal())
	case pdata.AttributeValueTypeInt:
		fmt.Fprintf(b, "%d", v.IntVal())
	case pdata.AttributeValueTypeMap:
		b.WriteString("{")
		v.MapVal().Sort().Range(func(k string, mapVal pdata.AttributeValue) bool {
			fmt.Fprintf(b, "%s:", k)
			addAttributeToIdentityBuilder(b, mapVal)
			b.WriteString(",")
			return true
		})
		b.WriteString("}")
	case pdata.AttributeValueTypeNull:
		b.WriteString("NULL")
	case pdata.AttributeValueTypeString:
		fmt.Fprintf(b, "'%s'", v.StringVal())
	}
}

func removeDoubleDataPointAt(slice pdata.DoubleDataPointSlice, idx int) {
	newSlice := pdata.NewDoubleDataPointSlice()
	j := 0
	for i := 0; i < slice.Len(); i++ {
		if i != idx {
			dp := newSlice.AppendEmpty()
			slice.At(i).CopyTo(dp)
			j++
		}
	}
	newSlice.CopyTo(slice)
}

func removeIntDataPointAt(slice pdata.IntDataPointSlice, idx int) {
	newSlice := pdata.NewIntDataPointSlice()
	j := 0
	for i := 0; i < slice.Len(); i++ {
		if i != idx {
			dp := newSlice.AppendEmpty()
			slice.At(i).CopyTo(dp)
			j++
		}
	}
	newSlice.CopyTo(slice)
}
