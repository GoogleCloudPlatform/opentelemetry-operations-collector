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

	"go.opentelemetry.io/collector/model/pdata"
	"go.opentelemetry.io/collector/pdata/pmetric"
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
	start, last pdata.NumberDataPoint
}

func newNormalizeSumsProcessor(logger *zap.Logger) *NormalizeSumsProcessor {
	return &NormalizeSumsProcessor{
		logger:  logger,
		history: make(map[string]*startPoint),
	}
}

// ProcessMetrics implements the MProcessor interface.
func (nsp *NormalizeSumsProcessor) ProcessMetrics(ctx context.Context, metrics pdata.Metrics) (pdata.Metrics, error) {
	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		rms := metrics.ResourceMetrics().At(i)
		nsp.transformMetrics(rms)
	}

	return metrics, nil
}

func (nsp *NormalizeSumsProcessor) transformMetrics(rms pdata.ResourceMetrics) {
	ilms := rms.ScopeMetrics()
	for j := 0; j < ilms.Len(); j++ {
		ilm := ilms.At(j).Metrics()
		newSlice := pdata.NewMetricSlice()
		for k := 0; k < ilm.Len(); k++ {
			metric := ilm.At(k)
			if metric.DataType() == pmetric.MetricDataTypeSum && metric.Sum().IsMonotonic() {
				keepMetric := nsp.processMetric(rms.Resource(), metric)
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
}

// processMetric processes a Sum-type metric.
// It returns a boolean that indicates if the metric should be kept.
func (nsp *NormalizeSumsProcessor) processMetric(resource pdata.Resource, metric pdata.Metric) bool {
	dps := metric.Sum().DataPoints()

	// Only transform data when the StartTimestamp was not set
	if dps.Len() == 0 || dps.At(0).StartTimestamp() != 0 {
		return true
	}

	out := pdata.NewNumberDataPointSlice()
	out.EnsureCapacity(dps.Len())

	for i := 0; i < dps.Len(); i++ {
		nsp.processSumDataPoint(dps.At(i), resource, metric, out)
	}

	if out.Len() > 0 {
		out.CopyTo(dps)
		return true
	}
	return false
}

func (nsp *NormalizeSumsProcessor) processSumDataPoint(dp pdata.NumberDataPoint, resource pdata.Resource, metric pdata.Metric, ndps pdata.NumberDataPointSlice) {
	metricIdentifier := dataPointIdentifier(resource, metric, dp.Attributes())

	start := nsp.history[metricIdentifier]
	// If this is the first time we've observed this unique metric,
	// record it as the start point and do not report this data point
	if start == nil {
		newDP := pdata.NewNumberDataPoint()
		dp.CopyTo(newDP)
		newDP2 := pdata.NewNumberDataPoint()
		newDP.CopyTo(newDP2)

		newStart := startPoint{
			start: newDP,
			last:  newDP2,
		}
		nsp.history[metricIdentifier] = &newStart

		return
	}

	// If this data is older than the start point, we can't meaningfully report this point
	// TODO - consider resetting on two subsequent data points older than current start timestamp.
	// This could signify a permanent clock change.
	if dp.Timestamp() <= start.start.Timestamp() {
		nsp.logger.Info(
			"data point being processed older than last recorded reset, will not be emitted",
			zap.String("lastRecordedReset", start.start.Timestamp().String()),
			zap.String("dataPoint", dp.Timestamp().String()),
		)
		return
	}

	// If data has rolled over or the counter has been restarted for
	// any other reason, grab a new start point and do not report this data
	if (dp.ValueType() == pmetric.MetricValueTypeInt && dp.IntVal() < start.last.IntVal()) || (dp.ValueType() == pmetric.MetricValueTypeDouble && dp.DoubleVal() < start.last.DoubleVal()) {
		dp.CopyTo(start.start)
		dp.CopyTo(start.last)

		return
	}

	dp.CopyTo(start.last)

	newDP := ndps.AppendEmpty()
	dp.CopyTo(newDP)
	switch dp.ValueType() {
	case pmetric.MetricValueTypeInt:
		newDP.SetIntVal(dp.IntVal() - start.start.IntVal())
	case pmetric.MetricValueTypeDouble:
		newDP.SetDoubleVal(dp.DoubleVal() - start.start.DoubleVal())
	}
	newDP.SetStartTimestamp(start.start.Timestamp())
}

func dataPointIdentifier(resource pdata.Resource, metric pdata.Metric, labels pdata.Map) string {
	var b strings.Builder

	// Resource identifiers
	resource.Attributes().Sort().Range(func(k string, v pdata.Value) bool {
		fmt.Fprintf(&b, "%s=%s|", k, v.AsString())
		return true
	})

	// Metric identifiers
	fmt.Fprintf(&b, " - %s", metric.Name())
	labels.Sort().Range(func(k string, v pdata.Value) bool {
		fmt.Fprintf(&b, " %s=%s", k, v.AsString())
		return true
	})
	return b.String()
}
