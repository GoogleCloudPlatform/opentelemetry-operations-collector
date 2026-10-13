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
// Dropped datapoints are pruned, empty metrics/scopes/resources are removed, and batch transformation stats are returned.
func (e *Evaluator) TransformMetrics(md pmetric.Metrics) TransformStats {
	stats := newTransformStats()
	if len(e.metricPolicies) == 0 {
		return stats
	}

	md.ResourceMetrics().RemoveIf(func(rm pmetric.ResourceMetrics) bool {
		resource := rm.Resource()
		resourceSchemaURL := rm.SchemaUrl()

		rm.ScopeMetrics().RemoveIf(func(sm pmetric.ScopeMetrics) bool {
			scope := sm.Scope()
			scopeSchemaURL := sm.SchemaUrl()

			sm.Metrics().RemoveIf(func(m pmetric.Metric) bool {
				return e.transformMetricDataPoints(&stats, m, MetricContext{
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

	return stats
}

// transformMetricDataPoints evaluates one instrument and returns true if the
// whole metric should be dropped. The passed context carries everything except
// the instrument-specific temporality, which is filled in here.
func (e *Evaluator) transformMetricDataPoints(stats *TransformStats, m pmetric.Metric, ctx MetricContext) bool {
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
	instMatches, instKeep, instDrop := evaluateMetricSlice(e.instrumentMetricPolicies, ctx)
	if instKeep {
		res := buildMetricEvalResult(instMatches, nil, true, instDrop)
		if datapointCount > 0 {
			recordMetricEvalStats(stats, res, int64(datapointCount))
		}
		return false
	}

	// With no datapoint-level policies (or no datapoints to inspect, such as
	// untyped MetricTypeEmpty or empty series), the instrument verdict is final.
	if datapointCount == 0 || !e.hasDatapointMetricPolicies {
		res := buildMetricEvalResult(instMatches, nil, false, instDrop)
		if datapointCount > 0 {
			recordMetricEvalStats(stats, res, int64(datapointCount))
		}
		return res.Drop
	}

	switch m.Type() {
	case pmetric.MetricTypeGauge:
		pruneDatapoints[pmetric.NumberDataPoint](e, stats, m.Gauge().DataPoints(), ctx, instMatches, instDrop)
		return m.Gauge().DataPoints().Len() == 0
	case pmetric.MetricTypeSum:
		pruneDatapoints[pmetric.NumberDataPoint](e, stats, m.Sum().DataPoints(), ctx, instMatches, instDrop)
		return m.Sum().DataPoints().Len() == 0
	case pmetric.MetricTypeHistogram:
		pruneDatapoints[pmetric.HistogramDataPoint](e, stats, m.Histogram().DataPoints(), ctx, instMatches, instDrop)
		return m.Histogram().DataPoints().Len() == 0
	case pmetric.MetricTypeExponentialHistogram:
		pruneDatapoints[pmetric.ExponentialHistogramDataPoint](e, stats, m.ExponentialHistogram().DataPoints(), ctx, instMatches, instDrop)
		return m.ExponentialHistogram().DataPoints().Len() == 0
	case pmetric.MetricTypeSummary:
		pruneDatapoints[pmetric.SummaryDataPoint](e, stats, m.Summary().DataPoints(), ctx, instMatches, instDrop)
		return m.Summary().DataPoints().Len() == 0
	default:
		return false
	}
}

func recordMetricEvalStats(stats *TransformStats, res metricEvalResult, count int64) {
	if res.Drop {
		stats.Dropped += count
	} else if len(res.Evaluations) > 0 {
		stats.Kept += count
	} else {
		stats.NoMatch += count
	}
	for _, ev := range res.Evaluations {
		if stats.PolicyRecords[ev.PolicyID] == nil {
			stats.PolicyRecords[ev.PolicyID] = make(map[PolicyResult]int64)
		}
		stats.PolicyRecords[ev.PolicyID][ev.Result] += count
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
func pruneDatapoints[DP datapoint, S datapointSlice[DP]](
	e *Evaluator,
	stats *TransformStats,
	datapoints S,
	base MetricContext,
	instMatches []matchedMetricPolicy,
	instDrop bool,
) {
	datapoints.RemoveIf(func(dp DP) bool {
		ctx := base
		ctx.DatapointAttributes = dp.Attributes()
		dpMatches, dpKeep, dpDrop := evaluateMetricSlice(e.datapointMetricPolicies, ctx)
		res := buildMetricEvalResult(instMatches, dpMatches, dpKeep, instDrop || dpDrop)
		recordMetricEvalStats(stats, res, 1)
		return res.Drop
	})
}

type metricEvalResult struct {
	Drop        bool
	Evaluations []PolicyEvaluation
}

type matchedMetricPolicy struct {
	id  string
	res googlepolicy.EvalResult
}

func evaluateMetricSlice(policies []googlepolicy.MetricPolicyEvaluator, ctx MetricContext) (matches []matchedMetricPolicy, hasKeep, hasDrop bool) {
	for _, p := range policies {
		evalRes := p.EvaluateMetric(ctx)
		if evalRes != googlepolicy.EvalNoMatch {
			matches = append(matches, matchedMetricPolicy{
				id:  p.PolicyName(),
				res: evalRes,
			})
			if evalRes == googlepolicy.EvalKeep {
				hasKeep = true
			} else if evalRes == googlepolicy.EvalDrop {
				hasDrop = true
			}
		}
	}
	return matches, hasKeep, hasDrop
}

func buildMetricEvalResult(instMatches, dpMatches []matchedMetricPolicy, hasKeep, hasDrop bool) metricEvalResult {
	var res metricEvalResult
	appendEvals := func(matches []matchedMetricPolicy) {
		for _, p := range matches {
			if hasKeep {
				if p.res == googlepolicy.EvalKeep {
					res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultKept})
				} else {
					res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultNoMatch})
				}
			} else if hasDrop {
				res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultDropped})
			}
		}
	}

	if hasKeep {
		res.Drop = false
		appendEvals(instMatches)
		appendEvals(dpMatches)
	} else if hasDrop {
		res.Drop = true
		appendEvals(instMatches)
		appendEvals(dpMatches)
	} else {
		res.Drop = false
	}

	return res
}

// EvalMetric returns true if the datapoint should be DROPPED, false if KEPT.
func (e *Evaluator) EvalMetric(ctx MetricContext) bool {
	matches, hasKeep, hasDrop := evaluateMetricSlice(e.metricPolicies, ctx)
	return buildMetricEvalResult(matches, nil, hasKeep, hasDrop).Drop
}
