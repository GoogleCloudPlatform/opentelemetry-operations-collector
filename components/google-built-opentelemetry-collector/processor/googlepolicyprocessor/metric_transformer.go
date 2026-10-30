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
// Filter policies are evaluated before transforms (Pass 1); surviving metrics and datapoints are evaluated against
// transform policies in deterministic stage order (rename -> add). If any transform modifies a metric or datapoint,
// filter policies are re-evaluated (Pass 2). Dropped datapoints are pruned, and empty metrics/scopes/resources are removed.
func (e *Evaluator) TransformMetrics(md pmetric.Metrics) {
	if len(e.metricFilterPolicies) == 0 && len(e.metricTransformPolicies) == 0 {
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

	// Filter-only path (no transform policies active):
	if len(e.metricTransformPolicies) == 0 {
		return e.filterMetricDataPointsOnly(m, ctx, datapointCount)
	}

	// Combined Filter + Transform path:
	// Evaluate instrument-level policies once per metric.
	var instrumentDrop bool
	var instrumentKeep bool
	for _, p := range e.instrumentMetricFilterPolicies {
		switch p.FilterMetric(ctx) {
		case googlepolicy.FilterKeep:
			instrumentKeep = true
		case googlepolicy.FilterDrop:
			instrumentDrop = true
		}
	}

	// If dropped at instrument level and not exempted by KEEP:
	if instrumentDrop && !instrumentKeep && (datapointCount == 0 || !e.hasDatapointMetricFilterPolicies) {
		return true
	}

	// Apply metric-level (non-datapoint) transforms if not dropped:
	var metricLevelModified bool
	for _, tp := range e.metricTransformPolicies {
		if !tp.IsDatapointLevel() {
			if tp.TransformMetric(ctx) == googlepolicy.TransformModified {
				metricLevelModified = true
			}
		}
	}

	// Re-evaluate instrument-level filter policies in Pass 2 if metric-level transforms modified the metric:
	if metricLevelModified && len(e.instrumentMetricFilterPolicies) > 0 {
		instrumentDrop = false
		instrumentKeep = false
		for _, p := range e.instrumentMetricFilterPolicies {
			switch p.FilterMetric(ctx) {
			case googlepolicy.FilterKeep:
				instrumentKeep = true
			case googlepolicy.FilterDrop:
				instrumentDrop = true
			}
		}
	}

	if instrumentDrop && !instrumentKeep && (datapointCount == 0 || !e.hasDatapointMetricFilterPolicies) {
		return true
	}

	if datapointCount == 0 {
		return instrumentDrop && !instrumentKeep
	}

	switch m.Type() {
	case pmetric.MetricTypeGauge:
		transformDatapointsSlice[pmetric.NumberDataPoint](e, m.Gauge().DataPoints(), ctx, instrumentKeep, instrumentDrop)
		return m.Gauge().DataPoints().Len() == 0
	case pmetric.MetricTypeSum:
		transformDatapointsSlice[pmetric.NumberDataPoint](e, m.Sum().DataPoints(), ctx, instrumentKeep, instrumentDrop)
		return m.Sum().DataPoints().Len() == 0
	case pmetric.MetricTypeHistogram:
		transformDatapointsSlice[pmetric.HistogramDataPoint](e, m.Histogram().DataPoints(), ctx, instrumentKeep, instrumentDrop)
		return m.Histogram().DataPoints().Len() == 0
	case pmetric.MetricTypeExponentialHistogram:
		transformDatapointsSlice[pmetric.ExponentialHistogramDataPoint](e, m.ExponentialHistogram().DataPoints(), ctx, instrumentKeep, instrumentDrop)
		return m.ExponentialHistogram().DataPoints().Len() == 0
	case pmetric.MetricTypeSummary:
		transformDatapointsSlice[pmetric.SummaryDataPoint](e, m.Summary().DataPoints(), ctx, instrumentKeep, instrumentDrop)
		return m.Summary().DataPoints().Len() == 0
	default:
		return false
	}
}

func (e *Evaluator) filterMetricDataPointsOnly(m pmetric.Metric, ctx MetricContext, datapointCount int) bool {
	var instrumentDrop bool
	for _, p := range e.instrumentMetricFilterPolicies {
		switch p.FilterMetric(ctx) {
		case googlepolicy.FilterKeep:
			return false
		case googlepolicy.FilterDrop:
			instrumentDrop = true
		}
	}

	if datapointCount == 0 || !e.hasDatapointMetricFilterPolicies {
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
	Len() int
	At(i int) DP
	RemoveIf(f func(DP) bool)
}

func transformDatapointsSlice[DP datapoint, S datapointSlice[DP]](e *Evaluator, datapoints S, base MetricContext, instrumentKeep, instrumentDrop bool) {
	datapoints.RemoveIf(func(dp DP) bool {
		ctx := base
		ctx.DatapointAttributes = dp.Attributes()

		// Pass 1: Filter policies.
		if !instrumentKeep && e.evalDatapointMetricFilters(ctx, instrumentDrop) {
			return true
		}

		// Stage 2: Transform policies.
		var modified bool
		for _, tp := range e.metricTransformPolicies {
			if tp.IsDatapointLevel() {
				if tp.TransformMetric(ctx) == googlepolicy.TransformModified {
					modified = true
				}
			}
		}

		// Pass 2: Post-transform filters if modified.
		if modified && !instrumentKeep && (instrumentDrop || e.hasDatapointMetricFilterPolicies) {
			if e.evalDatapointMetricFilters(ctx, instrumentDrop) {
				return true
			}
		}

		return false
	})
}

// pruneDatapoints removes the datapoints that the active policies drop. The
// base context is copied per datapoint so that only DatapointAttributes varies,
// and only datapoint-level policies are evaluated inside the loop.
func pruneDatapoints[DP datapoint, S datapointSlice[DP]](e *Evaluator, datapoints S, base MetricContext, instrumentDrop bool) {
	datapoints.RemoveIf(func(dp DP) bool {
		ctx := base
		ctx.DatapointAttributes = dp.Attributes()
		return e.evalDatapointMetricFilters(ctx, instrumentDrop)
	})
}

func (e *Evaluator) evalDatapointMetricFilters(ctx MetricContext, instrumentDrop bool) bool {
	var hasDrop bool
	if instrumentDrop {
		hasDrop = true
	}
	for _, p := range e.datapointMetricFilterPolicies {
		switch p.FilterMetric(ctx) {
		case googlepolicy.FilterKeep:
			return false
		case googlepolicy.FilterDrop:
			hasDrop = true
		}
	}
	return hasDrop
}

// EvalMetric returns true if the datapoint should be DROPPED, false if KEPT.
// Surviving datapoints also have any matching MetricTransformPolicy rules applied in-place,
// followed by a post-transform filter check (Pass 2) if any transform modified the metric or datapoint.
func (e *Evaluator) EvalMetric(ctx MetricContext) bool {
	var kept bool
	var hasDrop bool
	for _, p := range e.metricFilterPolicies {
		switch p.FilterMetric(ctx) {
		case googlepolicy.FilterKeep:
			kept = true
		case googlepolicy.FilterDrop:
			hasDrop = true
		}
	}
	if hasDrop && !kept {
		return true
	}
	var modified bool
	for _, tp := range e.metricTransformPolicies {
		if tp.TransformMetric(ctx) == googlepolicy.TransformModified {
			modified = true
		}
	}
	if modified && len(e.metricFilterPolicies) > 0 {
		kept = false
		hasDrop = false
		for _, p := range e.metricFilterPolicies {
			switch p.FilterMetric(ctx) {
			case googlepolicy.FilterKeep:
				kept = true
			case googlepolicy.FilterDrop:
				hasDrop = true
			}
		}
		if hasDrop && !kept {
			return true
		}
	}
	return false
}
