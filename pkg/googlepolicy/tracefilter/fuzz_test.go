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

package tracefilter

import (
	"encoding/json"
	"math"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func fuzzTraceContexts() []googlepolicy.TraceContext {
	span, ss, rs := newTestSpanBundle()

	edgeTraces := ptrace.NewTraces()
	edgeRS := edgeTraces.ResourceSpans().AppendEmpty()
	edgeSS := edgeRS.ScopeSpans().AppendEmpty()
	rootSpan := edgeSS.Spans().AppendEmpty()
	edgeAttrs := rootSpan.Attributes()
	edgeAttrs.PutDouble("nan_val", math.NaN())
	edgeAttrs.PutDouble("pos_inf", math.Inf(1))
	edgeAttrs.PutDouble("neg_inf", math.Inf(-1))
	edgeAttrs.PutEmpty("null_key")
	edgeAttrs.PutStr("empty_str", "")
	edgeAttrs.PutEmptySlice("empty_slice")
	edgeAttrs.PutEmptyMap("empty_map")

	return []googlepolicy.TraceContext{
		traceContext(span, ss, rs),
		traceContext(rootSpan, edgeSS, edgeRS),
		{},
	}
}

func dynamicTraceContext(name, statusMsg, attrKey, attrStr string, kind, statusCode int32, attrInt int64, attrFloat float64, attrBool bool) googlepolicy.TraceContext {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr(attrKey, attrStr)

	ss := rs.ScopeSpans().AppendEmpty()
	ss.Scope().SetName(attrStr)

	span := ss.Spans().AppendEmpty()
	span.SetName(name)
	span.SetKind(ptrace.SpanKind(kind))
	span.Status().SetCode(ptrace.StatusCode(statusCode))
	span.Status().SetMessage(statusMsg)

	attrs := span.Attributes()
	attrs.PutStr(attrKey, attrStr)
	attrs.PutInt("int_attr", attrInt)
	attrs.PutDouble("float_attr", attrFloat)
	attrs.PutBool("bool_attr", attrBool)

	return traceContext(span, ss, rs)
}

func fuzzTraceSeedPolicies() []*policyv1alpha1.TraceFilterPolicy {
	return []*policyv1alpha1.TraceFilterPolicy{
		{
			Id: "projects/123/locations/us-central1/policySets/ps-1/policies/drop-healthcheck-span",
			Matchers: []*policyv1alpha1.TraceMatcher{
				{
					Target: recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
					Predicate: &policyv1alpha1.TraceMatcher_Glob{
						Glob: "healthcheck.*",
					},
				},
			},
		},
		{
			Id: "drop-server-spans",
			Matchers: []*policyv1alpha1.TraceMatcher{
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND),
					Predicate: equalsString("SERVER"),
				},
				{
					Target:    spanAttrTarget("metadata", "env"),
					Predicate: equalsString("prod"),
				},
				{
					Target: spanAttrTarget("http.status_code"),
					Predicate: &policyv1alpha1.TraceMatcher_InclusiveMinimum{
						InclusiveMinimum: &policyv1alpha1.NumericValue{
							Kind: &policyv1alpha1.NumericValue_IntValue{IntValue: 200},
						},
					},
				},
			},
		},
		{
			Id: "drop-root-spans-and-trace-id",
			Matchers: []*policyv1alpha1.TraceMatcher{
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID),
					Predicate: &policyv1alpha1.TraceMatcher_Existence{Existence: &emptypb.Empty{}},
					Negated:    true,
				},
				{
					Target: recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID),
					Predicate: &policyv1alpha1.TraceMatcher_Equality{
						Equality: &policyv1alpha1.Value{
							Kind: &policyv1alpha1.Value_BytesValue{
								BytesValue: testTraceIDBytes[:],
							},
						},
					},
				},
				{
					Target:    scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
					Predicate: equalsString("my.library"),
				},
			},
		},
	}
}

func verifyCompiledTracePolicy(t *testing.T, p googlepolicy.Policy, contexts []googlepolicy.TraceContext) {
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

	eval, ok := p.(googlepolicy.TraceFilterPolicyEvaluator)
	if !ok {
		t.Fatalf("policy %T does not implement TraceFilterPolicyEvaluator", p)
	}
	if _, ok := p.(*Policy); !ok {
		t.Fatalf("unexpected concrete policy type %T", p)
	}

	for _, ctx := range contexts {
		res := eval.FilterTrace(ctx)
		switch res {
		case googlepolicy.FilterNoMatch, googlepolicy.FilterDrop:
		default:
			t.Fatalf("FilterTrace returned unexpected FilterResult %v", res)
		}
	}
}

func FuzzTraceFilterPolicy(f *testing.F) {
	f.Add([]byte{}, "", "", "", "", int32(0), int32(0), int64(0), float64(0), false)
	for _, seed := range fuzzTraceSeedPolicies() {
		if _, err := NewPolicyFromProto(seed); err != nil {
			f.Fatalf("invalid seed policy %q: %v", seed.GetId(), err)
		}
		wireBytes, err := proto.Marshal(seed)
		if err != nil {
			f.Fatalf("proto.Marshal seed %q: %v", seed.GetId(), err)
		}
		f.Add(wireBytes, "healthcheck.ping", "upstream unavailable", "cloud.zone", "us-central1-a", int32(ptrace.SpanKindServer), int32(ptrace.StatusCodeError), int64(200), 1.25, true)
		jsonBytes, err := protojson.Marshal(seed)
		if err != nil {
			f.Fatalf("protojson.Marshal seed %q: %v", seed.GetId(), err)
		}
		f.Add(jsonBytes, "healthcheck.ping", "upstream unavailable", "cloud.zone", "us-central1-a", int32(ptrace.SpanKindServer), int32(ptrace.StatusCodeError), int64(200), 1.25, true)
	}

	staticContexts := fuzzTraceContexts()

	f.Fuzz(func(t *testing.T, data []byte, name, statusMsg, attrKey, attrStr string, kind, statusCode int32, attrInt int64, attrFloat float64, attrBool bool) {
		var contexts []googlepolicy.TraceContext
		getContexts := func() []googlepolicy.TraceContext {
			if contexts == nil {
				contexts = make([]googlepolicy.TraceContext, 0, len(staticContexts)+1)
				contexts = append(contexts, staticContexts...)
				contexts = append(contexts, dynamicTraceContext(name, statusMsg, attrKey, attrStr, kind, statusCode, attrInt, attrFloat, attrBool))
			}
			return contexts
		}

		pb := &policyv1alpha1.TraceFilterPolicy{}
		if err := proto.Unmarshal(data, pb); err == nil {
			ps, err := googlepolicy.MakePolicySetFromProtos("projects/p/locations/l/policySets/ps", "fuzz-rev", []proto.Message{pb})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				ctxs := getContexts()
				for _, entry := range ps.Policies {
					verifyCompiledTracePolicy(t, entry.PolicyObj, ctxs)

					if rtBytes, err := protojson.Marshal(pb); err == nil {
						var rtMap map[string]any
						if err := json.Unmarshal(rtBytes, &rtMap); err == nil && rtMap != nil {
							rtMap["type"] = PolicyType
							rtPS, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{rtMap})
							rtEntry, ok := rtPS.Policies[entry.PolicyObj.PolicyName()]
							if err != nil || len(rtPS.Policies) != 1 || !ok {
								t.Fatalf("protojson round-trip failed MakePolicySet: err=%v, len=%d, ok=%v", err, len(rtPS.Policies), ok)
							}
							verifyCompiledTracePolicy(t, rtEntry.PolicyObj, ctxs)
							protoEval := entry.PolicyObj.(googlepolicy.TraceFilterPolicyEvaluator)
							jsonEval := rtEntry.PolicyObj.(googlepolicy.TraceFilterPolicyEvaluator)
							for i, ctx := range ctxs {
								if got, want := jsonEval.FilterTrace(ctx), protoEval.FilterTrace(ctx); got != want {
									t.Fatalf("context[%d] FilterResult mismatch between proto (%v) and JSON (%v)", i, want, got)
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
				ctxs := getContexts()
				for _, entry := range ps.Policies {
					verifyCompiledTracePolicy(t, entry.PolicyObj, ctxs)
				}
			}
		}
	})
}
