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

package metrictransform

import (
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"google.golang.org/protobuf/types/known/emptypb"
)

func newTestMetricBundle() (pmetric.Metric, pmetric.NumberDataPoint, pmetric.ScopeMetrics, pmetric.ResourceMetrics) {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.SetSchemaUrl("https://opentelemetry.io/schemas/1.21.0")
	rm.Resource().Attributes().PutStr("service.name", "checkout-service")
	rm.Resource().Attributes().PutStr("cloud.zone", "us-central1-a")

	sm := rm.ScopeMetrics().AppendEmpty()
	sm.SetSchemaUrl("https://opentelemetry.io/schemas/1.21.0/scope")
	sm.Scope().SetName("my.library")
	sm.Scope().SetVersion("1.2.3")
	sm.Scope().Attributes().PutStr("library.language", "go")

	m := sm.Metrics().AppendEmpty()
	m.SetName("http.server.duration")
	m.SetDescription("HTTP server request duration")
	m.SetUnit("ms")
	sum := m.SetEmptySum()
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	sum.SetIsMonotonic(true)
	dp := sum.DataPoints().AppendEmpty()
	dp.Attributes().PutStr("http.method", "GET")
	dp.Attributes().PutInt("http.status_code", 200)

	return m, dp, sm, rm
}

func metricContext(m pmetric.Metric, dp pmetric.NumberDataPoint, sm pmetric.ScopeMetrics, rm pmetric.ResourceMetrics) googlepolicy.MetricContext {
	var temporality pmetric.AggregationTemporality
	if m.Type() == pmetric.MetricTypeSum {
		temporality = m.Sum().AggregationTemporality()
	}
	return googlepolicy.MetricContext{
		Metric:                 m,
		DatapointAttributes:    dp.Attributes(),
		AggregationTemporality: temporality,
		Resource:               rm.Resource(),
		Scope:                  sm.Scope(),
		ResourceSchemaURL:      rm.SchemaUrl(),
		ScopeSchemaURL:         sm.SchemaUrl(),
	}
}

func dpAttrTarget(path ...string) *policyv1alpha1.MetricFieldSelector {
	return &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
			DatapointAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func resourceAttrTarget(path ...string) *policyv1alpha1.MetricFieldSelector {
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

func descriptorFieldTarget(f policyv1alpha1.MetricDescriptorField) *policyv1alpha1.MetricFieldSelector {
	return &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
			DescriptorField: f,
		},
	}
}

func scopeFieldTarget(f policyv1alpha1.ScopeField) *policyv1alpha1.MetricFieldSelector {
	return &policyv1alpha1.MetricFieldSelector{
		Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
			ScopeField: f,
		},
	}
}

func strVal(s string) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: s}}
}

func intVal(n int64) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_IntValue{IntValue: n}}
}

func TestNewPolicyFromProto_Validation(t *testing.T) {
	validMatcher := &policyv1alpha1.MetricMatcher{
		Target:    descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
		Predicate: &policyv1alpha1.MetricMatcher_Exists{Exists: &emptypb.Empty{}},
	}
	validAction := &policyv1alpha1.MetricTransformPolicy_Add{
		Add: &policyv1alpha1.MetricAddAction{
			Target: dpAttrTarget("tier"),
			Value:  strVal("core"),
		},
	}

	tests := []struct {
		name    string
		pb      *policyv1alpha1.MetricTransformPolicy
		wantErr error
	}{
		{
			name:    "nil proto",
			pb:      nil,
			wantErr: ErrNilProto,
		},
		{
			name: "missing id",
			pb: &policyv1alpha1.MetricTransformPolicy{
				Matches: []*policyv1alpha1.MetricMatcher{validMatcher},
				Action:  validAction,
			},
			wantErr: ErrMissingID,
		},
		{
			name: "missing action",
			pb: &policyv1alpha1.MetricTransformPolicy{
				Id:      "no-action",
				Matches: []*policyv1alpha1.MetricMatcher{validMatcher},
			},
			wantErr: ErrMissingAction,
		},
		{
			name: "nil add action struct",
			pb: &policyv1alpha1.MetricTransformPolicy{
				Id:      "nil-add-struct",
				Matches: []*policyv1alpha1.MetricMatcher{validMatcher},
				Action:  &policyv1alpha1.MetricTransformPolicy_Add{},
			},
			wantErr: ErrMissingAction,
		},
		{
			name: "rename action unsupported",
			pb: &policyv1alpha1.MetricTransformPolicy{
				Id:      "unsupported-rename",
				Matches: []*policyv1alpha1.MetricMatcher{validMatcher},
				Action: &policyv1alpha1.MetricTransformPolicy_Rename{
					Rename: &policyv1alpha1.MetricRenameAction{
						From: dpAttrTarget("old"),
						To:   dpAttrTarget("new"),
					},
				},
			},
			wantErr: ErrUnsupportedAction,
		},
		{
			name: "add missing target",
			pb: &policyv1alpha1.MetricTransformPolicy{
				Id:      "missing-target",
				Matches: []*policyv1alpha1.MetricMatcher{validMatcher},
				Action: &policyv1alpha1.MetricTransformPolicy_Add{
					Add: &policyv1alpha1.MetricAddAction{Value: strVal("v")},
				},
			},
			wantErr: ErrMissingTarget,
		},
		{
			name: "add missing value",
			pb: &policyv1alpha1.MetricTransformPolicy{
				Id:      "missing-value",
				Matches: []*policyv1alpha1.MetricMatcher{validMatcher},
				Action: &policyv1alpha1.MetricTransformPolicy_Add{
					Add: &policyv1alpha1.MetricAddAction{Target: dpAttrTarget("k")},
				},
			},
			wantErr: ErrMissingValue,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPolicyFromProto(tc.pb)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestMetricAddAction_AttributesAndCollisionSafety(t *testing.T) {
	m, dp, sm, rm := newTestMetricBundle()
	ctx := metricContext(m, dp, sm, rm)

	// 1. Add new datapoint attribute, nested datapoint attribute, resource attribute, and scope attribute.
	steps := []struct {
		id     string
		target *policyv1alpha1.MetricFieldSelector
		val    *policyv1alpha1.Value
		wantDP bool
	}{
		{
			id:     "add-dp-tier",
			target: dpAttrTarget("tier"),
			val:    strVal("core"),
			wantDP: true,
		},
		{
			id:     "add-dp-nested",
			target: dpAttrTarget("routing", "priority"),
			val:    intVal(1),
			wantDP: true,
		},
		{
			id:     "add-res-env",
			target: resourceAttrTarget("deployment.environment"),
			val:    strVal("prod"),
			wantDP: false,
		},
		{
			id:     "add-scope-team",
			target: scopeAttrTarget("owner.team"),
			val:    strVal("telemetry"),
			wantDP: false,
		},
	}

	for _, st := range steps {
		p, err := NewPolicyFromProto(&policyv1alpha1.MetricTransformPolicy{
			Id: st.id,
			Action: &policyv1alpha1.MetricTransformPolicy_Add{
				Add: &policyv1alpha1.MetricAddAction{
					Target: st.target,
					Value:  st.val,
				},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformStageAdd, p.TransformStage())
		assert.Equal(t, st.wantDP, p.IsDatapointLevel())
		assert.Equal(t, googlepolicy.TransformModified, p.TransformMetric(ctx))
		// Calling a second time must be skipped (upsert is always false on metrics).
		assert.Equal(t, googlepolicy.TransformNoMatch, p.TransformMetric(ctx))
	}

	v, ok := dp.Attributes().Get("tier")
	require.True(t, ok)
	assert.Equal(t, "core", v.Str())

	routing, ok := dp.Attributes().Get("routing")
	require.True(t, ok)
	prio, ok := routing.Map().Get("priority")
	require.True(t, ok)
	assert.Equal(t, int64(1), prio.Int())

	env, ok := rm.Resource().Attributes().Get("deployment.environment")
	require.True(t, ok)
	assert.Equal(t, "prod", env.Str())

	team, ok := sm.Scope().Attributes().Get("owner.team")
	require.True(t, ok)
	assert.Equal(t, "telemetry", team.Str())
}

func TestMetricAddAction_DescriptorAndScopeFields(t *testing.T) {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	m := sm.Metrics().AppendEmpty()
	dp := m.SetEmptyGauge().DataPoints().AppendEmpty()

	ctx := googlepolicy.MetricContext{
		Metric:              m,
		DatapointAttributes: dp.Attributes(),
		Resource:            rm.Resource(),
		Scope:               sm.Scope(),
	}

	fields := []struct {
		id     string
		target *policyv1alpha1.MetricFieldSelector
		val    string
		get    func() string
	}{
		{
			id:     "set-name",
			target: descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
			val:    "custom.metric",
			get:    func() string { return m.Name() },
		},
		{
			id:     "set-desc",
			target: descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION),
			val:    "Custom description",
			get:    func() string { return m.Description() },
		},
		{
			id:     "set-unit",
			target: descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT),
			val:    "By",
			get:    func() string { return m.Unit() },
		},
		{
			id:     "set-scope-name",
			target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
			val:    "custom.scope",
			get:    func() string { return sm.Scope().Name() },
		},
		{
			id:     "set-scope-version",
			target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION),
			val:    "2.0.0",
			get:    func() string { return sm.Scope().Version() },
		},
	}

	for _, tc := range fields {
		p, err := NewPolicyFromProto(&policyv1alpha1.MetricTransformPolicy{
			Id: tc.id,
			Action: &policyv1alpha1.MetricTransformPolicy_Add{
				Add: &policyv1alpha1.MetricAddAction{
					Target: tc.target,
					Value:  strVal(tc.val),
				},
			},
		})
		require.NoError(t, err)
		assert.False(t, p.IsDatapointLevel())
		assert.Equal(t, googlepolicy.TransformModified, p.TransformMetric(ctx))
		assert.Equal(t, tc.val, tc.get())
		// Subsequent call does not overwrite existing non-empty field.
		assert.Equal(t, googlepolicy.TransformNoMatch, p.TransformMetric(ctx))
	}
}

func TestMetricAddAction_InvalidFieldTargets(t *testing.T) {
	invalidTargets := []struct {
		name    string
		target  *policyv1alpha1.MetricFieldSelector
		val     *policyv1alpha1.Value
		wantErr string
	}{
		{
			name:    "descriptor TYPE",
			target:  descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE),
			val:     strVal("SUM"),
			wantErr: "is not supported as a transform target",
		},
		{
			name:    "descriptor AGGREGATION_TEMPORALITY",
			target:  descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY),
			val:     strVal("DELTA"),
			wantErr: "is not supported as a transform target",
		},
		{
			name:    "descriptor IS_MONOTONIC",
			target:  descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC),
			val:     &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BoolValue{BoolValue: true}},
			wantErr: "is not supported as a transform target",
		},
		{
			name:    "descriptor UNSPECIFIED",
			target:  descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNSPECIFIED),
			val:     strVal("x"),
			wantErr: "metric descriptor field cannot be unspecified",
		},
		{
			name:    "descriptor UNIT non-string",
			target:  descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT),
			val:     intVal(1),
			wantErr: "requires a string value",
		},
		{
			name:    "descriptor UNIT empty string",
			target:  descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT),
			val:     strVal(""),
			wantErr: "requires a non-empty string value",
		},
		{
			name:    "scope SCHEMA_URL",
			target:  scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL),
			val:     strVal("https://example.com"),
			wantErr: "scope field SCHEMA_URL is not supported as a transform target",
		},
		{
			name:    "scope UNSPECIFIED",
			target:  scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED),
			val:     strVal("x"),
			wantErr: "scope field cannot be unspecified",
		},
	}

	for _, tc := range invalidTargets {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPolicyFromProto(&policyv1alpha1.MetricTransformPolicy{
				Id: "bad-" + tc.name,
				Action: &policyv1alpha1.MetricTransformPolicy_Add{
					Add: &policyv1alpha1.MetricAddAction{
						Target: tc.target,
						Value:  tc.val,
					},
				},
			})
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestPolicyMetadataAndDriver(t *testing.T) {
	pb := &policyv1alpha1.MetricTransformPolicy{
		Id: "meta-test",
		Matches: []*policyv1alpha1.MetricMatcher{
			{
				Target:    dpAttrTarget("http.method"),
				Predicate: &policyv1alpha1.MetricMatcher_Equals{Equals: strVal("GET")},
			},
		},
		Action: &policyv1alpha1.MetricTransformPolicy_Add{
			Add: &policyv1alpha1.MetricAddAction{
				Target: resourceAttrTarget("env"),
				Value:  strVal("prod"),
			},
		},
	}
	p, err := NewPolicyFromProto(pb)
	require.NoError(t, err)
	assert.Equal(t, "meta-test", p.PolicyName())
	assert.Equal(t, PolicyType, p.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassTransformation, p.PolicyClass())
	assert.Equal(t, []googlepolicy.Signal{googlepolicy.SignalMetrics}, p.TargetSignals())
	assert.NoError(t, p.Validate())
	assert.Same(t, pb, p.Proto())
	assert.True(t, p.IsDatapointLevel(), "matcher on datapoint_attribute marks policy datapoint-level")
	assert.ErrorIs(t, (&Policy{}).Validate(), ErrNilProto)

	// Zero MetricContext is safe and returns TransformNoMatch.
	assert.Equal(t, googlepolicy.TransformNoMatch, p.TransformMetric(googlepolicy.MetricContext{}))

	// Load via googlepolicy registry.
	loaded, err := googlepolicy.LoadPolicy(PolicyType, map[string]any{
		"id":   "from-map",
		"type": PolicyType,
		"add": map[string]any{
			"target": map[string]any{
				"datapoint_attribute": map[string]any{
					"path": []any{"tier"},
				},
			},
			"value": map[string]any{
				"string_value": "gold",
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "from-map", loaded.PolicyName())
}
