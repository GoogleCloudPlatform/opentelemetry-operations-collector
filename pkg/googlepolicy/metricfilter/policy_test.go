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

package metricfilter

import (
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// newTestMetricBundle builds a monotonic delta Sum instrument carrying one
// datapoint, wrapped in a populated scope and resource.
func newTestMetricBundle() (pmetric.Metric, pmetric.ScopeMetrics, pmetric.ResourceMetrics) {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("cloud.zone", "us-central1-a")

	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName("my.library")
	sm.Scope().SetVersion("v1.2.3")
	sm.SetSchemaUrl("https://opentelemetry.io/schemas/1.24.0")
	sm.Scope().Attributes().PutStr("scope.tag", "backend")

	m := sm.Metrics().AppendEmpty()
	m.SetName("http.server.duration")
	m.SetDescription("Duration of inbound HTTP requests")
	m.SetUnit("ms")
	sum := m.SetEmptySum()
	sum.SetIsMonotonic(true)
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)

	dp := sum.DataPoints().AppendEmpty()
	attrs := dp.Attributes()
	attrs.PutStr("http.method", "GET")
	attrs.PutInt("http.status_code", 200)
	attrs.PutDouble("latency_seconds", 1.25)
	attrs.PutBool("feature.enabled", true)
	attrs.PutEmptyBytes("binary.id").FromRaw([]byte{0x01, 0x02, 0x03})

	roles := attrs.PutEmptySlice("user.roles")
	roles.AppendEmpty().SetStr("viewer")
	roles.AppendEmpty().SetStr("admin")

	meta := attrs.PutEmptyMap("metadata")
	meta.PutStr("env", "prod")

	items := attrs.PutEmptySlice("items")
	item0 := items.AppendEmpty().SetEmptyMap()
	item0.PutStr("id", "item-001")

	return m, sm, rm
}

// metricContext assembles the MetricContext the collector processor would build
// for the first datapoint of the given instrument.
func metricContext(m pmetric.Metric, scope pmetric.ScopeMetrics, res pmetric.ResourceMetrics) googlepolicy.MetricContext {
	ctx := googlepolicy.MetricContext{
		Metric:            m,
		Resource:          res.Resource(),
		Scope:             scope.Scope(),
		ResourceSchemaURL: res.SchemaUrl(),
		ScopeSchemaURL:    scope.SchemaUrl(),
	}

	switch m.Type() {
	case pmetric.MetricTypeGauge:
		if dps := m.Gauge().DataPoints(); dps.Len() > 0 {
			ctx.DatapointAttributes = dps.At(0).Attributes()
		}
	case pmetric.MetricTypeSum:
		ctx.AggregationTemporality = m.Sum().AggregationTemporality()
		if dps := m.Sum().DataPoints(); dps.Len() > 0 {
			ctx.DatapointAttributes = dps.At(0).Attributes()
		}
	case pmetric.MetricTypeHistogram:
		ctx.AggregationTemporality = m.Histogram().AggregationTemporality()
		if dps := m.Histogram().DataPoints(); dps.Len() > 0 {
			ctx.DatapointAttributes = dps.At(0).Attributes()
		}
	case pmetric.MetricTypeExponentialHistogram:
		ctx.AggregationTemporality = m.ExponentialHistogram().AggregationTemporality()
		if dps := m.ExponentialHistogram().DataPoints(); dps.Len() > 0 {
			ctx.DatapointAttributes = dps.At(0).Attributes()
		}
	case pmetric.MetricTypeSummary:
		if dps := m.Summary().DataPoints(); dps.Len() > 0 {
			ctx.DatapointAttributes = dps.At(0).Attributes()
		}
	}

	return ctx
}

func evalMetric(pol *Policy, m pmetric.Metric, scope pmetric.ScopeMetrics, res pmetric.ResourceMetrics) googlepolicy.EvalResult {
	return pol.EvaluateMetric(metricContext(m, scope, res))
}

func matchesMetric(pol *Policy, m pmetric.Metric, scope pmetric.ScopeMetrics, res pmetric.ResourceMetrics) bool {
	return pol.matchesContext(metricContext(m, scope, res))
}

// descriptorTarget and friends keep the deeply nested selector literals out of
// the table bodies.
func descriptorTarget(f policyv1alpha1.MetricDescriptorField) *policyv1alpha1.MetricFieldSelector {
	return &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{DescriptorField: f},
	}
}

func scopeFieldTarget(f policyv1alpha1.ScopeField) *policyv1alpha1.MetricFieldSelector {
	return &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ScopeField{ScopeField: f},
	}
}

func datapointTarget(path ...string) *policyv1alpha1.MetricFieldSelector {
	return &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
			DatapointAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func resourceTarget(path ...string) *policyv1alpha1.MetricFieldSelector {
	return &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ResourceAttribute{
			ResourceAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func scopeAttrTarget(path ...string) *policyv1alpha1.MetricFieldSelector {
	return &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ScopeAttribute{
			ScopeAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func stringValue(s string) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: s}}
}

// mustExtract compiles a selector into its target extractor, so tests can probe
// the (value, exists) pair directly instead of inferring it from a predicate.
func mustExtract(t *testing.T, target *policyv1alpha1.MetricFieldSelector) targetExtractor {
	t.Helper()
	extract, err := compileExtractor(target)
	require.NoError(t, err)
	return extract
}

func TestPolicyValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		proto     *policyv1alpha1.MetricFilterPolicy
		errExpect error
	}{
		{
			name:      "nil proto",
			proto:     nil,
			errExpect: ErrNilProto,
		},
		{
			name: "missing id",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target:    descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingID,
		},
		{
			name: "missing action",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p1",
				Action: policyv1alpha1.Action_ACTION_UNSPECIFIED.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target:    descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "invalid action",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p1-invalid-action",
				Action: policyv1alpha1.Action(99).Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target:    descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "missing matchers",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:      "p2",
				Action:  policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{},
			},
			errExpect: ErrMissingMatchers,
		},
		{
			name: "matcher missing target",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p3",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "matcher empty target oneof",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p3-empty-oneof",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target:    &policyv1alpha1.MetricFieldSelector{},
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "matcher unspecified descriptor field",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p3-unspecified-descriptor",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target:    descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNSPECIFIED),
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher unspecified scope field",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p3-unspecified-scope",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target:    scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED),
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher empty attribute path",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p3-empty-path",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target:    datapointTarget(),
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher empty resource attribute path",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p3-empty-resource-path",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target:    resourceTarget(),
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher empty scope attribute path",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p3-empty-scope-path",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target:    scopeAttrTarget(),
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher nil datapoint attribute path",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p3-nil-datapoint-path",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target: &policyv1alpha1.MetricFieldSelector{
							Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{},
						},
						Predicate: &policyv1alpha1.MetricMatcher_Exists{},
					},
				},
			},
			errExpect: assert.AnError,
		},
		{
			name: "matcher missing predicate",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p4",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
					},
				},
			},
			errExpect: ErrMissingPredicate,
		},
		{
			name: "matcher invalid regex",
			proto: &policyv1alpha1.MetricFilterPolicy{
				Id:     "p5",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
				Matches: []*policyv1alpha1.MetricMatcher{
					{
						Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
						Predicate: &policyv1alpha1.MetricMatcher_Regex{
							Regex: "[invalid(regex",
						},
					},
				},
			},
			errExpect: assert.AnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPolicyFromProto(tt.proto)
			require.Error(t, err)
			if tt.errExpect != assert.AnError {
				assert.ErrorIs(t, err, tt.errExpect)
			}
		})
	}
}

func TestPolicyMatchingAndDrop(t *testing.T) {
	metric, scope, res := newTestMetricBundle()

	t.Run("Action DROP matching metric", func(t *testing.T) {
		pb := &policyv1alpha1.MetricFilterPolicy{
			Id:     "drop-duration-histogram",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
					Predicate: &policyv1alpha1.MetricMatcher_Regex{
						Regex: "^http\\.server\\.",
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.Equal(t, "drop-duration-histogram", pol.PolicyName())
		assert.Equal(t, PolicyType, pol.PolicyType())
		assert.Equal(t, googlepolicy.PolicyClassTransformation, pol.PolicyClass())
		assert.Equal(t, []googlepolicy.Signal{googlepolicy.SignalMetrics}, pol.TargetSignals())
		assert.Equal(t, policyv1alpha1.Action_ACTION_DROP, pol.Action())
		assert.Same(t, pb, pol.Proto())
		assert.NoError(t, pol.Validate())

		assert.True(t, matchesMetric(pol, metric, scope, res))
		assert.Equal(t, googlepolicy.EvalDrop, evalMetric(pol, metric, scope, res))
	})

	t.Run("Action DROP non-matching metric", func(t *testing.T) {
		pb := &policyv1alpha1.MetricFilterPolicy{
			Id:     "drop-non-matching",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("system.cpu.load"),
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)

		assert.False(t, matchesMetric(pol, metric, scope, res))
		assert.Equal(t, googlepolicy.EvalNoMatch, evalMetric(pol, metric, scope, res))
	})

	t.Run("Action KEEP matching metric", func(t *testing.T) {
		pb := &policyv1alpha1.MetricFilterPolicy{
			Id:     "keep-sum",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("SUM"),
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)

		assert.True(t, matchesMetric(pol, metric, scope, res))
		// Matching KEEP policy evaluates to EvalKeep (exemption)
		assert.Equal(t, googlepolicy.EvalKeep, evalMetric(pol, metric, scope, res))
	})

	t.Run("Action KEEP non-matching metric", func(t *testing.T) {
		pb := &policyv1alpha1.MetricFilterPolicy{
			Id:     "keep-histogram-only",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("HISTOGRAM"),
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)

		assert.False(t, matchesMetric(pol, metric, scope, res))
		// Non-matching KEEP policy evaluates to EvalNoMatch: KEEP is an
		// exemption and never prunes on its own.
		assert.Equal(t, googlepolicy.EvalNoMatch, evalMetric(pol, metric, scope, res))
	})

	t.Run("Multiple matchers AND semantics", func(t *testing.T) {
		pb := &policyv1alpha1.MetricFilterPolicy{
			Id:     "and-matchers",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("http.server.duration"),
					},
				},
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("SUM"),
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesMetric(pol, metric, scope, res))

		// If one matcher does not match, the entire policy should not match
		pb.Matches[1].Predicate = &policyv1alpha1.MetricMatcher_Equals{
			Equals: stringValue("HISTOGRAM"),
		}
		pol2, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.False(t, matchesMetric(pol2, metric, scope, res))
	})

	t.Run("Negate matcher", func(t *testing.T) {
		pb := &policyv1alpha1.MetricFilterPolicy{
			Id:     "negate-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: datapointTarget("http.method"),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("POST"), // does not match GET
					},
					Negate: true, // negated -> true
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesMetric(pol, metric, scope, res))

		// Without negate the same matcher must not match.
		pb.Matches[0].Negate = false
		pol2, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.False(t, matchesMetric(pol2, metric, scope, res))
	})

	t.Run("Resource, scope attribute, and scope field selectors", func(t *testing.T) {
		pb := &policyv1alpha1.MetricFilterPolicy{
			Id:     "res-scope-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: resourceTarget("cloud.zone"),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("us-central1-a"),
					},
				},
				{
					Target: scopeAttrTarget("scope.tag"),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("backend"),
					},
				},
				{
					Target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("my.library"),
					},
				},
				{
					Target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("v1.2.3"),
					},
				},
				{
					Target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("https://opentelemetry.io/schemas/1.24.0"),
					},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesMetric(pol, metric, scope, res))
	})

	t.Run("Typed datapoint attribute matchers", func(t *testing.T) {
		pb := &policyv1alpha1.MetricFilterPolicy{
			Id:     "typed-test",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: datapointTarget("feature.enabled"),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BoolValue{BoolValue: true}},
					},
				},
				{
					Target: datapointTarget("latency_seconds"),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_DoubleValue{DoubleValue: 1.25}},
					},
				},
				{
					Target: datapointTarget("binary.id"),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: []byte{0x01, 0x02, 0x03}}},
					},
				},
				{
					Target: datapointTarget("http.status_code"),
					Predicate: &policyv1alpha1.MetricMatcher_Gte{
						Gte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 200}},
					},
				},
				{
					Target: datapointTarget("http.status_code"),
					Predicate: &policyv1alpha1.MetricMatcher_Lt{
						Lt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 300}},
					},
				},
				{
					Target: datapointTarget("latency_seconds"),
					Predicate: &policyv1alpha1.MetricMatcher_Gt{
						Gt: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: 1.0}},
					},
				},
				{
					Target: datapointTarget("latency_seconds"),
					Predicate: &policyv1alpha1.MetricMatcher_Lte{
						Lte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_DoubleValue{DoubleValue: 1.25}},
					},
				},
				{
					Target: datapointTarget("user.roles"),
					Predicate: &policyv1alpha1.MetricMatcher_Contains{
						Contains: stringValue("admin"),
					},
				},
				{
					Target:    datapointTarget("metadata", "env"),
					Predicate: &policyv1alpha1.MetricMatcher_Exists{},
				},
			},
		}

		pol, err := NewPolicyFromProto(pb)
		require.NoError(t, err)
		assert.True(t, matchesMetric(pol, metric, scope, res))
	})
}

func TestMetricDescriptorFieldSelectors(t *testing.T) {
	metric, scope, res := newTestMetricBundle()
	ctx := metricContext(metric, scope, res)

	tests := []struct {
		name       string
		field      policyv1alpha1.MetricDescriptorField
		wantValue  any
		wantExists bool
	}{
		{
			name:       "NAME",
			field:      policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
			wantValue:  "http.server.duration",
			wantExists: true,
		},
		{
			name:       "DESCRIPTION",
			field:      policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION,
			wantValue:  "Duration of inbound HTTP requests",
			wantExists: true,
		},
		{
			name:       "UNIT",
			field:      policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT,
			wantValue:  "ms",
			wantExists: true,
		},
		{
			name:       "TYPE",
			field:      policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE,
			wantValue:  "SUM",
			wantExists: true,
		},
		{
			name:       "AGGREGATION_TEMPORALITY",
			field:      policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY,
			wantValue:  "DELTA",
			wantExists: true,
		},
		{
			name:       "IS_MONOTONIC",
			field:      policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC,
			wantValue:  true,
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extract := mustExtract(t, descriptorTarget(tt.field))
			val, exists := extract(ctx)
			assert.Equal(t, tt.wantExists, exists)
			assert.Equal(t, tt.wantValue, val)
		})
	}

	t.Run("empty descriptor strings report absent", func(t *testing.T) {
		bare := pmetric.NewMetric()
		bare.SetEmptyGauge()
		bareCtx := googlepolicy.MetricContext{Metric: bare}

		for _, field := range []policyv1alpha1.MetricDescriptorField{
			policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
			policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION,
			policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT,
		} {
			extract := mustExtract(t, descriptorTarget(field))
			val, exists := extract(bareCtx)
			assert.False(t, exists, "field %v", field)
			assert.Nil(t, val, "field %v", field)
		}
	})
}

func TestMetricAttributeAndScopeSelectors(t *testing.T) {
	metric, scope, res := newTestMetricBundle()
	ctx := metricContext(metric, scope, res)

	tests := []struct {
		name       string
		target     *policyv1alpha1.MetricFieldSelector
		wantValue  any
		wantExists bool
	}{
		{
			name:       "DatapointAttribute",
			target:     datapointTarget("http.method"),
			wantValue:  "GET",
			wantExists: true,
		},
		{
			name:       "DatapointAttribute missing key",
			target:     datapointTarget("no.such.key"),
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "ResourceAttribute",
			target:     resourceTarget("cloud.zone"),
			wantValue:  "us-central1-a",
			wantExists: true,
		},
		{
			name:       "ResourceAttribute missing key",
			target:     resourceTarget("no.such.key"),
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "ScopeAttribute",
			target:     scopeAttrTarget("scope.tag"),
			wantValue:  "backend",
			wantExists: true,
		},
		{
			name:       "ScopeAttribute missing key",
			target:     scopeAttrTarget("no.such.key"),
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "ScopeField NAME",
			target:     scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
			wantValue:  "my.library",
			wantExists: true,
		},
		{
			name:       "ScopeField VERSION",
			target:     scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION),
			wantValue:  "v1.2.3",
			wantExists: true,
		},
		{
			name:       "ScopeField SCHEMA_URL",
			target:     scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL),
			wantValue:  "https://opentelemetry.io/schemas/1.24.0",
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extract := mustExtract(t, tt.target)
			val, exists := extract(ctx)
			assert.Equal(t, tt.wantExists, exists)
			assert.Equal(t, tt.wantValue, val)
		})
	}

	t.Run("zero-valued resource and scope report absent", func(t *testing.T) {
		empty := googlepolicy.MetricContext{Metric: metric}

		for _, target := range []*policyv1alpha1.MetricFieldSelector{
			resourceTarget("cloud.zone"),
			scopeAttrTarget("scope.tag"),
			scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
			scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION),
			scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL),
		} {
			extract := mustExtract(t, target)
			_, exists := extract(empty)
			assert.False(t, exists)
		}
	})
}

func TestIsMonotonicOnlyExistsForSum(t *testing.T) {
	extract := mustExtract(t, descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC))

	tests := []struct {
		name       string
		setup      func(m pmetric.Metric)
		wantValue  any
		wantExists bool
	}{
		{
			name:       "Sum monotonic",
			setup:      func(m pmetric.Metric) { m.SetEmptySum().SetIsMonotonic(true) },
			wantValue:  true,
			wantExists: true,
		},
		{
			name:       "Sum non-monotonic",
			setup:      func(m pmetric.Metric) { m.SetEmptySum().SetIsMonotonic(false) },
			wantValue:  false,
			wantExists: true,
		},
		{
			name:       "Gauge",
			setup:      func(m pmetric.Metric) { m.SetEmptyGauge() },
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "Histogram",
			setup:      func(m pmetric.Metric) { m.SetEmptyHistogram() },
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "ExponentialHistogram",
			setup:      func(m pmetric.Metric) { m.SetEmptyExponentialHistogram() },
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "Summary",
			setup:      func(m pmetric.Metric) { m.SetEmptySummary() },
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "Empty instrument",
			setup:      func(_ pmetric.Metric) {},
			wantValue:  nil,
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := pmetric.NewMetric()
			m.SetName("some.metric")
			tt.setup(m)

			val, exists := extract(googlepolicy.MetricContext{Metric: m})
			assert.Equal(t, tt.wantExists, exists)
			assert.Equal(t, tt.wantValue, val)
		})
	}
}

func TestAggregationTemporalitySelector(t *testing.T) {
	extract := mustExtract(t, descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY))

	tests := []struct {
		name         string
		temporality  pmetric.AggregationTemporality
		wantValue    any
		wantExists   bool
		wantAbsentDP bool
	}{
		{
			name:        "delta",
			temporality: pmetric.AggregationTemporalityDelta,
			wantValue:   "DELTA",
			wantExists:  true,
		},
		{
			name:        "cumulative",
			temporality: pmetric.AggregationTemporalityCumulative,
			wantValue:   "CUMULATIVE",
			wantExists:  true,
		},
		{
			// Gauges and summaries carry no temporality; the field must read as
			// absent rather than as the "UNSPECIFIED" string.
			name:        "unspecified",
			temporality: pmetric.AggregationTemporalityUnspecified,
			wantValue:   nil,
			wantExists:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := pmetric.NewMetric()
			m.SetName("some.metric")
			m.SetEmptySum()

			val, exists := extract(googlepolicy.MetricContext{
				Metric:                 m,
				AggregationTemporality: tt.temporality,
			})
			assert.Equal(t, tt.wantExists, exists)
			assert.Equal(t, tt.wantValue, val)
		})
	}
}

func TestMetricTypeString(t *testing.T) {
	tests := []struct {
		name  string
		input pmetric.MetricType
		want  string
	}{
		{name: "gauge", input: pmetric.MetricTypeGauge, want: "GAUGE"},
		{name: "sum", input: pmetric.MetricTypeSum, want: "SUM"},
		{name: "histogram", input: pmetric.MetricTypeHistogram, want: "HISTOGRAM"},
		{name: "exponential histogram", input: pmetric.MetricTypeExponentialHistogram, want: "EXPONENTIAL_HISTOGRAM"},
		{name: "summary", input: pmetric.MetricTypeSummary, want: "SUMMARY"},
		{name: "empty", input: pmetric.MetricTypeEmpty, want: "UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MetricTypeString(tt.input))
		})
	}
}

func TestTemporalityString(t *testing.T) {
	tests := []struct {
		name  string
		input pmetric.AggregationTemporality
		want  string
	}{
		{name: "delta", input: pmetric.AggregationTemporalityDelta, want: "DELTA"},
		{name: "cumulative", input: pmetric.AggregationTemporalityCumulative, want: "CUMULATIVE"},
		{name: "unspecified", input: pmetric.AggregationTemporalityUnspecified, want: "UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, TemporalityString(tt.input))
		})
	}
}

func TestIsDatapointLevel(t *testing.T) {
	tests := []struct {
		name    string
		targets []*policyv1alpha1.MetricFieldSelector
		want    bool
	}{
		{
			name:    "descriptor field only",
			targets: []*policyv1alpha1.MetricFieldSelector{descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME)},
			want:    false,
		},
		{
			name: "resource, scope attribute, and scope field only",
			targets: []*policyv1alpha1.MetricFieldSelector{
				resourceTarget("cloud.zone"),
				scopeAttrTarget("scope.tag"),
				scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
			},
			want: false,
		},
		{
			name:    "datapoint attribute only",
			targets: []*policyv1alpha1.MetricFieldSelector{datapointTarget("http.method")},
			want:    true,
		},
		{
			name: "mixed instrument and datapoint level",
			targets: []*policyv1alpha1.MetricFieldSelector{
				descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
				datapointTarget("http.method"),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := &policyv1alpha1.MetricFilterPolicy{
				Id:     "datapoint-level",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			}
			for _, target := range tt.targets {
				pb.Matches = append(pb.Matches, &policyv1alpha1.MetricMatcher{
					Target:    target,
					Predicate: &policyv1alpha1.MetricMatcher_Exists{},
				})
			}

			pol, err := NewPolicyFromProto(pb)
			require.NoError(t, err)
			assert.Equal(t, tt.want, pol.IsDatapointLevel())
		})
	}
}

func TestNestedAttributePathTraversal(t *testing.T) {
	metric, scope, res := newTestMetricBundle()
	ctx := metricContext(metric, scope, res)

	tests := []struct {
		name       string
		path       []string
		wantValue  any
		wantExists bool
	}{
		{
			name:       "map descent",
			path:       []string{"metadata", "env"},
			wantValue:  "prod",
			wantExists: true,
		},
		{
			name:       "map descent missing key",
			path:       []string{"metadata", "missing"},
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "slice index",
			path:       []string{"user.roles", "1"},
			wantValue:  "admin",
			wantExists: true,
		},
		{
			name:       "slice index into nested map",
			path:       []string{"items", "0", "id"},
			wantValue:  "item-001",
			wantExists: true,
		},
		{
			name:       "slice index out of range",
			path:       []string{"user.roles", "99"},
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "non-numeric index into slice",
			path:       []string{"user.roles", "not_an_int"},
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "negative index into slice",
			path:       []string{"user.roles", "-1"},
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "descend into scalar",
			path:       []string{"http.method", "deeper"},
			wantValue:  nil,
			wantExists: false,
		},
		{
			name:       "whole slice value",
			path:       []string{"user.roles"},
			wantValue:  []any{"viewer", "admin"},
			wantExists: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extract := mustExtract(t, datapointTarget(tt.path...))
			val, exists := extract(ctx)
			assert.Equal(t, tt.wantExists, exists)
			assert.Equal(t, tt.wantValue, val)
		})
	}
}

func TestDriverLoadPolicy(t *testing.T) {
	raw := map[string]any{
		"type":   "metric_filter",
		"id":     "loaded-via-driver",
		"action": "ACTION_DROP",
		"matches": []any{
			map[string]any{
				"target": map[string]any{
					"descriptor_field": "METRIC_DESCRIPTOR_FIELD_NAME",
				},
				"equals": map[string]any{
					"string_value": "drop.this.metric",
				},
			},
		},
	}

	p, err := googlepolicy.LoadPolicy(PolicyType, raw)
	require.NoError(t, err)
	assert.Equal(t, "loaded-via-driver", p.PolicyName())
	assert.Equal(t, PolicyType, p.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassTransformation, p.PolicyClass())

	mpe, ok := p.(googlepolicy.MetricPolicyEvaluator)
	require.True(t, ok)
	assert.False(t, mpe.IsDatapointLevel())

	metric, scope, res := newTestMetricBundle()
	metric.SetName("drop.this.metric")
	assert.Equal(t, googlepolicy.EvalDrop, mpe.EvaluateMetric(metricContext(metric, scope, res)))

	metric.SetName("keep.this.metric")
	assert.Equal(t, googlepolicy.EvalNoMatch, mpe.EvaluateMetric(metricContext(metric, scope, res)))
}

// TestDriverLoadPolicyErrors covers the failure paths of Driver.LoadPolicy.
// Loading goes through googlepolicy.LoadPolicy, the way the policy manager does
// it.
func TestDriverLoadPolicyErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]any
	}{
		{
			name: "empty id",
			raw: map[string]any{
				"type":   "metric_filter",
				"id":     "",
				"action": "ACTION_DROP",
				"matches": []any{
					map[string]any{
						"target": map[string]any{
							"descriptor_field": "METRIC_DESCRIPTOR_FIELD_NAME",
						},
						"exists": map[string]any{},
					},
				},
			},
		},
		{
			name: "missing action due to typo'd top-level field",
			raw: map[string]any{
				"type":    "metric_filter",
				"id":      "typo-policy",
				"actions": "ACTION_DROP", // unknown field discarded -> action left unspecified -> validation error
				"matches": []any{
					map[string]any{
						"target": map[string]any{
							"descriptor_field": "METRIC_DESCRIPTOR_FIELD_NAME",
						},
						"exists": map[string]any{},
					},
				},
			},
		},
		{
			name: "missing target due to typo'd nested field",
			raw: map[string]any{
				"type":   "metric_filter",
				"id":     "typo-nested",
				"action": "ACTION_DROP",
				"matches": []any{
					map[string]any{
						"target": map[string]any{
							"descriptor_fields": "METRIC_DESCRIPTOR_FIELD_NAME", // unknown field discarded -> target unset -> validation error
						},
						"exists": map[string]any{},
					},
				},
			},
		},
		{
			name: "invalid enum value",
			raw: map[string]any{
				"type":   "metric_filter",
				"id":     "bad-enum",
				"action": "ACTION_NOPE",
			},
		},
		{
			name: "valid json but invalid policy",
			raw: map[string]any{
				"type":   "metric_filter",
				"id":     "no-matchers",
				"action": "ACTION_DROP",
			},
		},
		{
			// A config value that cannot be JSON-encoded must surface as an
			// error from the marshal step rather than panicking.
			name: "unmarshalable config value",
			raw: map[string]any{
				"type": "metric_filter",
				"id":   make(chan int),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := googlepolicy.LoadPolicy(PolicyType, tt.raw)
			assert.Error(t, err)
		})
	}
}

// --- Helpers for the table-driven tests below, mirroring the conventions used
// by the logfilter and tracefilter test suites. ---

func equalsString(s string) *policyv1alpha1.MetricMatcher_Equals {
	return &policyv1alpha1.MetricMatcher_Equals{Equals: stringValue(s)}
}

func equalsBool(b bool) *policyv1alpha1.MetricMatcher_Equals {
	return &policyv1alpha1.MetricMatcher_Equals{
		Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BoolValue{BoolValue: b}},
	}
}

func containsString(s string) *policyv1alpha1.MetricMatcher_Contains {
	return &policyv1alpha1.MetricMatcher_Contains{Contains: stringValue(s)}
}

// dropPolicy builds a single-matcher ACTION_DROP policy around target/predicate.
func dropPolicy(id string, target *policyv1alpha1.MetricFieldSelector, predicate any, negate bool) *policyv1alpha1.MetricFilterPolicy {
	m := &policyv1alpha1.MetricMatcher{
		Target: target,
		Negate: negate,
	}
	switch p := predicate.(type) {
	case *policyv1alpha1.MetricMatcher_Exists:
		m.Predicate = p
	case *policyv1alpha1.MetricMatcher_Equals:
		m.Predicate = p
	case *policyv1alpha1.MetricMatcher_Regex:
		m.Predicate = p
	case *policyv1alpha1.MetricMatcher_Contains:
		m.Predicate = p
	case *policyv1alpha1.MetricMatcher_Gt:
		m.Predicate = p
	case *policyv1alpha1.MetricMatcher_Gte:
		m.Predicate = p
	case *policyv1alpha1.MetricMatcher_Lt:
		m.Predicate = p
	case *policyv1alpha1.MetricMatcher_Lte:
		m.Predicate = p
	}
	return &policyv1alpha1.MetricFilterPolicy{
		Id:      id,
		Action:  policyv1alpha1.Action_ACTION_DROP.Enum(),
		Matches: []*policyv1alpha1.MetricMatcher{m},
	}
}

// mustDropPolicy compiles a single-matcher ACTION_DROP policy or fails the test.
func mustDropPolicy(t *testing.T, id string, target *policyv1alpha1.MetricFieldSelector, predicate any, negate bool) *Policy {
	t.Helper()
	pol, err := NewPolicyFromProto(dropPolicy(id, target, predicate, negate))
	require.NoError(t, err)
	return pol
}

// instrumentContext wraps a bare instrument (no resource, no scope) in the
// MetricContext the processor would build for it, including the aggregation
// temporality it carries.
func instrumentContext(m pmetric.Metric) googlepolicy.MetricContext {
	ctx := googlepolicy.MetricContext{Metric: m}
	switch m.Type() {
	case pmetric.MetricTypeSum:
		ctx.AggregationTemporality = m.Sum().AggregationTemporality()
	case pmetric.MetricTypeHistogram:
		ctx.AggregationTemporality = m.Histogram().AggregationTemporality()
	case pmetric.MetricTypeExponentialHistogram:
		ctx.AggregationTemporality = m.ExponentialHistogram().AggregationTemporality()
	}
	return ctx
}

func TestPolicyMetadata(t *testing.T) {
	pb := dropPolicy(
		"metadata-policy",
		descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
		&policyv1alpha1.MetricMatcher_Exists{},
		false,
	)
	pol, err := NewPolicyFromProto(pb)
	require.NoError(t, err)

	assert.Equal(t, "metadata-policy", pol.PolicyName())
	assert.Equal(t, PolicyType, pol.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassTransformation, pol.PolicyClass())
	assert.Equal(t, []googlepolicy.Signal{googlepolicy.SignalMetrics}, pol.TargetSignals())
	assert.Equal(t, policyv1alpha1.Action_ACTION_DROP, pol.Action())
	assert.NoError(t, pol.Validate())

	// Proto() hands back the very message the policy was compiled from, which
	// is what the registry relies on to re-serialize an active policy set.
	got, ok := pol.Proto().(*policyv1alpha1.MetricFilterPolicy)
	require.True(t, ok)
	assert.Same(t, pb, got)
}

func TestDriverMetadata(t *testing.T) {
	d := &Driver{}
	assert.Equal(t, PolicyType, d.PolicyName())
}

// TestValidateNilProto covers the defensive nil-proto arm of Validate. A Policy
// can only reach this state by being constructed directly (NewPolicyFromProto
// rejects a nil proto up front), so the guard is exercised in-package.
func TestValidateNilProto(t *testing.T) {
	assert.ErrorIs(t, (&Policy{}).Validate(), ErrNilProto)
}

// TestEvaluateMetricUnknownActionFailsClosed covers the defensive default arm
// of EvaluateMetric. NewPolicyFromProto rejects any action other than
// KEEP/DROP, so the only way in is direct construction; the contract being
// pinned is that an unrecognized action degrades to EvalNoMatch (telemetry
// passes through) instead of silently dropping datapoints.
func TestEvaluateMetricUnknownActionFailsClosed(t *testing.T) {
	pol := &Policy{
		proto: &policyv1alpha1.MetricFilterPolicy{
			Id:     "unknown-action",
			Action: policyv1alpha1.Action(99).Enum(),
		},
	}
	// No matchers => matchesContext is vacuously true, so evaluation reaches
	// the action switch.
	metric, scope, res := newTestMetricBundle()
	require.True(t, matchesMetric(pol, metric, scope, res))
	assert.Equal(t, googlepolicy.EvalNoMatch, evalMetric(pol, metric, scope, res))
}

// TestUnsetDescriptorStringFieldsReportAbsent verifies that unset first-class
// descriptor string fields (Name, Description, Unit) report (nil, false) so they
// do not falsely match value-comparing predicates (like negative regexes or
// `equals: ""`).
func TestUnsetDescriptorStringFieldsReportAbsent(t *testing.T) {
	fields := []policyv1alpha1.MetricDescriptorField{
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION,
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT,
	}

	for _, field := range fields {
		t.Run(field.String(), func(t *testing.T) {
			m := pmetric.NewMetric()
			m.SetEmptyGauge()
			ctx := instrumentContext(m)
			target := descriptorTarget(field)

			val, exists := mustExtract(t, target)(ctx)
			assert.False(t, exists, "unset value must report exists=false")
			assert.Nil(t, val, "unset value must return nil so value predicates do not match")

			// exists is false ...
			existsPol := mustDropPolicy(t, "exists-"+field.String(), target, &policyv1alpha1.MetricMatcher_Exists{}, false)
			assert.Equal(t, googlepolicy.EvalNoMatch, existsPol.EvaluateMetric(ctx))

			// ... which makes `exists` + negate the "field is unset" selector.
			negatedPol := mustDropPolicy(t, "not-exists-"+field.String(), target, &policyv1alpha1.MetricMatcher_Exists{}, true)
			assert.Equal(t, googlepolicy.EvalDrop, negatedPol.EvaluateMetric(ctx))

			// Value predicates must NOT match an unset field.
			equalsPol := mustDropPolicy(t, "equals-empty-"+field.String(), target, equalsString(""), false)
			assert.Equal(t, googlepolicy.EvalNoMatch, equalsPol.EvaluateMetric(ctx))

			regexEmptyPol := mustDropPolicy(t, "regex-empty-"+field.String(), target, &policyv1alpha1.MetricMatcher_Regex{Regex: "^$"}, false)
			assert.Equal(t, googlepolicy.EvalNoMatch, regexEmptyPol.EvaluateMetric(ctx))

			regexNegPol := mustDropPolicy(t, "regex-neg-"+field.String(), target, &policyv1alpha1.MetricMatcher_Regex{Regex: "^[^E]*$"}, false)
			assert.Equal(t, googlepolicy.EvalNoMatch, regexNegPol.EvaluateMetric(ctx))
		})
	}
}

// TestMetricTypeAbsentOnUntypedInstrument pins the three consequences of
// METRIC_DESCRIPTOR_FIELD_TYPE reporting exists=false on a
// pmetric.MetricTypeEmpty instrument while still rendering the string
// "UNSPECIFIED": `exists` is false, `equals: {string_value: "UNSPECIFIED"}`
// still matches, and `exists` + negate is therefore the selector for "untyped
// instrument".
func TestMetricTypeAbsentOnUntypedInstrument(t *testing.T) {
	target := descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE)

	untyped := pmetric.NewMetric()
	untyped.SetName("no.type.set")
	require.Equal(t, pmetric.MetricTypeEmpty, untyped.Type())
	untypedCtx := instrumentContext(untyped)

	typed := pmetric.NewMetric()
	typed.SetName("has.a.type")
	typed.SetEmptyGauge()
	typedCtx := instrumentContext(typed)

	t.Run("value is rendered but reported absent", func(t *testing.T) {
		val, exists := mustExtract(t, target)(untypedCtx)
		assert.False(t, exists)
		assert.Equal(t, "UNSPECIFIED", val)
	})

	t.Run("exists reports false", func(t *testing.T) {
		pol := mustDropPolicy(t, "type-exists", target, &policyv1alpha1.MetricMatcher_Exists{}, false)
		assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateMetric(untypedCtx))
		// A typed instrument does satisfy the same selector.
		assert.Equal(t, googlepolicy.EvalDrop, pol.EvaluateMetric(typedCtx))
	})

	t.Run("equals UNSPECIFIED still matches", func(t *testing.T) {
		pol := mustDropPolicy(t, "type-equals-unspecified", target, equalsString("UNSPECIFIED"), false)
		assert.Equal(t, googlepolicy.EvalDrop, pol.EvaluateMetric(untypedCtx))
		assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateMetric(typedCtx))

		containsPol := mustDropPolicy(t, "type-contains-unspecified", target, containsString("UNSPEC"), false)
		assert.Equal(t, googlepolicy.EvalDrop, containsPol.EvaluateMetric(untypedCtx))
	})

	t.Run("negated exists selects untyped instruments", func(t *testing.T) {
		pol := mustDropPolicy(t, "type-not-exists", target, &policyv1alpha1.MetricMatcher_Exists{}, true)
		assert.Equal(t, googlepolicy.EvalDrop, pol.EvaluateMetric(untypedCtx))
		assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateMetric(typedCtx))
	})
}

// TestAggregationTemporalityAbsentWhenUnspecified is the
// AGGREGATION_TEMPORALITY counterpart of
// TestMetricTypeAbsentOnUntypedInstrument. Unlike TYPE, this extractor returns
// a literal nil value, so nothing matches the "UNSPECIFIED" spelling and
// `exists` + negate is the only way to select it.
func TestAggregationTemporalityAbsentWhenUnspecified(t *testing.T) {
	target := descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY)

	unspecified := pmetric.NewMetric()
	unspecified.SetName("gauge.no.temporality")
	unspecified.SetEmptyGauge()
	unspecifiedCtx := instrumentContext(unspecified)

	delta := pmetric.NewMetric()
	delta.SetName("delta.sum")
	delta.SetEmptySum().SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
	deltaCtx := instrumentContext(delta)

	t.Run("value is nil and reported absent", func(t *testing.T) {
		val, exists := mustExtract(t, target)(unspecifiedCtx)
		assert.False(t, exists)
		assert.Nil(t, val)
	})

	t.Run("exists reports false", func(t *testing.T) {
		pol := mustDropPolicy(t, "temporality-exists", target, &policyv1alpha1.MetricMatcher_Exists{}, false)
		assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateMetric(unspecifiedCtx))
		assert.Equal(t, googlepolicy.EvalDrop, pol.EvaluateMetric(deltaCtx))
	})

	t.Run("no value predicate matches the absent field", func(t *testing.T) {
		predicates := map[string]any{
			"equals UNSPECIFIED": equalsString("UNSPECIFIED"),
			"equals empty":       equalsString(""),
			"regex anything":     &policyv1alpha1.MetricMatcher_Regex{Regex: ".*"},
			"contains empty":     containsString(""),
		}
		for name, predicate := range predicates {
			pol := mustDropPolicy(t, "temporality-"+name, target, predicate, false)
			assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateMetric(unspecifiedCtx), name)
		}
	})

	t.Run("negated exists selects instruments without a temporality", func(t *testing.T) {
		pol := mustDropPolicy(t, "temporality-not-exists", target, &policyv1alpha1.MetricMatcher_Exists{}, true)
		assert.Equal(t, googlepolicy.EvalDrop, pol.EvaluateMetric(unspecifiedCtx))
		assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateMetric(deltaCtx))
	})
}

// TestIsMonotonicAbsentForNonSumInstruments pins the policy-level consequences
// of IS_MONOTONIC being defined only for sums: every other instrument type
// reports the field as genuinely absent (a nil value), so no value predicate
// matches and `exists` + negate is the "not a sum" selector.
func TestIsMonotonicAbsentForNonSumInstruments(t *testing.T) {
	target := descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC)

	nonSums := []struct {
		name  string
		setup func(pmetric.Metric)
	}{
		{name: "Gauge", setup: func(m pmetric.Metric) { m.SetEmptyGauge() }},
		{name: "Histogram", setup: func(m pmetric.Metric) { m.SetEmptyHistogram() }},
		{name: "ExponentialHistogram", setup: func(m pmetric.Metric) { m.SetEmptyExponentialHistogram() }},
		{name: "Summary", setup: func(m pmetric.Metric) { m.SetEmptySummary() }},
		{name: "Empty", setup: func(pmetric.Metric) {}},
	}

	for _, tt := range nonSums {
		t.Run(tt.name, func(t *testing.T) {
			m := pmetric.NewMetric()
			m.SetName("some.metric")
			tt.setup(m)
			ctx := instrumentContext(m)

			val, exists := mustExtract(t, target)(ctx)
			assert.False(t, exists)
			assert.Nil(t, val)

			existsPol := mustDropPolicy(t, "monotonic-exists-"+tt.name, target, &policyv1alpha1.MetricMatcher_Exists{}, false)
			assert.Equal(t, googlepolicy.EvalNoMatch, existsPol.EvaluateMetric(ctx))

			// Absent means absent: neither `false` nor `true` matches.
			for _, want := range []bool{true, false} {
				pol := mustDropPolicy(t, "monotonic-equals-"+tt.name, target, equalsBool(want), false)
				assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateMetric(ctx))
			}

			negatedPol := mustDropPolicy(t, "monotonic-not-exists-"+tt.name, target, &policyv1alpha1.MetricMatcher_Exists{}, true)
			assert.Equal(t, googlepolicy.EvalDrop, negatedPol.EvaluateMetric(ctx))
		})
	}

	t.Run("Sum", func(t *testing.T) {
		for _, monotonic := range []bool{true, false} {
			m := pmetric.NewMetric()
			m.SetName("some.sum")
			m.SetEmptySum().SetIsMonotonic(monotonic)
			ctx := instrumentContext(m)

			val, exists := mustExtract(t, target)(ctx)
			assert.True(t, exists)
			assert.Equal(t, monotonic, val)

			pol := mustDropPolicy(t, "sum-monotonic", target, equalsBool(monotonic), false)
			assert.Equal(t, googlepolicy.EvalDrop, pol.EvaluateMetric(ctx))
		}
	})
}

// selectorCase names one selector of every supported MetricFieldSelector shape,
// so the zero-value tests below can prove that none of them panics.
type selectorCase struct {
	name   string
	target *policyv1alpha1.MetricFieldSelector
	// wantValue is nil for every selector that guards on a zero-valued pdata
	// handle. SCOPE_FIELD_SCHEMA_URL is the one exception: the schema URL is
	// carried as a plain string on MetricContext, so there is no handle to
	// test and it reads as present-but-empty. `exists` is false either way.
	wantValue any
}

func allSelectors() []selectorCase {
	return []selectorCase{
		{name: "descriptor name", target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME)},
		{name: "descriptor description", target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION)},
		{name: "descriptor unit", target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT)},
		{name: "descriptor type", target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE)},
		{name: "descriptor aggregation temporality", target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY)},
		{name: "descriptor is monotonic", target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC)},
		{name: "datapoint attribute", target: datapointTarget("http.method")},
		{name: "resource attribute", target: resourceTarget("cloud.zone")},
		{name: "scope attribute", target: scopeAttrTarget("scope.tag")},
		{name: "scope name", target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME)},
		{name: "scope version", target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION)},
		{name: "scope schema url", target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL)},
	}
}

// TestEmptyContextFields drives every selector against a zero-valued
// MetricContext, where Metric, DatapointAttributes, Resource and Scope are all
// unset pdata handles. Each extractor must report the target as absent rather
// than dereferencing a nil pdata struct and panicking.
func TestEmptyContextFields(t *testing.T) {
	for _, tt := range allSelectors() {
		t.Run(tt.name, func(t *testing.T) {
			val, exists := mustExtract(t, tt.target)(googlepolicy.MetricContext{})
			assert.False(t, exists)
			assert.Equal(t, tt.wantValue, val)

			pol := mustDropPolicy(t, "empty-ctx-"+tt.name, tt.target, &policyv1alpha1.MetricMatcher_Exists{}, false)
			assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateMetric(googlepolicy.MetricContext{}))
		})
	}
}

// TestZeroValueMetricInPopulatedContext covers the descriptor-field guards
// specifically: a MetricContext whose resource and scope are populated but
// whose Metric is the zero pmetric.Metric{} must not panic for any descriptor
// selector, because ctx.Metric.Name() / .Type() would otherwise dereference a
// nil pdata struct.
func TestZeroValueMetricInPopulatedContext(t *testing.T) {
	_, scope, res := newTestMetricBundle()

	descriptorFields := []policyv1alpha1.MetricDescriptorField{
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION,
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT,
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE,
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY,
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC,
	}

	ctx := googlepolicy.MetricContext{
		Metric:            pmetric.Metric{},
		Resource:          res.Resource(),
		Scope:             scope.Scope(),
		ResourceSchemaURL: res.SchemaUrl(),
		ScopeSchemaURL:    scope.SchemaUrl(),
	}

	predicates := []struct {
		name      string
		predicate any
	}{
		{name: "exists", predicate: &policyv1alpha1.MetricMatcher_Exists{}},
		{name: "equals", predicate: equalsString("")},
		{name: "regex", predicate: &policyv1alpha1.MetricMatcher_Regex{Regex: ".*"}},
		{name: "contains", predicate: containsString("")},
		{name: "gte", predicate: &policyv1alpha1.MetricMatcher_Gte{Gte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 0}}}},
	}

	for _, field := range descriptorFields {
		t.Run(field.String(), func(t *testing.T) {
			target := descriptorTarget(field)

			require.NotPanics(t, func() {
				val, exists := mustExtract(t, target)(ctx)
				assert.False(t, exists)
				assert.Nil(t, val)
			})

			// Every predicate family must survive the zero instrument too.
			for _, p := range predicates {
				pol := mustDropPolicy(t, "zero-metric-"+field.String()+"-"+p.name, target, p.predicate, false)
				require.NotPanics(t, func() {
					assert.Equal(t, googlepolicy.EvalNoMatch, pol.EvaluateMetric(ctx), p.name)
				})
			}
		})
	}
}

// TestDriverLoadPolicyIgnoresUnknownFields pins forward-compatibility behavior
// (DiscardUnknown: true): when the server adds new top-level or nested proto
// fields in future versions, older agents ignore the unknown fields and still
// compile and evaluate the policy cleanly.
func TestDriverLoadPolicyIgnoresUnknownFields(t *testing.T) {
	raw := map[string]any{
		"type":             "metric_filter",
		"id":               "future-compatible-metric-policy",
		"action":           "ACTION_DROP",
		"future_top_field": "ignored_server_metadata",
		"future_priority":  42,
		"matches": []any{
			map[string]any{
				"target":              map[string]any{"descriptor_field": "METRIC_DESCRIPTOR_FIELD_NAME"},
				"exists":              map[string]any{},
				"future_matcher_hint": true,
			},
		},
	}

	p, err := (&Driver{}).LoadPolicy(raw)
	require.NoError(t, err)
	assert.Equal(t, "future-compatible-metric-policy", p.PolicyName())

	metricPol, ok := p.(googlepolicy.MetricPolicyEvaluator)
	require.True(t, ok)

	m, sm, rm := newTestMetricBundle()
	assert.Equal(t, googlepolicy.EvalDrop, metricPol.EvaluateMetric(metricContext(m, sm, rm)))
}
