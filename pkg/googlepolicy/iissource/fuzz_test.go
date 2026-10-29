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

package iissource

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func fuzzIISSeedPolicies() []*policyv1alpha1.IisSourcePolicy {
	return []*policyv1alpha1.IisSourcePolicy{
		{
			Id:                        "projects/123/locations/us-central1/policySets/ps-1/policies/enable-iis-default",
			CollectionIntervalSeconds: nil,
		},
		{
			Id:                        "iis-custom-interval-30",
			CollectionIntervalSeconds: proto.Int32(30),
		},
		{
			Id:                        "iis-min-boundary-10",
			CollectionIntervalSeconds: proto.Int32(10),
		},
		{
			Id:                        "iis-slow-scrape-120",
			CollectionIntervalSeconds: proto.Int32(120),
		},
	}
}

func verifyCompiledIISPolicy(t *testing.T, p googlepolicy.Policy) {
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
	if p.PolicyClass() != googlepolicy.PolicyClassSource {
		t.Fatalf("unexpected PolicyClass(): %q", p.PolicyClass())
	}

	ip, ok := p.(*Policy)
	if !ok {
		t.Fatalf("unexpected concrete policy type %T", p)
	}

	effInterval := ip.EffectiveCollectionIntervalSeconds()
	if effInterval < MinCollectionIntervalSeconds {
		t.Fatalf("EffectiveCollectionIntervalSeconds() = %d, want >= %d", effInterval, MinCollectionIntervalSeconds)
	}

	metricIDs := ip.MetricReceiverIDs()
	if len(metricIDs) != 1 || metricIDs[0] != component.MustNewID(DefaultReceiverID) {
		t.Fatalf("MetricReceiverIDs() = %v, want [%s]", metricIDs, DefaultReceiverID)
	}
	if ip.LogReceiverIDs() != nil {
		t.Fatalf("LogReceiverIDs() = %v, want nil", ip.LogReceiverIDs())
	}
	if ip.TraceReceiverIDs() != nil {
		t.Fatalf("TraceReceiverIDs() = %v, want nil", ip.TraceReceiverIDs())
	}

	conf, err := ip.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() returned error: %v", err)
	}
	if conf == nil {
		t.Fatal("Evaluate() returned nil confmap")
	}

	raw := conf.ToStringMap()
	receivers, ok := raw["receivers"].(map[string]any)
	if !ok {
		t.Fatalf("receivers not a map: %T", raw["receivers"])
	}
	iisCfg, ok := receivers[DefaultReceiverID].(map[string]any)
	if !ok {
		t.Fatalf("receivers.%s not a map: %T", DefaultReceiverID, receivers[DefaultReceiverID])
	}
	expectedIntervalStr := fmt.Sprintf("%ds", effInterval)
	if got := iisCfg["collection_interval"]; got != expectedIntervalStr {
		t.Fatalf("collection_interval = %v, want %q", got, expectedIntervalStr)
	}
}

func FuzzIISSourcePolicy(f *testing.F) {
	f.Add([]byte{})
	for _, seed := range fuzzIISSeedPolicies() {
		if _, err := NewPolicyFromProto(seed); err != nil {
			f.Fatalf("invalid seed policy %q: %v", seed.GetId(), err)
		}
		if wireBytes, err := proto.Marshal(seed); err == nil {
			f.Add(wireBytes)
		}
		if jsonBytes, err := protojson.Marshal(seed); err == nil {
			f.Add(jsonBytes)
		}
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		pb := &policyv1alpha1.IisSourcePolicy{}
		if err := proto.Unmarshal(data, pb); err == nil {
			ps, err := googlepolicy.MakePolicySetFromProtos("projects/p/locations/l/policySets/ps", "fuzz-rev", []proto.Message{pb})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					verifyCompiledIISPolicy(t, entry.PolicyObj)

					// ProtoJSON round-trip differential check:
					// Authored JSON and wire proto must compile and evaluate identically.
					if rtBytes, err := protojson.Marshal(pb); err == nil {
						var rtMap map[string]any
						if err := json.Unmarshal(rtBytes, &rtMap); err == nil && rtMap != nil {
							rtMap["type"] = PolicyType
							rtPS, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{rtMap})
							if err != nil {
								t.Fatalf("MakePolicySet failed for protojson roundtrip: %v", err)
							}
							rtEntry, ok := rtPS.Policies[entry.PolicyObj.PolicyName()]
							if !ok {
								t.Fatalf("policy %q missing in round-trip policy set", entry.PolicyObj.PolicyName())
							}
							verifyCompiledIISPolicy(t, rtEntry.PolicyObj)

							protoPol := entry.PolicyObj.(*Policy)
							jsonPol := rtEntry.PolicyObj.(*Policy)
							if protoPol.EffectiveCollectionIntervalSeconds() != jsonPol.EffectiveCollectionIntervalSeconds() {
								t.Fatalf("EffectiveCollectionIntervalSeconds mismatch: proto=%d, json=%d",
									protoPol.EffectiveCollectionIntervalSeconds(), jsonPol.EffectiveCollectionIntervalSeconds())
							}
						}
					}
				}
			}
		}

		// Raw JSON dictionary unmarshaling fuzzing
		var rawMap map[string]any
		if err := json.Unmarshal(data, &rawMap); err == nil && rawMap != nil {
			rawMap["type"] = PolicyType
			ps, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{rawMap})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					verifyCompiledIISPolicy(t, entry.PolicyObj)
				}
			}
		}
	})
}

func FuzzIISSourcePolicy_Consolidation(f *testing.F) {
	f.Add("iis-1", int32(60), true, "iis-2", int32(30), true)
	f.Add("iis-a", int32(10), true, "iis-b", int32(10), true)
	f.Add("iis-unset", int32(0), false, "iis-custom", int32(15), true)
	f.Add("iis-unset-1", int32(0), false, "iis-unset-2", int32(0), false)
	f.Add("iis-slow", int32(120), true, "iis-unset", int32(0), false)

	f.Fuzz(func(t *testing.T, id1 string, interval1 int32, hasInterval1 bool, id2 string, interval2 int32, hasInterval2 bool) {
		pb1 := &policyv1alpha1.IisSourcePolicy{Id: id1}
		if hasInterval1 {
			pb1.CollectionIntervalSeconds = proto.Int32(interval1)
		}
		p1, err1 := NewPolicyFromProto(pb1)

		pb2 := &policyv1alpha1.IisSourcePolicy{Id: id2}
		if hasInterval2 {
			pb2.CollectionIntervalSeconds = proto.Int32(interval2)
		}
		p2, err2 := NewPolicyFromProto(pb2)

		if err1 != nil || err2 != nil {
			// One or both policies are invalid, skip consolidation differential check
			return
		}

		// Verify order-independence: P1.Consolidate([P1, P2]) == P2.Consolidate([P2, P1])
		c1, errCons1 := p1.Consolidate([]googlepolicy.SourcePolicy{p1, p2})
		if errCons1 != nil {
			t.Fatalf("P1.Consolidate failed on valid policies: %v", errCons1)
		}
		c2, errCons2 := p2.Consolidate([]googlepolicy.SourcePolicy{p2, p1})
		if errCons2 != nil {
			t.Fatalf("P2.Consolidate failed on valid policies: %v", errCons2)
		}

		ip1 := c1.(*Policy)
		ip2 := c2.(*Policy)

		// Both must have resolved to the identical minimum interval
		if ip1.EffectiveCollectionIntervalSeconds() != ip2.EffectiveCollectionIntervalSeconds() {
			t.Fatalf("Consolidation order mismatch: c1=%d, c2=%d",
				ip1.EffectiveCollectionIntervalSeconds(), ip2.EffectiveCollectionIntervalSeconds())
		}

		wantMin := p1.EffectiveCollectionIntervalSeconds()
		if p2.EffectiveCollectionIntervalSeconds() < wantMin {
			wantMin = p2.EffectiveCollectionIntervalSeconds()
		}
		if ip1.EffectiveCollectionIntervalSeconds() != wantMin {
			t.Fatalf("Consolidation min mismatch: got %d, want %d", ip1.EffectiveCollectionIntervalSeconds(), wantMin)
		}

		verifyCompiledIISPolicy(t, c1)
		verifyCompiledIISPolicy(t, c2)
	})
}
