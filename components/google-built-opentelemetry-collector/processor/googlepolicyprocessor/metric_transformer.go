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
	"errors"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type compiledMetricPolicy struct {
	id               string
	action           policyv1alpha1.Action
	isDatapointLevel bool
	matchers         []*compiledMetricMatcher
}

type compiledMetricMatcher struct {
	target *policyv1alpha1.MetricFieldSelector
	pred   matcherPredicate
}

func compileMetricPolicy(p *policyv1alpha1.MetricFilterPolicy) (*compiledMetricPolicy, error) {
	if p.GetAction() == policyv1alpha1.Action_ACTION_UNSPECIFIED {
		return nil, errors.New("policy action must be specified")
	}
	if len(p.GetMatches()) == 0 {
		return nil, errors.New("policy must have at least one matcher")
	}
	cp := &compiledMetricPolicy{
		id:     p.GetId(),
		action: p.GetAction(),
	}
	for _, m := range p.GetMatches() {
		pred, err := compilePredicate(m.GetPredicate(), m.GetNegate())
		if err != nil {
			return nil, err
		}
		if _, ok := m.GetTarget().GetTarget().(*policyv1alpha1.MetricFieldSelector_DatapointAttribute); ok {
			cp.isDatapointLevel = true
		}
		cp.matchers = append(cp.matchers, &compiledMetricMatcher{
			target: m.GetTarget(),
			pred:   pred,
		})
	}
	return cp, nil
}

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
				return e.transformMetricDataPoints(m, resource, scope, resourceSchemaURL, scopeSchemaURL)
			})

			return sm.Metrics().Len() == 0
		})

		return rm.ScopeMetrics().Len() == 0
	})
}

func (e *Evaluator) transformMetricDataPoints(m pmetric.Metric, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) bool {
	var temporality pmetric.AggregationTemporality
	var initialLen int
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		temporality = pmetric.AggregationTemporalityUnspecified
		initialLen = m.Gauge().DataPoints().Len()
	case pmetric.MetricTypeSum:
		temporality = m.Sum().AggregationTemporality()
		initialLen = m.Sum().DataPoints().Len()
	case pmetric.MetricTypeHistogram:
		temporality = m.Histogram().AggregationTemporality()
		initialLen = m.Histogram().DataPoints().Len()
	case pmetric.MetricTypeExponentialHistogram:
		temporality = m.ExponentialHistogram().AggregationTemporality()
		initialLen = m.ExponentialHistogram().DataPoints().Len()
	case pmetric.MetricTypeSummary:
		temporality = pmetric.AggregationTemporalityUnspecified
		initialLen = m.Summary().DataPoints().Len()
	default:
		return false
	}

	instrumentCtx := MetricContext{
		Metric:                 m,
		DatapointAttributes:    pcommon.NewMap(),
		AggregationTemporality: temporality,
		Resource:               resource,
		Scope:                  scope,
		ResourceSchemaURL:      resourceSchemaURL,
		ScopeSchemaURL:         scopeSchemaURL,
	}

	if initialLen == 0 || !e.hasDatapointMetricPolicies {
		return e.evalMetricInstrumentOnly(instrumentCtx)
	}

	for _, p := range e.metricPolicies {
		if !p.isDatapointLevel && p.action == policyv1alpha1.Action_ACTION_KEEP && p.matches(instrumentCtx) {
			return false
		}
	}

	switch m.Type() {
	case pmetric.MetricTypeGauge:
		e.transformNumberDataPoints(m, m.Gauge().DataPoints(), temporality, resource, scope, resourceSchemaURL, scopeSchemaURL)
		return m.Gauge().DataPoints().Len() == 0
	case pmetric.MetricTypeSum:
		e.transformNumberDataPoints(m, m.Sum().DataPoints(), temporality, resource, scope, resourceSchemaURL, scopeSchemaURL)
		return m.Sum().DataPoints().Len() == 0
	case pmetric.MetricTypeHistogram:
		e.transformHistogramDataPoints(m, m.Histogram().DataPoints(), temporality, resource, scope, resourceSchemaURL, scopeSchemaURL)
		return m.Histogram().DataPoints().Len() == 0
	case pmetric.MetricTypeExponentialHistogram:
		e.transformExponentialHistogramDataPoints(m, m.ExponentialHistogram().DataPoints(), temporality, resource, scope, resourceSchemaURL, scopeSchemaURL)
		return m.ExponentialHistogram().DataPoints().Len() == 0
	case pmetric.MetricTypeSummary:
		e.transformSummaryDataPoints(m, m.Summary().DataPoints(), resource, scope, resourceSchemaURL, scopeSchemaURL)
		return m.Summary().DataPoints().Len() == 0
	default:
		return false
	}
}

func (e *Evaluator) evalMetricInstrumentOnly(ctx MetricContext) bool {
	var hasDrop bool
	for _, p := range e.metricPolicies {
		if !p.isDatapointLevel && p.matches(ctx) {
			if p.action == policyv1alpha1.Action_ACTION_KEEP {
				return false
			}
			if p.action == policyv1alpha1.Action_ACTION_DROP {
				hasDrop = true
			}
		}
	}
	return hasDrop
}

func (e *Evaluator) transformNumberDataPoints(m pmetric.Metric, datapoints pmetric.NumberDataPointSlice, temporality pmetric.AggregationTemporality, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) {
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

func (e *Evaluator) transformHistogramDataPoints(m pmetric.Metric, datapoints pmetric.HistogramDataPointSlice, temporality pmetric.AggregationTemporality, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) {
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

func (e *Evaluator) transformExponentialHistogramDataPoints(m pmetric.Metric, datapoints pmetric.ExponentialHistogramDataPointSlice, temporality pmetric.AggregationTemporality, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) {
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

func (e *Evaluator) transformSummaryDataPoints(m pmetric.Metric, datapoints pmetric.SummaryDataPointSlice, resource pcommon.Resource, scope pcommon.InstrumentationScope, resourceSchemaURL, scopeSchemaURL string) {
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

	var hasDrop bool
	for _, p := range e.metricPolicies {
		if p.matches(ctx) {
			if p.action == policyv1alpha1.Action_ACTION_KEEP {
				return false
			}
			if p.action == policyv1alpha1.Action_ACTION_DROP {
				hasDrop = true
			}
		}
	}
	return hasDrop
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
			if ctx.AggregationTemporality == pmetric.AggregationTemporalityUnspecified {
				return nil, false
			}
			return temporalityToString(ctx.AggregationTemporality), true
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC:
			if ctx.Metric.Type() == pmetric.MetricTypeSum {
				return ctx.Metric.Sum().IsMonotonic(), true
			}
			return nil, false
		default:
			return nil, false
		}
	case *policyv1alpha1.MetricFieldSelector_DatapointAttribute:
		return lookupPath(ctx.DatapointAttributes, t.DatapointAttribute.GetPath())
	case *policyv1alpha1.MetricFieldSelector_ResourceAttribute:
		if ctx.Resource == (pcommon.Resource{}) {
			return nil, false
		}
		return lookupPath(ctx.Resource.Attributes(), t.ResourceAttribute.GetPath())
	case *policyv1alpha1.MetricFieldSelector_ScopeAttribute:
		if ctx.Scope == (pcommon.InstrumentationScope{}) {
			return nil, false
		}
		return lookupPath(ctx.Scope.Attributes(), t.ScopeAttribute.GetPath())
	case *policyv1alpha1.MetricFieldSelector_ScopeField:
		if ctx.Scope == (pcommon.InstrumentationScope{}) {
			return nil, false
		}
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
		return "GAUGE"
	case pmetric.MetricTypeSum:
		return "SUM"
	case pmetric.MetricTypeHistogram:
		return "HISTOGRAM"
	case pmetric.MetricTypeExponentialHistogram:
		return "EXPONENTIAL_HISTOGRAM"
	case pmetric.MetricTypeSummary:
		return "SUMMARY"
	default:
		return "UNSPECIFIED"
	}
}

func temporalityToString(t pmetric.AggregationTemporality) string {
	switch t {
	case pmetric.AggregationTemporalityDelta:
		return "DELTA"
	case pmetric.AggregationTemporalityCumulative:
		return "CUMULATIVE"
	default:
		return "UNSPECIFIED"
	}
}
