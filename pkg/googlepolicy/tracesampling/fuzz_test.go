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

package tracesampling

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	policyv1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var testFuzzTraceID = pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
var testFuzzSpanID = pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})

func fuzzTraceSamplingContexts() []googlepolicy.TraceContext {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "frontend")

	ss := rs.ScopeSpans().AppendEmpty()
	ss.Scope().SetName("http.client")

	span := ss.Spans().AppendEmpty()
	span.SetTraceID(testFuzzTraceID)
	span.SetSpanID(testFuzzSpanID)
	span.SetName("/api/checkout")
	span.SetKind(ptrace.SpanKindServer)
	span.Status().SetCode(ptrace.StatusCodeOk)
	span.Attributes().PutStr("http.method", "POST")
	span.Attributes().PutInt("http.status_code", 200)

	edgeTraces := ptrace.NewTraces()
	edgeRS := edgeTraces.ResourceSpans().AppendEmpty()
	edgeSS := edgeRS.ScopeSpans().AppendEmpty()
	edgeSpan := edgeSS.Spans().AppendEmpty()
	edgeAttrs := edgeSpan.Attributes()
	edgeAttrs.PutDouble("nan_val", math.NaN())
	edgeAttrs.PutDouble("pos_inf", math.Inf(1))
	edgeAttrs.PutDouble("neg_inf", math.Inf(-1))
	edgeAttrs.PutEmpty("null_key")
	edgeAttrs.PutStr("empty_str", "")

	return []googlepolicy.TraceContext{
		googlepolicy.TraceContext{
			Span:     span,
			Resource: rs.Resource(),
			Scope:    ss.Scope(),
		},
		googlepolicy.TraceContext{
			Span:     edgeSpan,
			Resource: edgeRS.Resource(),
			Scope:    edgeSS.Scope(),
		},
		{},
	}
}

func dynamicSamplingTraceContext(name, serviceName, httpMethod string, statusCode int32, traceIDBytes [16]byte, traceStateStr string) googlepolicy.TraceContext {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", serviceName)

	ss := rs.ScopeSpans().AppendEmpty()
	span := ss.Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID(traceIDBytes))
	span.SetSpanID(testFuzzSpanID)
	span.SetName(name)
	span.Status().SetCode(ptrace.StatusCode(statusCode))
	span.TraceState().FromRaw(traceStateStr)

	attrs := span.Attributes()
	attrs.PutStr("http.method", httpMethod)

	return googlepolicy.TraceContext{
		Span:     span,
		Resource: rs.Resource(),
		Scope:    ss.Scope(),
	}
}

func fuzzTraceSamplingSeedPolicies() []*policyv1.TraceSamplingPolicy {
	return []*policyv1.TraceSamplingPolicy{
		{
			Id:              "projects/123/locations/us-central1/policySets/ps-1/policies/sample-50",
			SamplingPercent: 50.0,
		},
		{
			Id:              "sample-100-all",
			SamplingPercent: 100.0,
		},
		{
			Id:              "sample-10-noisy-endpoints",
			SamplingPercent: 10.0,
			Matchers: []*policyv1.TraceMatcher{
				{
					Target: &policyv1.TraceFieldSelector{
						Target: &policyv1.TraceFieldSelector_SpanAttributePath{
							SpanAttributePath: &policyv1.AttributePath{
								Segments: []string{"http.method"},
							},
						},
					},
					Predicate: &policyv1.Predicate{
						Kind: &policyv1.Predicate_Equality{
							Equality: &policyv1.Value{
								Kind: &policyv1.Value_StringValue{StringValue: "GET"},
							},
						},
					},
				},
			},
		},
		{
			Id:              "sample-0-01-edge",
			SamplingPercent: 0.01,
			Matchers: []*policyv1.TraceMatcher{
				{
					Target: &policyv1.TraceFieldSelector{
						Target: &policyv1.TraceFieldSelector_RecordField{
							RecordField: policyv1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
						},
					},
					Predicate: &policyv1.Predicate{
						Kind: &policyv1.Predicate_Glob{
							Glob: "/healthz*",
						},
					},
				},
			},
		},
	}
}

func verifyCompiledSamplingPolicy(t *testing.T, p googlepolicy.Policy, contexts []googlepolicy.TraceContext) {
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

	evaluator, ok := p.(googlepolicy.TraceSamplingPolicyEvaluator)
	if !ok {
		t.Fatalf("policy %T does not implement TraceSamplingPolicyEvaluator", p)
	}

	if evaluator.SamplingPercent() <= 0.0 || evaluator.SamplingPercent() > 100.0 || math.IsNaN(evaluator.SamplingPercent()) {
		t.Fatalf("invalid SamplingPercent(): %v", evaluator.SamplingPercent())
	}

	for _, ctx := range contexts {
		_ = evaluator.MatchesTrace(ctx)
		if ctx.Span != (ptrace.Span{}) {
			cloneSpan := ptrace.NewSpan()
			ctx.Span.CopyTo(cloneSpan)
			_, _, _ = EvaluateSampling(cloneSpan, evaluator.Threshold())
		}
	}
}

func FuzzTraceSamplingPolicy(f *testing.F) {
	seeds := fuzzTraceSamplingSeedPolicies()
	for _, seed := range seeds {
		if _, err := NewPolicyFromProto(seed); err != nil {
			f.Fatalf("seed policy %q failed NewPolicyFromProto: %v", seed.GetId(), err)
		}

		wireBytes, err := proto.Marshal(seed)
		if err != nil {
			f.Fatalf("failed to marshal seed proto %q to wire bytes: %v", seed.GetId(), err)
		}
		f.Add(wireBytes)

		jsonBytes, err := protojson.Marshal(seed)
		if err != nil {
			f.Fatalf("failed to marshal seed proto %q to protojson: %v", seed.GetId(), err)
		}
		f.Add(jsonBytes)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		contexts := fuzzTraceSamplingContexts()

		var pb policyv1.TraceSamplingPolicy
		if err := proto.Unmarshal(data, &pb); err == nil {
			ps, err := googlepolicy.MakePolicySetFromProtos("projects/p/locations/l/policySets/ps", "fuzz-rev", []proto.Message{&pb})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}

				var traceID [16]byte
				if len(data) >= 16 {
					copy(traceID[:], data[:16])
				} else {
					binary.BigEndian.PutUint64(traceID[8:], 0x12345678)
				}
				dynCtx := dynamicSamplingTraceContext(pb.GetId(), "svc", "GET", 0, traceID, "")
				allContexts := make([]googlepolicy.TraceContext, 0, len(contexts)+1)
				allContexts = append(allContexts, contexts...)
				allContexts = append(allContexts, dynCtx)

				for _, entry := range ps.Policies {
					verifyCompiledSamplingPolicy(t, entry.PolicyObj, allContexts)

					if rtBytes, err := protojson.Marshal(&pb); err == nil {
						var rtMap map[string]any
						if err := json.Unmarshal(rtBytes, &rtMap); err == nil && rtMap != nil {
							rtMap["type"] = PolicyType
							rtPS, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{rtMap})
							rtEntry, ok := rtPS.Policies[entry.PolicyObj.PolicyName()]
							if err != nil || len(rtPS.Policies) != 1 || !ok {
								t.Fatalf("protojson round-trip failed MakePolicySet: err=%v, len=%d, ok=%v", err, len(rtPS.Policies), ok)
							}
							verifyCompiledSamplingPolicy(t, rtEntry.PolicyObj, allContexts)
							protoEval := entry.PolicyObj.(googlepolicy.TraceSamplingPolicyEvaluator)
							jsonEval := rtEntry.PolicyObj.(googlepolicy.TraceSamplingPolicyEvaluator)
							if got, want := jsonEval.Threshold(), protoEval.Threshold(); got != want {
								t.Fatalf("Threshold mismatch between proto (%v) and JSON (%v)", want, got)
							}
							if got, want := jsonEval.SamplingPercent(), protoEval.SamplingPercent(); got != want {
								t.Fatalf("SamplingPercent mismatch between proto (%v) and JSON (%v)", want, got)
							}
							for i, ctx := range allContexts {
								if got, want := jsonEval.MatchesTrace(ctx), protoEval.MatchesTrace(ctx); got != want {
									t.Fatalf("context[%d] MatchesTrace mismatch between proto (%v) and JSON (%v)", i, want, got)
								}
							}
						}
					}
				}
			}
		}

		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err == nil && raw != nil {
			raw["type"] = PolicyType
			ps, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{raw})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					verifyCompiledSamplingPolicy(t, entry.PolicyObj, contexts)
				}
			}
		}

		if err := protojson.Unmarshal(data, &pb); err == nil {
			jsonBytes, err := protojson.Marshal(&pb)
			if err == nil {
				var jsonMap map[string]any
				if err := json.Unmarshal(jsonBytes, &jsonMap); err == nil && jsonMap != nil {
					jsonMap["type"] = PolicyType
					ps, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{jsonMap})
					if err == nil {
						if len(ps.Policies) != 1 {
							t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
						}
						for _, entry := range ps.Policies {
							verifyCompiledSamplingPolicy(t, entry.PolicyObj, contexts)
						}
					}
				}
			}
		}
	})
}
