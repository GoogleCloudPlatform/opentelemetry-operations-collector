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

package logfilter

import (
	"encoding/json"
	"math"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func fuzzLogContexts() []googlepolicy.LogContext {
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
	}
}

func fuzzLogSeedPolicies() []*policyv1alpha1.LogFilterPolicy {
	return []*policyv1alpha1.LogFilterPolicy{
		{
			Id:     "projects/123/locations/us-central1/policySets/ps-1/policies/drop-body-regex",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target:    recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY),
					Predicate: &policyv1alpha1.LogMatcher_Regex{Regex: "^hello.*"},
				},
			},
		},
		{
			Id:     "keep-nested-attr-and-severity",
			Action: policyv1alpha1.Action_ACTION_KEEP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target:    logAttrTarget("items", "0", "id"),
					Predicate: equalsString("item-001"),
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
					Predicate: containsString("admin"),
					Negate:    true,
				},
			},
		},
		{
			Id:     "drop-resource-and-scope",
			Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target:    resourceAttrTarget("cloud.zone"),
					Predicate: &policyv1alpha1.LogMatcher_Exists{Exists: &emptypb.Empty{}},
				},
				{
					Target:    scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME),
					Predicate: equalsString("my.library"),
				},
				{
					Target:    recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID),
					Predicate: equalsBytes([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}),
				},
			},
		},
	}
}

func verifyCompiledLogPolicy(t *testing.T, p googlepolicy.Policy, contexts []googlepolicy.LogContext) {
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

	eval, ok := p.(googlepolicy.LogPolicyEvaluator)
	if !ok {
		t.Fatalf("policy %T does not implement LogPolicyEvaluator", p)
	}
	pol, ok := p.(*Policy)
	if !ok {
		t.Fatalf("unexpected concrete policy type %T", p)
	}

	for _, ctx := range contexts {
		res := eval.EvaluateLog(ctx)
		switch res {
		case googlepolicy.EvalNoMatch:
		case googlepolicy.EvalKeep:
			if pol.Action() != policyv1alpha1.Action_ACTION_KEEP {
				t.Fatalf("EvaluateLog returned EvalKeep for action %v", pol.Action())
			}
		case googlepolicy.EvalDrop:
			if pol.Action() != policyv1alpha1.Action_ACTION_DROP {
				t.Fatalf("EvaluateLog returned EvalDrop for action %v", pol.Action())
			}
		default:
			t.Fatalf("EvaluateLog returned unknown EvalResult %v", res)
		}
	}
}

func FuzzLogFilterPolicy(f *testing.F) {
	f.Add([]byte{})
	for _, seed := range fuzzLogSeedPolicies() {
		if wireBytes, err := proto.Marshal(seed); err == nil {
			f.Add(wireBytes)
		}
		if jsonBytes, err := protojson.Marshal(seed); err == nil {
			f.Add(jsonBytes)
		}
	}

	contexts := fuzzLogContexts()

	f.Fuzz(func(t *testing.T, data []byte) {
		pb := &policyv1alpha1.LogFilterPolicy{}
		if err := proto.Unmarshal(data, pb); err == nil {
			ps, err := googlepolicy.MakePolicySetFromProtos("fuzz-rev", []proto.Message{pb})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					verifyCompiledLogPolicy(t, entry.PolicyObj, contexts)
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
					verifyCompiledLogPolicy(t, entry.PolicyObj, contexts)
				}
			}
		}
	})
}
