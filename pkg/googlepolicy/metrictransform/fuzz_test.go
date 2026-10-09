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
	"bytes"
	"encoding/json"
	"math"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func buildFuzzMetricContexts(name, unit, attrKey, attrStr string, attrInt int64, attrFloat float64, attrBool bool) []googlepolicy.MetricContext {
	m, dp, sm, rm := newTestMetricBundle()

	edgeMetrics := pmetric.NewMetrics()
	edgeRM := edgeMetrics.ResourceMetrics().AppendEmpty()
	edgeSM := edgeRM.ScopeMetrics().AppendEmpty()
	edgeM := edgeSM.Metrics().AppendEmpty()
	edgeDP := edgeM.SetEmptyGauge().DataPoints().AppendEmpty()
	edgeAttrs := edgeDP.Attributes()
	edgeAttrs.PutDouble("nan_val", math.NaN())
	edgeAttrs.PutDouble("pos_inf", math.Inf(1))
	edgeAttrs.PutDouble("neg_inf", math.Inf(-1))
	edgeAttrs.PutEmpty("null_key")
	edgeAttrs.PutStr("empty_str", "")
	edgeAttrs.PutEmptySlice("empty_slice")
	edgeAttrs.PutEmptyMap("empty_map")

	return []googlepolicy.MetricContext{
		metricContext(m, dp, sm, rm),
		{
			Metric:              edgeM,
			DatapointAttributes: edgeDP.Attributes(),
			Resource:            edgeRM.Resource(),
			Scope:               edgeSM.Scope(),
		},
		{},
		dynamicMetricContext(name, unit, attrKey, attrStr, attrInt, attrFloat, attrBool),
	}
}

func dynamicMetricContext(name, unit, attrKey, attrStr string, attrInt int64, attrFloat float64, attrBool bool) googlepolicy.MetricContext {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr(attrKey, attrStr)

	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName(attrStr)

	m := sm.Metrics().AppendEmpty()
	m.SetName(name)
	m.SetUnit(unit)
	sum := m.SetEmptySum()
	sum.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
	sum.SetIsMonotonic(attrBool)
	dp := sum.DataPoints().AppendEmpty()

	attrs := dp.Attributes()
	attrs.PutStr(attrKey, attrStr)
	attrs.PutInt("int_attr", attrInt)
	attrs.PutDouble("float_attr", attrFloat)
	attrs.PutBool("bool_attr", attrBool)

	return metricContext(m, dp, sm, rm)
}

func serializeMetricContext(ctx googlepolicy.MetricContext) ([]byte, error) {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	rm.SetSchemaUrl(ctx.ResourceSchemaURL)
	if ctx.Resource != (pcommon.Resource{}) {
		ctx.Resource.CopyTo(rm.Resource())
	}
	sm := rm.ScopeMetrics().AppendEmpty()
	sm.SetSchemaUrl(ctx.ScopeSchemaURL)
	if ctx.Scope != (pcommon.InstrumentationScope{}) {
		ctx.Scope.CopyTo(sm.Scope())
	}
	if ctx.Metric != (pmetric.Metric{}) {
		ctx.Metric.CopyTo(sm.Metrics().AppendEmpty())
	}
	var m pmetric.ProtoMarshaler
	return m.MarshalMetrics(md)
}

func fuzzMetricTransformSeedPolicies() []*policyv1alpha1.MetricTransformPolicy {
	return []*policyv1alpha1.MetricTransformPolicy{
		{
			Id: "projects/123/locations/us-central1/policySets/ps-1/policies/add-datapoint-tier",
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target:    descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME),
					Predicate: &policyv1alpha1.MetricMatcher_Regex{Regex: "^http\\..*"},
				},
			},
			Action: &policyv1alpha1.MetricTransformPolicy_Add{
				Add: &policyv1alpha1.MetricAddAction{
					Target: dpAttrTarget("tier"),
					Value:  strVal("core"),
				},
			},
		},
		{
			Id: "add-descriptor-unit",
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target:    resourceAttrTarget("cloud.zone"),
					Predicate: &policyv1alpha1.MetricMatcher_Exists{Exists: &emptypb.Empty{}},
				},
			},
			Action: &policyv1alpha1.MetricTransformPolicy_Add{
				Add: &policyv1alpha1.MetricAddAction{
					Target: descriptorFieldTarget(policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT),
					Value:  strVal("ms"),
				},
			},
		},
		{
			Id: "add-scope-attribute",
			Action: &policyv1alpha1.MetricTransformPolicy_Add{
				Add: &policyv1alpha1.MetricAddAction{
					Target: scopeAttrTarget("pipeline"),
					Value:  strVal("metrics-default"),
				},
			},
		},
		{
			Id: "rename-datapoint-attribute",
			Matches: []*policyv1alpha1.MetricMatcher{
				{
					Target:    dpAttrTarget("http.method"),
					Predicate: &policyv1alpha1.MetricMatcher_Exists{Exists: &emptypb.Empty{}},
				},
			},
			Action: &policyv1alpha1.MetricTransformPolicy_Rename{
				Rename: &policyv1alpha1.MetricRenameAction{
					From: dpAttrTarget("http.method"),
					To:   dpAttrTarget("rpc.method"),
				},
			},
		},
	}
}

func verifyCompiledMetricTransformPolicy(t *testing.T, p googlepolicy.Policy, contexts []googlepolicy.MetricContext) []googlepolicy.TransformResult {
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

	eval, ok := p.(googlepolicy.MetricTransformPolicyEvaluator)
	if !ok {
		t.Fatalf("policy %T does not implement MetricTransformPolicyEvaluator", p)
	}
	if _, ok := p.(*Policy); !ok {
		t.Fatalf("unexpected concrete policy type %T", p)
	}

	results := make([]googlepolicy.TransformResult, len(contexts))
	for i, ctx := range contexts {
		res := eval.TransformMetric(ctx)
		switch res {
		case googlepolicy.TransformNoMatch, googlepolicy.TransformModified:
		default:
			t.Fatalf("TransformMetric returned unknown TransformResult %v", res)
		}
		if ctx == (googlepolicy.MetricContext{}) && res != googlepolicy.TransformNoMatch {
			t.Fatalf("TransformMetric on zero MetricContext returned %v, want TransformNoMatch", res)
		}
		results[i] = res
	}
	return results
}

func FuzzMetricTransformPolicy(f *testing.F) {
	f.Add([]byte{}, "", "", "", "", int64(0), float64(0), false)
	for _, seed := range fuzzMetricTransformSeedPolicies() {
		if wireBytes, err := proto.Marshal(seed); err == nil {
			f.Add(wireBytes, "http.server.duration", "", "cloud.zone", "us-central1-a", int64(200), 1.25, true)
		}
		if jsonBytes, err := protojson.Marshal(seed); err == nil {
			f.Add(jsonBytes, "http.server.duration", "", "cloud.zone", "us-central1-a", int64(200), 1.25, true)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte, name, unit, attrKey, attrStr string, attrInt int64, attrFloat float64, attrBool bool) {
		newContexts := func() []googlepolicy.MetricContext {
			return buildFuzzMetricContexts(name, unit, attrKey, attrStr, attrInt, attrFloat, attrBool)
		}

		pb := &policyv1alpha1.MetricTransformPolicy{}
		if err := proto.Unmarshal(data, pb); err == nil {
			ps, err := googlepolicy.MakePolicySetFromProtos("fuzz-rev", []proto.Message{pb})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					protoCtxs := newContexts()
					protoResults := verifyCompiledMetricTransformPolicy(t, entry.PolicyObj, protoCtxs)

					if rtBytes, err := protojson.Marshal(pb); err == nil {
						var rtMap map[string]any
						if err := json.Unmarshal(rtBytes, &rtMap); err == nil && rtMap != nil {
							rtMap["type"] = PolicyType
							rtPS, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{rtMap})
							rtEntry, ok := rtPS.Policies[entry.PolicyObj.PolicyName()]
							if err != nil || len(rtPS.Policies) != 1 || !ok {
								t.Fatalf("protojson round-trip failed MakePolicySet: err=%v, len=%d, ok=%v", err, len(rtPS.Policies), ok)
							}
							jsonCtxs := newContexts()
							jsonResults := verifyCompiledMetricTransformPolicy(t, rtEntry.PolicyObj, jsonCtxs)
							for i := range protoCtxs {
								if got, want := jsonResults[i], protoResults[i]; got != want {
									t.Fatalf("context[%d] TransformResult mismatch between proto (%v) and JSON (%v)", i, want, got)
								}
								protoBytes, err := serializeMetricContext(protoCtxs[i])
								if err != nil {
									t.Fatalf("context[%d] failed to serialize proto MetricContext: %v", i, err)
								}
								jsonBytes, err := serializeMetricContext(jsonCtxs[i])
								if err != nil {
									t.Fatalf("context[%d] failed to serialize JSON MetricContext: %v", i, err)
								}
								if !bytes.Equal(protoBytes, jsonBytes) {
									t.Fatalf("context[%d] mutated MetricContext mismatch between proto and JSON policies", i)
								}
							}
						}
					}
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
					verifyCompiledMetricTransformPolicy(t, entry.PolicyObj, newContexts())
				}
			}
		}
	})
}
