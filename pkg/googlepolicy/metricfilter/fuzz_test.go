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
	"encoding/json"
	"math"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func fuzzMetricContexts() []googlepolicy.MetricContext {
	m, sm, rm := newTestMetricBundle()

	edgeMetrics := pmetric.NewMetrics()
	edgeRM := edgeMetrics.ResourceMetrics().AppendEmpty()
	edgeSM := edgeRM.ScopeMetrics().AppendEmpty()
	untypedMetric := edgeSM.Metrics().AppendEmpty()

	gaugeMetric := edgeSM.Metrics().AppendEmpty()
	gaugeMetric.SetName("system.cpu.utilization")
	dp := gaugeMetric.SetEmptyGauge().DataPoints().AppendEmpty()
	dpAttrs := dp.Attributes()
	dpAttrs.PutDouble("nan_val", math.NaN())
	dpAttrs.PutDouble("pos_inf", math.Inf(1))
	dpAttrs.PutDouble("neg_inf", math.Inf(-1))
	dpAttrs.PutEmpty("null_key")
	dpAttrs.PutStr("empty_str", "")
	dpAttrs.PutEmptySlice("empty_slice")
	dpAttrs.PutEmptyMap("empty_map")

	return []googlepolicy.MetricContext{
		metricContext(m, sm, rm),
		metricContext(untypedMetric, edgeSM, edgeRM),
		metricContext(gaugeMetric, edgeSM, edgeRM),
		{},
	}
}

func fuzzMetricSeedPolicies() []*policyv1alpha1.MetricFilterPolicy {
	return []*policyv1alpha1.MetricFilterPolicy{
		{
			Id:     "projects/123/locations/us-central1/policySets/ps-1/policies/drop-metric-name",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
					Predicate: &policyv1alpha1.MetricMatcher_Regex{
						Regex: "^http\\.server\\..*",
					},
				},
			},
		},
		{
			Id:     "keep-sum-datapoint-attr",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("SUM"),
					},
				},
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_BoolValue{BoolValue: true},
						},
					},
				},
				{
					Target: datapointTarget("items", "0", "id"),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("item-001"),
					},
				},
				{
					Target: datapointTarget("http.status_code"),
					Predicate: &policyv1alpha1.MetricMatcher_Gte{
						Gte: &policyv1alpha1.NumericValue{
							Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 200},
						},
					},
				},
			},
		},
		{
			Id:     "drop-temporality-and-scope",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target: descriptorTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY),
					Predicate: &policyv1alpha1.MetricMatcher_Equals{
						Equals: stringValue("DELTA"),
					},
				},
				{
					Target:    resourceTarget("cloud.zone"),
					Predicate: &policyv1alpha1.MetricMatcher_Exists{Exists: &emptypb.Empty{}},
				},
				{
					Target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
					Predicate: &policyv1alpha1.MetricMatcher_Contains{
						Contains: stringValue("library"),
					},
				},
			},
		},
	}
}

func verifyCompiledMetricPolicy(t *testing.T, p googlepolicy.Policy, contexts []googlepolicy.MetricContext) {
	t.Helper()
	if err := p.Validate(); err != nil {
		t.Fatalf("compiled policy failed Validate(): %v", err)
	}
	if p.PolicyName() == "" {
		t.Fatal("compiled policy has empty PolicyName()")
	}
	if p.PolicyType() != PolicyType {
		t.Fatalf("unexpected PolicyType(): %q", p.PolicyType())
	}
	if p.PolicyClass() != googlepolicy.PolicyClassTransformation {
		t.Fatalf("unexpected PolicyClass(): %q", p.PolicyClass())
	}

	eval, ok := p.(googlepolicy.MetricPolicyEvaluator)
	if !ok {
		t.Fatalf("policy %T does not implement MetricPolicyEvaluator", p)
	}
	_ = eval.IsDatapointLevel()
	pol, ok := p.(*Policy)
	if !ok {
		t.Fatalf("unexpected concrete policy type %T", p)
	}

	for _, ctx := range contexts {
		res := eval.EvaluateMetric(ctx)
		switch res {
		case googlepolicy.EvalNoMatch:
		case googlepolicy.EvalKeep:
			if pol.Action() != policyv1alpha1.Action_ACTION_KEEP {
				t.Fatalf("EvaluateMetric returned EvalKeep for action %v", pol.Action())
			}
		case googlepolicy.EvalDrop:
			if pol.Action() != policyv1alpha1.Action_ACTION_DROP {
				t.Fatalf("EvaluateMetric returned EvalDrop for action %v", pol.Action())
			}
		default:
			t.Fatalf("EvaluateMetric returned unknown EvalResult %v", res)
		}
	}
}

func FuzzMetricFilterPolicy(f *testing.F) {
	f.Add([]byte{})
	for _, seed := range fuzzMetricSeedPolicies() {
		if wireBytes, err := proto.Marshal(seed); err == nil {
			f.Add(wireBytes)
		}
		if jsonBytes, err := protojson.Marshal(seed); err == nil {
			f.Add(jsonBytes)
		}
	}

	contexts := fuzzMetricContexts()

	f.Fuzz(func(t *testing.T, data []byte) {
		pb := &policyv1alpha1.MetricFilterPolicy{}
		if err := proto.Unmarshal(data, pb); err == nil {
			ps, err := googlepolicy.MakePolicySetFromProtos("fuzz-rev", []proto.Message{pb})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					verifyCompiledMetricPolicy(t, entry.PolicyObj, contexts)
				}
			}
		}

		var rawMap map[string]any
		if err := json.Unmarshal(data, &rawMap); err == nil && rawMap != nil {
			rawMap["type"] = PolicyType
			ps, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{rawMap})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					verifyCompiledMetricPolicy(t, entry.PolicyObj, contexts)
				}
			}
		}
	})
}
