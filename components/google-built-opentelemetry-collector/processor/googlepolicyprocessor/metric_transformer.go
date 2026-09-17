// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package googlepolicyprocessor

import (
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// TransformMetrics applies active transformation policies in-place across the Resource -> Scope -> Metric -> Datapoints hierarchy.
// Dropped datapoints are pruned, and empty metrics/scopes/resources are removed.
func (e *Evaluator) TransformMetrics(md pmetric.Metrics) {
	if len(e.metricPolicies) == 0 {
		return
	}

	md.ResourceMetrics().RemoveIf(func(rm pmetric.ResourceMetrics) bool {
		resource := rm.Resource()
		resourceSchemaURL := rm.SchemaUrl()

		rm.ScopeMetrics().RemoveIf(func(sm pmetric.ScopeMetrics) bool {
			scope := sm.Scope()
			scopeSchemaURL := sm.SchemaUrl()

			sm.Metrics().RemoveIf(func(m pmetric.Metric) bool {
				return e.transformMetricDataPoints(m, MetricContext{
					Metric:              m,
					DatapointAttributes: pcommon.NewMap(),
					Resource:            resource,
					Scope:               scope,
					ResourceSchemaURL:   resourceSchemaURL,
					ScopeSchemaURL:      scopeSchemaURL,
				})
			})

			return sm.Metrics().Len() == 0
		})

		return rm.ScopeMetrics().Len() == 0
	})
}

// transformMetricDataPoints evaluates one instrument and returns true if the
// whole metric should be dropped. The passed context carries everything except
// the instrument-specific temporality, which is filled in here.
func (e *Evaluator) transformMetricDataPoints(m pmetric.Metric, ctx MetricContext) bool {
	var datapointCount int
	switch m.Type() {
	case pmetric.MetricTypeEmpty:
		datapointCount = 0
	case pmetric.MetricTypeGauge:
		datapointCount = m.Gauge().DataPoints().Len()
	case pmetric.MetricTypeSum:
		ctx.AggregationTemporality = m.Sum().AggregationTemporality()
		datapointCount = m.Sum().DataPoints().Len()
	case pmetric.MetricTypeHistogram:
		ctx.AggregationTemporality = m.Histogram().AggregationTemporality()
		datapointCount = m.Histogram().DataPoints().Len()
	case pmetric.MetricTypeExponentialHistogram:
		ctx.AggregationTemporality = m.ExponentialHistogram().AggregationTemporality()
		datapointCount = m.ExponentialHistogram().DataPoints().Len()
	case pmetric.MetricTypeSummary:
		datapointCount = m.Summary().DataPoints().Len()
	default:
		return false
	}

	// Evaluate instrument-level policies once per metric. An instrument-level
	// KEEP exempts every datapoint below it, overriding any datapoint DROP.
	var instrumentDrop bool
	for _, p := range e.instrumentMetricPolicies {
		switch p.EvaluateMetric(ctx) {
		case googlepolicy.EvalKeep:
			return false
		case googlepolicy.EvalDrop:
			instrumentDrop = true
		}
	}

	// With no datapoint-level policies (or no datapoints to inspect, such as
	// untyped MetricTypeEmpty or empty series), the instrument verdict is final.
	if datapointCount == 0 || !e.hasDatapointMetricPolicies {
		return instrumentDrop
	}

	switch m.Type() {
	case pmetric.MetricTypeGauge:
		pruneDatapoints[pmetric.NumberDataPoint](e, m.Gauge().DataPoints(), ctx, instrumentDrop)
		return m.Gauge().DataPoints().Len() == 0
	case pmetric.MetricTypeSum:
		pruneDatapoints[pmetric.NumberDataPoint](e, m.Sum().DataPoints(), ctx, instrumentDrop)
		return m.Sum().DataPoints().Len() == 0
	case pmetric.MetricTypeHistogram:
		pruneDatapoints[pmetric.HistogramDataPoint](e, m.Histogram().DataPoints(), ctx, instrumentDrop)
		return m.Histogram().DataPoints().Len() == 0
	case pmetric.MetricTypeExponentialHistogram:
		pruneDatapoints[pmetric.ExponentialHistogramDataPoint](e, m.ExponentialHistogram().DataPoints(), ctx, instrumentDrop)
		return m.ExponentialHistogram().DataPoints().Len() == 0
	case pmetric.MetricTypeSummary:
		pruneDatapoints[pmetric.SummaryDataPoint](e, m.Summary().DataPoints(), ctx, instrumentDrop)
		return m.Summary().DataPoints().Len() == 0
	default:
		return false
	}
}

// datapoint is satisfied by every pmetric datapoint type.
type datapoint interface {
	Attributes() pcommon.Map
}

// datapointSlice is satisfied by every pmetric datapoint slice type.
type datapointSlice[DP datapoint] interface {
	RemoveIf(f func(DP) bool)
}

// pruneDatapoints removes the datapoints that the active policies drop. The
// base context is copied per datapoint so that only DatapointAttributes varies,
// and only datapoint-level policies are evaluated inside the loop.
func pruneDatapoints[DP datapoint, S datapointSlice[DP]](e *Evaluator, datapoints S, base MetricContext, instrumentDrop bool) {
	datapoints.RemoveIf(func(dp DP) bool {
		ctx := base
		ctx.DatapointAttributes = dp.Attributes()
		return e.evalDatapointMetricPolicies(ctx, instrumentDrop)
	})
}

func (e *Evaluator) evalDatapointMetricPolicies(ctx MetricContext, instrumentDrop bool) bool {
	hasDrop := instrumentDrop
	for _, p := range e.datapointMetricPolicies {
		switch p.EvaluateMetric(ctx) {
		case googlepolicy.EvalKeep:
			return false
		case googlepolicy.EvalDrop:
			hasDrop = true
		}
	}
	return hasDrop
}

// EvalMetric returns true if the datapoint should be DROPPED, false if KEPT.
func (e *Evaluator) EvalMetric(ctx MetricContext) bool {
	var hasDrop bool
	for _, p := range e.metricPolicies {
		switch p.EvaluateMetric(ctx) {
		case googlepolicy.EvalKeep:
			return false
		case googlepolicy.EvalDrop:
			hasDrop = true
		}
	}
	return hasDrop
}
