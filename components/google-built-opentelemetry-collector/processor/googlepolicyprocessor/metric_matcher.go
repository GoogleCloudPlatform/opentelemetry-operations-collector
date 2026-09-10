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
	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type compiledMetricPolicy struct {
	id       string
	action   policyv1alpha1.Action
	matchers []*compiledMetricMatcher
}

type compiledMetricMatcher struct {
	target *policyv1alpha1.MetricFieldSelector
	pred   matcherPredicate
}

func compileMetricPolicy(p *policyv1alpha1.MetricFilterPolicy) (*compiledMetricPolicy, error) {
	cp := &compiledMetricPolicy{
		id:     p.GetId(),
		action: p.GetAction(),
	}
	for _, m := range p.GetMatches() {
		pred, err := compilePredicate(m.GetPredicate(), m.GetNegate())
		if err != nil {
			return nil, err
		}
		cp.matchers = append(cp.matchers, &compiledMetricMatcher{
			target: m.GetTarget(),
			pred:   pred,
		})
	}
	return cp, nil
}

// FilterMetrics filters metrics in-place across the Resource -> Scope -> Metric -> Datapoints hierarchy.
// Dropped datapoints are pruned, and empty metrics/scopes/resources are removed.
func (e *Evaluator) FilterMetrics(md pmetric.Metrics) {
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
				return e.filterMetricDataPoints(m, resource, scope, resourceSchemaURL, scopeSchemaURL)
			})

			return sm.Metrics().Len() == 0
		})

		return rm.ScopeMetrics().Len() == 0
	})
}

func (e *Evaluator) filterMetricDataPoints(m pmetric.Metric, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) bool {
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		e.filterNumberDataPoints(m, m.Gauge().DataPoints(), pmetric.AggregationTemporalityUnspecified, resource, scope, resourceSchemaURL, scopeSchemaURL)
		return m.Gauge().DataPoints().Len() == 0
	case pmetric.MetricTypeSum:
		sum := m.Sum()
		e.filterNumberDataPoints(m, sum.DataPoints(), sum.AggregationTemporality(), resource, scope, resourceSchemaURL, scopeSchemaURL)
		return sum.DataPoints().Len() == 0
	case pmetric.MetricTypeHistogram:
		hist := m.Histogram()
		e.filterHistogramDataPoints(m, hist.DataPoints(), hist.AggregationTemporality(), resource, scope, resourceSchemaURL, scopeSchemaURL)
		return hist.DataPoints().Len() == 0
	case pmetric.MetricTypeExponentialHistogram:
		expHist := m.ExponentialHistogram()
		e.filterExponentialHistogramDataPoints(m, expHist.DataPoints(), expHist.AggregationTemporality(), resource, scope, resourceSchemaURL, scopeSchemaURL)
		return expHist.DataPoints().Len() == 0
	case pmetric.MetricTypeSummary:
		e.filterSummaryDataPoints(m, m.Summary().DataPoints(), resource, scope, resourceSchemaURL, scopeSchemaURL)
		return m.Summary().DataPoints().Len() == 0
	default:
		return false
	}
}

func (e *Evaluator) filterNumberDataPoints(m pmetric.Metric, datapoints pmetric.NumberDataPointSlice, temporality pmetric.AggregationTemporality, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) {
	datapoints.RemoveIf(func(dp pmetric.NumberDataPoint) bool {
		ctx := MetricContext{
			Metric:                 m,
			DatapointAttributes:    dp.Attributes(),
			AggregationTemporality: temporality,
			Resource:               resource,
			Scope:                  scope,
			ResourceSchemaURL:      resourceSchemaURL,
			ScopeSchemaURL:         scopeSchemaURL,
		}
		return e.EvalMetric(ctx)
	})
}

func (e *Evaluator) filterHistogramDataPoints(m pmetric.Metric, datapoints pmetric.HistogramDataPointSlice, temporality pmetric.AggregationTemporality, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) {
	datapoints.RemoveIf(func(dp pmetric.HistogramDataPoint) bool {
		ctx := MetricContext{
			Metric:                 m,
			DatapointAttributes:    dp.Attributes(),
			AggregationTemporality: temporality,
			Resource:               resource,
			Scope:                  scope,
			ResourceSchemaURL:      resourceSchemaURL,
			ScopeSchemaURL:         scopeSchemaURL,
		}
		return e.EvalMetric(ctx)
	})
}

func (e *Evaluator) filterExponentialHistogramDataPoints(m pmetric.Metric, datapoints pmetric.ExponentialHistogramDataPointSlice, temporality pmetric.AggregationTemporality, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) {
	datapoints.RemoveIf(func(dp pmetric.ExponentialHistogramDataPoint) bool {
		ctx := MetricContext{
			Metric:                 m,
			DatapointAttributes:    dp.Attributes(),
			AggregationTemporality: temporality,
			Resource:               resource,
			Scope:                  scope,
			ResourceSchemaURL:      resourceSchemaURL,
			ScopeSchemaURL:         scopeSchemaURL,
		}
		return e.EvalMetric(ctx)
	})
}

func (e *Evaluator) filterSummaryDataPoints(m pmetric.Metric, datapoints pmetric.SummaryDataPointSlice, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) {
	datapoints.RemoveIf(func(dp pmetric.SummaryDataPoint) bool {
		ctx := MetricContext{
			Metric:                 m,
			DatapointAttributes:    dp.Attributes(),
			AggregationTemporality: pmetric.AggregationTemporalityUnspecified,
			Resource:               resource,
			Scope:                  scope,
			ResourceSchemaURL:      resourceSchemaURL,
			ScopeSchemaURL:         scopeSchemaURL,
		}
		return e.EvalMetric(ctx)
	})
}

// EvalMetric returns true if the datapoint should be DROPPED, false if KEPT.
func (e *Evaluator) EvalMetric(ctx MetricContext) bool {
	if len(e.metricPolicies) == 0 {
		return false
	}

	var hasKeep, hasDrop bool
	for _, p := range e.metricPolicies {
		if p.matches(ctx) {
			if p.action == policyv1alpha1.Action_ACTION_KEEP {
				hasKeep = true
			} else if p.action == policyv1alpha1.Action_ACTION_DROP {
				hasDrop = true
			}
		}
	}

	if hasKeep {
		return false
	}
	if hasDrop {
		return true
	}
	return false
}

func (p *compiledMetricPolicy) matches(ctx MetricContext) bool {
	for _, cm := range p.matchers {
		val, exists := extractMetricTarget(ctx, cm.target)
		if !cm.pred.evaluate(val, exists) {
			return false
		}
	}
	return true
}

func extractMetricTarget(ctx MetricContext, target *policyv1alpha1.MetricFieldSelector) (any, bool) {
	if target == nil {
		return nil, false
	}
	switch t := target.Target.(type) {
	case *policyv1alpha1.MetricFieldSelector_DescriptorField:
		switch t.DescriptorField {
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME:
			s := ctx.Metric.Name()
			return s, s != ""
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION:
			s := ctx.Metric.Description()
			return s, s != ""
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT:
			s := ctx.Metric.Unit()
			return s, s != ""
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE:
			return metricTypeToString(ctx.Metric.Type()), true
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY:
			return temporalityToString(ctx.AggregationTemporality), true
		default:
			return nil, false
		}
	case *policyv1alpha1.MetricFieldSelector_DatapointAttribute:
		return lookupPath(ctx.DatapointAttributes, t.DatapointAttribute.GetPath())
	case *policyv1alpha1.MetricFieldSelector_ResourceAttribute:
		return lookupPath(ctx.Resource.Attributes(), t.ResourceAttribute.GetPath())
	case *policyv1alpha1.MetricFieldSelector_ScopeAttribute:
		return lookupPath(ctx.Scope.Attributes(), t.ScopeAttribute.GetPath())
	case *policyv1alpha1.MetricFieldSelector_ScopeField:
		switch t.ScopeField {
		case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
			s := ctx.Scope.Name()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
			s := ctx.Scope.Version()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
			return ctx.ScopeSchemaURL, ctx.ScopeSchemaURL != ""
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

func metricTypeToString(t pmetric.MetricType) string {
	switch t {
	case pmetric.MetricTypeGauge:
		return "gauge"
	case pmetric.MetricTypeSum:
		return "sum"
	case pmetric.MetricTypeHistogram:
		return "histogram"
	case pmetric.MetricTypeExponentialHistogram:
		return "exponential_histogram"
	case pmetric.MetricTypeSummary:
		return "summary"
	default:
		return "unspecified"
	}
}

func temporalityToString(t pmetric.AggregationTemporality) string {
	switch t {
	case pmetric.AggregationTemporalityDelta:
		return "delta"
	case pmetric.AggregationTemporalityCumulative:
		return "cumulative"
	default:
		return "unspecified"
	}
}
