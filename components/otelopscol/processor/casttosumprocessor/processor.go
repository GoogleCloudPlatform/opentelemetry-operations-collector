// Copyright 2022 Google LLC
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

package casttosumprocessor

import (
	"context"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// TODO - This processor shares a lot of similar intent with the MetricsAdjuster present in the
// prometheus receiver. The relevant code should be merged together and made available in a way
// where it is available to all receivers.
// see: https://github.com/open-telemetry/opentelemetry-collector/blob/6e5beaf43b325e63ec6f1e864d9746a0d051cc35/receiver/prometheusreceiver/internal/metrics_adjuster.go#L187
type CastToSumProcessor struct {
	Metrics []string
	logger  *zap.Logger
}

func newCastToSumProcessor(config *Config, logger *zap.Logger) *CastToSumProcessor {
	return &CastToSumProcessor{
		Metrics: config.Metrics,
		logger:  logger,
	}
}

// ProcessMetrics implements the MProcessor interface.
func (ctsp *CastToSumProcessor) ProcessMetrics(_ context.Context, metrics pmetric.Metrics) (pmetric.Metrics, error) {
	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		rms := metrics.ResourceMetrics().At(i)
		ctsp.transformMetrics(rms)
	}

	return metrics, nil
}

func (ctsp *CastToSumProcessor) transformMetrics(rms pmetric.ResourceMetrics) {
	ilms := rms.ScopeMetrics()
	for j := 0; j < ilms.Len(); j++ {
		ilm := ilms.At(j).Metrics()
		for k := 0; k < ilm.Len(); k++ {
			metric := ilm.At(k)
			ctsp.processMetric(rms.Resource(), metric)
		}
	}
}

func sliceContains(names []string, name string) bool {
	for _, n := range names {
		if name == n {
			return true
		}
	}
	return false
}

// processMetric processes a supported metric.
func (ctsp *CastToSumProcessor) processMetric(_ pcommon.Resource, metric pmetric.Metric) {
	if !sliceContains(ctsp.Metrics, metric.Name()) {
		return
	}
	if metric.Type() == pmetric.MetricTypeGauge {
		newMetric := pmetric.NewMetric()
		metric.CopyTo(newMetric)
		newMetric.SetEmptySum()
		metric.Gauge().DataPoints().CopyTo(newMetric.Sum().DataPoints())
		newMetric.CopyTo(metric)
	} else if metric.Type() != pmetric.MetricTypeSum {
		ctsp.logger.Info("Configured metric %s is neither gauge nor sum", zap.String("metric", metric.Name()))
	}
	metric.Sum().SetIsMonotonic(true)
	metric.Sum().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
}
