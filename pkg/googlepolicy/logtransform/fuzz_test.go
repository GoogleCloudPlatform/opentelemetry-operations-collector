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

package logtransform

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func buildFuzzLogContexts(body, severityText, attrKey, attrStr string, severityNum int32, attrInt int64, attrFloat float64, attrBool bool) []googlepolicy.LogContext {
	lr, sl, rl := newTestLogBundle()

	edgeLogs := plog.NewLogs()
	edgeRL := edgeLogs.ResourceLogs().AppendEmpty()
	edgeSL := edgeRL.ScopeLogs().AppendEmpty()
	edgeLR := edgeSL.LogRecords().AppendEmpty()
	edgeLR.Body().SetInt(0)
	edgeAttrs := edgeLR.Attributes()
	edgeAttrs.PutDouble("nan_val", math.NaN())
	edgeAttrs.PutDouble("pos_inf", math.Inf(1))
	edgeAttrs.PutDouble("neg_inf", math.Inf(-1))
	edgeAttrs.PutEmpty("null_key")
	edgeAttrs.PutStr("empty_str", "")
	edgeAttrs.PutEmptySlice("empty_slice")
	edgeAttrs.PutEmptyMap("empty_map")

	return []googlepolicy.LogContext{
		logContext(lr, sl, rl),
		logContext(edgeLR, edgeSL, edgeRL),
		{},
		dynamicLogContext(body, severityText, attrKey, attrStr, severityNum, attrInt, attrFloat, attrBool),
	}
}

func dynamicLogContext(body, severityText, attrKey, attrStr string, severityNum int32, attrInt int64, attrFloat float64, attrBool bool) googlepolicy.LogContext {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr(attrKey, attrStr)

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName(attrStr)

	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr(body)
	lr.SetSeverityText(severityText)
	lr.SetSeverityNumber(plog.SeverityNumber(severityNum))

	attrs := lr.Attributes()
	attrs.PutStr(attrKey, attrStr)
	attrs.PutInt("int_attr", attrInt)
	attrs.PutDouble("float_attr", attrFloat)
	attrs.PutBool("bool_attr", attrBool)

	return logContext(lr, sl, rl)
}

func serializeLogContext(ctx googlepolicy.LogContext) ([]byte, error) {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.SetSchemaUrl(ctx.ResourceSchemaURL)
	if ctx.Resource != (pcommon.Resource{}) {
		ctx.Resource.CopyTo(rl.Resource())
	}
	sl := rl.ScopeLogs().AppendEmpty()
	sl.SetSchemaUrl(ctx.ScopeSchemaURL)
	if ctx.Scope != (pcommon.InstrumentationScope{}) {
		ctx.Scope.CopyTo(sl.Scope())
	}
	if ctx.Record != (plog.LogRecord{}) {
		ctx.Record.CopyTo(sl.LogRecords().AppendEmpty())
	}
	var m plog.ProtoMarshaler
	return m.MarshalLogs(ld)
}

func fuzzLogTransformSeedPolicies() []*policyv1alpha1.LogTransformPolicy {
	return []*policyv1alpha1.LogTransformPolicy{
		{
			Id: "projects/123/locations/us-central1/policySets/ps-1/policies/enrich-attributes-and-nested",
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target:    recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY),
					Predicate: &policyv1alpha1.LogMatcher_Regex{Regex: "^hello.*"},
				},
			},
			Action: &policyv1alpha1.LogTransformPolicy_Add{
				Add: &policyv1alpha1.LogAddAction{
					Target: logAttrTarget("tier"),
					Value:  strVal("soc"),
					Upsert: true,
				},
			},
		},
		{
			Id: "enrich-record-fields",
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target:    logAttrTarget("metadata", "env"),
					Predicate: &policyv1alpha1.LogMatcher_Equals{Equals: strVal("prod")},
				},
				{
					Target: recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER),
					Predicate: &policyv1alpha1.LogMatcher_Gte{
						Gte: &policyv1alpha1.NumericValue{
							Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 9},
						},
					},
				},
				{
					Target:    logAttrTarget("user.roles"),
					Predicate: &policyv1alpha1.LogMatcher_Contains{Contains: strVal("admin")},
					Negate:    true,
				},
			},
			Action: &policyv1alpha1.LogTransformPolicy_Add{
				Add: &policyv1alpha1.LogAddAction{
					Target: recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY),
					Value:  strVal("redacted"),
					Upsert: true,
				},
			},
		},
		{
			Id: "enrich-scope-fields",
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target:    resourceAttrTarget("cloud.zone"),
					Predicate: &policyv1alpha1.LogMatcher_Exists{Exists: &emptypb.Empty{}},
				},
				{
					Target:    scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
					Predicate: &policyv1alpha1.LogMatcher_Equals{Equals: strVal("my.library")},
				},
			},
			Action: &policyv1alpha1.LogTransformPolicy_Add{
				Add: &policyv1alpha1.LogAddAction{
					Target: scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
					Value:  strVal("override.library"),
					Upsert: true,
				},
			},
		},
		{
			Id: "remove-attribute",
			Action: &policyv1alpha1.LogTransformPolicy_Remove{
				Remove: &policyv1alpha1.LogRemoveAction{
					Target: logAttrTarget("feature.enabled"),
				},
			},
		},
	}
}

func verifyCompiledLogTransformPolicy(t *testing.T, p googlepolicy.Policy, contexts []googlepolicy.LogContext) []googlepolicy.TransformResult {
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

	eval, ok := p.(googlepolicy.LogTransformPolicyEvaluator)
	if !ok {
		t.Fatalf("policy %T does not implement LogTransformPolicyEvaluator", p)
	}
	if _, ok := p.(*Policy); !ok {
		t.Fatalf("unexpected concrete policy type %T", p)
	}

	results := make([]googlepolicy.TransformResult, len(contexts))
	for i, ctx := range contexts {
		res := eval.TransformLog(ctx)
		switch res {
		case googlepolicy.TransformNoMatch, googlepolicy.TransformModified:
		default:
			t.Fatalf("TransformLog returned unknown TransformResult %v", res)
		}
		if ctx == (googlepolicy.LogContext{}) && res != googlepolicy.TransformNoMatch {
			t.Fatalf("TransformLog on zero LogContext returned %v, want TransformNoMatch", res)
		}
		results[i] = res
	}
	return results
}

func FuzzLogTransformPolicy(f *testing.F) {
	f.Add([]byte{}, "", "", "", "", int32(0), int64(0), float64(0), false)
	for _, seed := range fuzzLogTransformSeedPolicies() {
		if wireBytes, err := proto.Marshal(seed); err == nil {
			f.Add(wireBytes, "hello world", "INFO", "cloud.zone", "us-central1-a", int32(9), int64(200), 1.25, true)
		}
		if jsonBytes, err := protojson.Marshal(seed); err == nil {
			f.Add(jsonBytes, "hello world", "INFO", "cloud.zone", "us-central1-a", int32(9), int64(200), 1.25, true)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte, body, sevText, attrKey, attrStr string, sevNum int32, attrInt int64, attrFloat float64, attrBool bool) {
		newContexts := func() []googlepolicy.LogContext {
			return buildFuzzLogContexts(body, sevText, attrKey, attrStr, sevNum, attrInt, attrFloat, attrBool)
		}

		pb := &policyv1alpha1.LogTransformPolicy{}
		if err := proto.Unmarshal(data, pb); err == nil {
			ps, err := googlepolicy.MakePolicySetFromProtos("fuzz-rev", []proto.Message{pb})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					protoCtxs := newContexts()
					protoResults := verifyCompiledLogTransformPolicy(t, entry.PolicyObj, protoCtxs)

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
							jsonResults := verifyCompiledLogTransformPolicy(t, rtEntry.PolicyObj, jsonCtxs)
							for i := range protoCtxs {
								if got, want := jsonResults[i], protoResults[i]; got != want {
									t.Fatalf("context[%d] TransformResult mismatch between proto (%v) and JSON (%v)", i, want, got)
								}
								protoBytes, err := serializeLogContext(protoCtxs[i])
								if err != nil {
									t.Fatalf("context[%d] failed to serialize proto LogContext: %v", i, err)
								}
								jsonBytes, err := serializeLogContext(jsonCtxs[i])
								if err != nil {
									t.Fatalf("context[%d] failed to serialize JSON LogContext: %v", i, err)
								}
								if !bytes.Equal(protoBytes, jsonBytes) {
									t.Fatalf("context[%d] mutated LogContext mismatch between proto and JSON policies", i)
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
					verifyCompiledLogTransformPolicy(t, entry.PolicyObj, newContexts())
				}
			}
		}
	})
}
