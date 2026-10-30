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

package hostmetricssource

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func fuzzHostMetricsSeedPolicies() []*policyv1alpha1.HostMetricsSourcePolicy {
	int10 := int32(10)
	int30 := int32(30)
	int60 := int32(60)
	int300 := int32(300)

	return []*policyv1alpha1.HostMetricsSourcePolicy{
		{
			Id: "seed-default-interval",
		},
		{
			Id:                        "seed-10s-min-interval",
			CollectionIntervalSeconds: &int10,
		},
		{
			Id:                        "seed-30s-interval",
			CollectionIntervalSeconds: &int30,
		},
		{
			Id:                        "seed-60s-interval",
			CollectionIntervalSeconds: &int60,
		},
		{
			Id:                        "seed-300s-interval",
			CollectionIntervalSeconds: &int300,
		},
	}
}

func verifyCompiledHostMetricsPolicy(t *testing.T, pol googlepolicy.Policy) {
	t.Helper()

	if pol == nil {
		t.Fatal("compiled policy is nil")
	}
	if pol.PolicyName() == "" {
		t.Fatal("compiled policy has empty PolicyName")
	}
	if pol.PolicyType() != PolicyType {
		t.Fatalf("PolicyType mismatch: got %q, want %q", pol.PolicyType(), PolicyType)
	}
	if pol.PolicyClass() != googlepolicy.PolicyClassSource {
		t.Fatalf("PolicyClass mismatch: got %v, want %v", pol.PolicyClass(), googlepolicy.PolicyClassSource)
	}

	hp, ok := pol.(*Policy)
	if !ok {
		t.Fatalf("expected *Policy, got %T", pol)
	}

	effectiveInterval := hp.EffectiveCollectionIntervalSeconds()
	if effectiveInterval < MinCollectionIntervalSeconds {
		t.Fatalf("effective interval %d < min %d", effectiveInterval, MinCollectionIntervalSeconds)
	}

	conf, err := hp.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() returned error on valid policy: %v", err)
	}
	if conf == nil {
		t.Fatal("Evaluate() returned nil confmap")
	}

	raw := conf.ToStringMap()
	receivers, ok := raw["receivers"].(map[string]any)
	if !ok {
		t.Fatal("receivers key missing or not a map")
	}
	hmCfg, ok := receivers[receiverID.String()].(map[string]any)
	if !ok {
		t.Fatalf("receiver %q missing from evaluated config", receiverID.String())
	}

	expectedIntervalStr := fmt.Sprintf("%ds", effectiveInterval)
	if hmCfg["collection_interval"] != expectedIntervalStr {
		t.Fatalf("collection_interval mismatch: got %v, want %v", hmCfg["collection_interval"], expectedIntervalStr)
	}

	scrapers, ok := hmCfg["scrapers"].(map[string]any)
	if !ok {
		t.Fatal("scrapers key missing or not a map")
	}
	for _, s := range defaultScrapers {
		if _, exists := scrapers[s]; !exists {
			t.Fatalf("expected scraper %q missing from evaluated config", s)
		}
	}

	if hp.LogReceiverIDs() != nil {
		t.Fatalf("LogReceiverIDs() must be nil, got %v", hp.LogReceiverIDs())
	}
	if hp.TraceReceiverIDs() != nil {
		t.Fatalf("TraceReceiverIDs() must be nil, got %v", hp.TraceReceiverIDs())
	}
	metricIDs := hp.MetricReceiverIDs()
	if len(metricIDs) != 1 || metricIDs[0] != receiverID {
		t.Fatalf("MetricReceiverIDs() mismatch: got %v, want [%s]", metricIDs, receiverID)
	}
}

func FuzzHostMetricsSourcePolicy(f *testing.F) {
	f.Add([]byte{})
	for _, seed := range fuzzHostMetricsSeedPolicies() {
		if _, err := NewPolicyFromProto(seed); err != nil {
			f.Fatalf("invalid seed policy %q: %v", seed.GetId(), err)
		}
		wireBytes, err := proto.Marshal(seed)
		if err != nil {
			f.Fatalf("proto.Marshal seed %q: %v", seed.GetId(), err)
		}
		f.Add(wireBytes)

		jsonBytes, err := protojson.Marshal(seed)
		if err != nil {
			f.Fatalf("protojson.Marshal seed %q: %v", seed.GetId(), err)
		}
		f.Add(jsonBytes)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// 1. Proto wire unmarshaling path
		pb := &policyv1alpha1.HostMetricsSourcePolicy{}
		if err := proto.Unmarshal(data, pb); err == nil {
			ps, err := googlepolicy.MakePolicySetFromProtos("projects/p/locations/l/policySets/ps", "fuzz-rev", []proto.Message{pb})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					verifyCompiledHostMetricsPolicy(t, entry.PolicyObj)

					// ProtoJSON round-trip consistency check
					if rtBytes, err := protojson.Marshal(pb); err == nil {
						var rtMap map[string]any
						if err := json.Unmarshal(rtBytes, &rtMap); err == nil && rtMap != nil {
							rtMap["type"] = PolicyType
							rtPS, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{rtMap})
							rtEntry, ok := rtPS.Policies[entry.PolicyObj.PolicyName()]
							if err != nil || len(rtPS.Policies) != 1 || !ok {
								t.Fatalf("protojson round-trip failed MakePolicySet: err=%v, len=%d, ok=%v", err, len(rtPS.Policies), ok)
							}
							verifyCompiledHostMetricsPolicy(t, rtEntry.PolicyObj)

							protoHp := entry.PolicyObj.(*Policy)
							jsonHp := rtEntry.PolicyObj.(*Policy)
							if protoHp.EffectiveCollectionIntervalSeconds() != jsonHp.EffectiveCollectionIntervalSeconds() {
								t.Fatalf("interval mismatch between proto (%d) and JSON (%d)",
									protoHp.EffectiveCollectionIntervalSeconds(), jsonHp.EffectiveCollectionIntervalSeconds())
							}
						}
					}
				}
			}
		}

		// 2. Raw JSON unmarshaling path
		var rawMap map[string]any
		if err := json.Unmarshal(data, &rawMap); err == nil && rawMap != nil {
			rawMap["type"] = PolicyType
			ps, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{rawMap})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					verifyCompiledHostMetricsPolicy(t, entry.PolicyObj)
				}
			}
		}
	})
}

func FuzzHostMetricsSourcePolicy_ConsolidationInvariants(f *testing.F) {
	f.Add("p1", int32(60), true, "p2", int32(30), true)
	f.Add("p1", int32(10), true, "p2", int32(60), true)
	f.Add("p1", int32(0), false, "p2", int32(15), true)
	f.Add("p1", int32(0), false, "p2", int32(0), false)
	f.Add("p1", int32(45), true, "p2", int32(45), true)

	f.Fuzz(func(t *testing.T, id1 string, interval1 int32, set1 bool, id2 string, interval2 int32, set2 bool) {
		pb1 := &policyv1alpha1.HostMetricsSourcePolicy{Id: id1}
		if set1 {
			pb1.CollectionIntervalSeconds = &interval1
		}
		p1, err1 := NewPolicyFromProto(pb1)

		pb2 := &policyv1alpha1.HostMetricsSourcePolicy{Id: id2}
		if set2 {
			pb2.CollectionIntervalSeconds = &interval2
		}
		p2, err2 := NewPolicyFromProto(pb2)

		// If either is invalid, consolidation of valid with invalid must fail
		if err1 != nil || err2 != nil {
			if err1 == nil && err2 != nil {
				_, err := p1.Consolidate([]googlepolicy.SourcePolicy{p1, &Policy{proto: pb2}})
				if err == nil {
					t.Fatalf("expected error consolidating invalid policy pb2")
				}
			}
			return
		}

		// Both policies are valid
		c12, err := p1.Consolidate([]googlepolicy.SourcePolicy{p1, p2})
		if err != nil {
			t.Fatalf("failed to consolidate p1 + p2: %v", err)
		}
		hp12 := c12.(*Policy)

		c21, err := p2.Consolidate([]googlepolicy.SourcePolicy{p2, p1})
		if err != nil {
			t.Fatalf("failed to consolidate p2 + p1: %v", err)
		}
		hp21 := c21.(*Policy)

		// Invariant 1: Commutativity (order independence)
		if hp12.EffectiveCollectionIntervalSeconds() != hp21.EffectiveCollectionIntervalSeconds() {
			t.Fatalf("consolidation not commutative: p1+p2=%d, p2+p1=%d",
				hp12.EffectiveCollectionIntervalSeconds(), hp21.EffectiveCollectionIntervalSeconds())
		}

		// Invariant 2: Result is minimum of effective intervals
		minExpected := p1.EffectiveCollectionIntervalSeconds()
		if p2.EffectiveCollectionIntervalSeconds() < minExpected {
			minExpected = p2.EffectiveCollectionIntervalSeconds()
		}
		if hp12.EffectiveCollectionIntervalSeconds() != minExpected {
			t.Fatalf("effective interval mismatch: got %d, want %d",
				hp12.EffectiveCollectionIntervalSeconds(), minExpected)
		}

		// Invariant 3: Consolidated policy evaluates successfully
		verifyCompiledHostMetricsPolicy(t, hp12)
		verifyCompiledHostMetricsPolicy(t, hp21)
	})
}
