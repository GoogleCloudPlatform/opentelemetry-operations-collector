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
			Id:     "projects/123/locations/us-central1/policySets/ps-1/policies/drop-healthcheck-span",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target: recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME),
					Predicate: &policyv1alpha1.TraceMatcher_Regex{
						Regex: "^healthcheck\\..*",
					},
				},
			},
		},
		{
			Id:     "keep-error-server-spans",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND),
					Predicate: equalsString("SERVER"),
				},
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE),
					Predicate: equalsString("ERROR"),
				},
				{
					Target:    spanAttrTarget("items", "0", "id"),
					Predicate: equalsString("item-001"),
				},
				{
					Target: spanAttrTarget("http.status_code"),
					Predicate: &policyv1alpha1.TraceMatcher_Gte{
						Gte: &policyv1alpha1.NumericValue{
							Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 200},
						},
					},
				},
			},
		},
		{
			Id:     "drop-root-spans-and-trace-id",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.TraceMatcher{
				{
					Target:    recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID),
					Predicate: &policyv1alpha1.TraceMatcher_Exists{Exists: &emptypb.Empty{}},
					Negate:    true,
				},
				{
					Target: recordFieldTarget(policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID),
					Predicate: &policyv1alpha1.TraceMatcher_Equals{
						Equals: &policyv1alpha1.Value{
							Value: &policyv1alpha1.Value_BytesValue{
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

	eval, ok := p.(googlepolicy.TracePolicyEvaluator)
	if !ok {
		t.Fatalf("policy %T does not implement TracePolicyEvaluator", p)
	}
	pol, ok := p.(*Policy)
	if !ok {
		t.Fatalf("unexpected concrete policy type %T", p)
	}

	for _, ctx := range contexts {
		res := eval.EvaluateTrace(ctx)
		switch res {
		case googlepolicy.EvalNoMatch:
		case googlepolicy.EvalKeep:
			if pol.Action() != policyv1alpha1.Action_ACTION_KEEP {
				t.Fatalf("EvaluateTrace returned EvalKeep for action %v", pol.Action())
			}
		case googlepolicy.EvalDrop:
			if pol.Action() != policyv1alpha1.Action_ACTION_DROP {
				t.Fatalf("EvaluateTrace returned EvalDrop for action %v", pol.Action())
			}
		default:
			t.Fatalf("EvaluateTrace returned unknown EvalResult %v", res)
		}
	}
}

func FuzzTraceFilterPolicy(f *testing.F) {
	f.Add([]byte{}, "", "", "", "", int32(0), int32(0), int64(0), float64(0), false)
	for _, seed := range fuzzTraceSeedPolicies() {
		if wireBytes, err := proto.Marshal(seed); err == nil {
			f.Add(wireBytes, "healthcheck.ping", "upstream unavailable", "cloud.zone", "us-central1-a", int32(ptrace.SpanKindServer), int32(ptrace.StatusCodeError), int64(200), 1.25, true)
		}
		if jsonBytes, err := protojson.Marshal(seed); err == nil {
			f.Add(jsonBytes, "healthcheck.ping", "upstream unavailable", "cloud.zone", "us-central1-a", int32(ptrace.SpanKindServer), int32(ptrace.StatusCodeError), int64(200), 1.25, true)
		}
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
			ps, err := googlepolicy.MakePolicySetFromProtos("fuzz-rev", []proto.Message{pb})
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
							protoEval := entry.PolicyObj.(googlepolicy.TracePolicyEvaluator)
							jsonEval := rtEntry.PolicyObj.(googlepolicy.TracePolicyEvaluator)
							for i, ctx := range ctxs {
								if got, want := jsonEval.EvaluateTrace(ctx), protoEval.EvaluateTrace(ctx); got != want {
									t.Fatalf("context[%d] EvalResult mismatch between proto (%v) and JSON (%v)", i, want, got)
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
