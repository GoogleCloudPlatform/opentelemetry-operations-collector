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

package matcher

import (
	"errors"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// ErrMissingMetricTarget is returned when a metric matcher does not specify a
// target field selector.
var ErrMissingMetricTarget = errors.New("metric matcher must specify a target field selector")

// MetricTargetExtractor pulls the value a metric matcher targets out of a
// MetricContext.
type MetricTargetExtractor func(ctx googlepolicy.MetricContext) (val any, exists bool)

// CompiledMetricMatcher is a validated, pre-compiled MetricMatcher ready for
// hot-loop evaluation across metric policies (e.g., metricfilter and
// metrictransform).
type CompiledMetricMatcher struct {
	extract   MetricTargetExtractor
	predicate Predicate
}

// Eval reports whether the matcher matches the given MetricContext.
func (cm *CompiledMetricMatcher) Eval(ctx googlepolicy.MetricContext) bool {
	return cm.predicate(cm.extract(ctx))
}

// CompileMetricMatcher validates and compiles a MetricMatcher protobuf into a
// CompiledMetricMatcher.
func CompileMetricMatcher(m *policyv1alpha1.MetricMatcher) (CompiledMetricMatcher, error) {
	extract, err := CompileMetricExtractor(m.GetTarget())
	if err != nil {
		return CompiledMetricMatcher{}, err
	}
	predicate, err := CompilePredicate(m.GetPredicate(), m.GetNegate())
	if err != nil {
		return CompiledMetricMatcher{}, err
	}
	return CompiledMetricMatcher{extract: extract, predicate: predicate}, nil
}

// CompileMetricExtractor validates and compiles a MetricFieldSelector into a
// MetricTargetExtractor.
func CompileMetricExtractor(target *policyv1alpha1.MetricFieldSelector) (MetricTargetExtractor, error) {
	if target == nil || target.Target == nil {
		return nil, ErrMissingMetricTarget
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.MetricFieldSelector_DescriptorField:
		return compileMetricDescriptorFieldExtractor(t.DescriptorField)

	case *policyv1alpha1.MetricFieldSelector_DatapointAttribute:
		steps, err := CompilePath(t.DatapointAttribute, "datapoint")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			return EvalPath(ctx.DatapointAttributes, steps)
		}, nil

	case *policyv1alpha1.MetricFieldSelector_ResourceAttribute:
		steps, err := CompilePath(t.ResourceAttribute, "resource")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Resource == (pcommon.Resource{}) {
				return nil, false
			}
			return EvalPath(ctx.Resource.Attributes(), steps)
		}, nil

	case *policyv1alpha1.MetricFieldSelector_ScopeAttribute:
		steps, err := CompilePath(t.ScopeAttribute, "scope")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			return EvalPath(ctx.Scope.Attributes(), steps)
		}, nil

	case *policyv1alpha1.MetricFieldSelector_ScopeField:
		return compileMetricScopeFieldExtractor(t.ScopeField)

	default:
		return nil, ErrMissingMetricTarget
	}
}

// compileMetricDescriptorFieldExtractor builds an extractor for a first-class
// metric descriptor field.
//
// Per metric_filter_policy.proto: "When evaluated with `exists`, first-class
// fields evaluate to true when set to a non-default (non-empty / non-zero)
// value." String fields (NAME, DESCRIPTION, UNIT) return (nil, false) when
// empty; enum fields (TYPE) return their rendered string ("UNSPECIFIED") with
// exists=false so string predicates can still match the default (see matcher.present).
func compileMetricDescriptorFieldExtractor(field policyv1alpha1.MetricDescriptorField) (MetricTargetExtractor, error) {
	switch field {
	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) {
				return nil, false
			}
			s := ctx.Metric.Name()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) {
				return nil, false
			}
			s := ctx.Metric.Description()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) {
				return nil, false
			}
			s := ctx.Metric.Unit()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE:
		// Every instrument has a type, so an untyped one is present-but-default
		// rather than absent: `exists` reports false, but string predicates can
		// still select it by its "UNSPECIFIED" spelling.
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) {
				return nil, false
			}
			t := ctx.Metric.Type()
			return MetricTypeString(t), t != pmetric.MetricTypeEmpty
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY:
		// Unlike TYPE, temporality is only defined for sums and histograms, and
		// the proto enumerates "DELTA" and "CUMULATIVE" as the only matchable
		// spellings. An instrument without a temporality is therefore reported
		// as truly absent, not as "UNSPECIFIED".
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.AggregationTemporality == pmetric.AggregationTemporalityUnspecified {
				return nil, false
			}
			return TemporalityString(ctx.AggregationTemporality), true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC:
		// Monotonicity is only defined for sums; every other instrument type
		// reports the field as absent rather than as false.
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) || ctx.Metric.Type() != pmetric.MetricTypeSum {
				return nil, false
			}
			return ctx.Metric.Sum().IsMonotonic(), true
		}, nil

	default:
		return nil, errors.New("metric descriptor field cannot be unspecified")
	}
}

func compileMetricScopeFieldExtractor(field policyv1alpha1.ScopeField) (MetricTargetExtractor, error) {
	switch field {
	case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			s := ctx.Scope.Name()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			s := ctx.Scope.Version()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.ScopeSchemaURL == "" {
				return nil, false
			}
			return ctx.ScopeSchemaURL, true
		}, nil

	default:
		return nil, errors.New("scope field cannot be unspecified")
	}
}

// MetricTypeString renders an instrument type using the spelling that policies
// match against.
func MetricTypeString(t pmetric.MetricType) string {
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

// TemporalityString renders an aggregation temporality using the spelling that
// policies match against.
func TemporalityString(t pmetric.AggregationTemporality) string {
	switch t {
	case pmetric.AggregationTemporalityDelta:
		return "DELTA"
	case pmetric.AggregationTemporalityCumulative:
		return "CUMULATIVE"
	default:
		return "UNSPECIFIED"
	}
}
