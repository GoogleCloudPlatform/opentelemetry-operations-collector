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

package windowseventlogsource

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func fuzzSeedPolicies() []*policyv1alpha1.WindowsEventLogSourcePolicy {
	return []*policyv1alpha1.WindowsEventLogSourcePolicy{
		{
			Id:       "win-source-standard",
			Channels: []string{"Security", "System", "Application"},
		},
		{
			Id:       "win-source-sysmon",
			Channels: []string{"Microsoft-Windows-Sysmon/Operational"},
		},
		{
			Id:       "win-source-delimiters",
			Channels: []string{`Microsoft-Windows-DNS-Client\Operational`, "Active Directory Web Services", "Channel:Name/With Spaces"},
		},
		{
			Id:       "win-source-casing-and-whitespace",
			Channels: []string{"  Security  ", "security", "SYSTEM", "System", "Channel\nWith\nNewlines"},
		},
	}
}

func verifyCompiledPolicy(t *testing.T, pol googlepolicy.Policy) {
	t.Helper()
	if pol == nil {
		t.Fatal("compiled policy is nil")
	}
	if pol.PolicyName() == "" {
		t.Fatal("compiled policy has empty PolicyName")
	}
	if pol.PolicyType() != PolicyType {
		t.Fatalf("expected PolicyType %q, got %q", PolicyType, pol.PolicyType())
	}
	if pol.PolicyClass() != googlepolicy.PolicyClassSource {
		t.Fatalf("expected PolicyClassSource, got %v", pol.PolicyClass())
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("compiled policy failed Validate(): %v", err)
	}

	srcPol, ok := pol.(googlepolicy.SourcePolicy)
	if !ok {
		t.Fatalf("compiled policy does not implement SourcePolicy: %T", pol)
	}

	recIDs := srcPol.LogReceiverIDs()
	if len(recIDs) == 0 {
		t.Fatal("expected at least 1 log receiver ID")
	}
	for _, id := range recIDs {
		if id.Type().String() != ReceiverType {
			t.Fatalf("expected receiver type %q, got %q", ReceiverType, id.Type().String())
		}
		if id.Name() == "" {
			t.Fatal("receiver ID name cannot be empty")
		}
		if strings.ContainsAny(id.Name(), "/\\ : \t\r\n") {
			t.Fatalf("receiver ID name %q contains unsanitized delimiter or whitespace", id.Name())
		}
	}

	if srcPol.MetricReceiverIDs() != nil {
		t.Fatal("MetricReceiverIDs must be nil")
	}
	if srcPol.TraceReceiverIDs() != nil {
		t.Fatal("TraceReceiverIDs must be nil")
	}

	conf, err := srcPol.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() returned error: %v", err)
	}
	if conf == nil {
		t.Fatal("Evaluate() returned nil conf")
	}
	m := conf.ToStringMap()
	receivers, ok := m["receivers"].(map[string]any)
	if !ok || len(receivers) != len(recIDs) {
		t.Fatalf("expected %d receivers in confmap, got %v", len(recIDs), m["receivers"])
	}
}

func FuzzSanitizeChannelName(f *testing.F) {
	seeds := []string{
		"Security",
		"System",
		"Application",
		"Microsoft-Windows-Sysmon/Operational",
		`Microsoft-Windows-DNS-Client\Operational`,
		"Active Directory Web Services",
		"Channel:Name/With Spaces",
		"   Trimmed Channel   ",
		"Multiple///Slashes\\\\Together",
		"Channel\nWith\nNewlines",
		"Channel\tWith\tTabs",
		"Channel\r\nWith\r\nCRLF",
		"COM1",
		"../../../",
		".._.._..",
		"::::::",
		"//////",
		`\\\\\\`,
		"   ",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, channel string) {
		san := SanitizeChannelName(channel)

		// Invariant 1: Sanitized name must be lowercase
		if san != strings.ToLower(san) {
			t.Fatalf("SanitizeChannelName(%q) = %q is not lowercase", channel, san)
		}

		// Invariant 2: No leading or trailing underscores
		if strings.HasPrefix(san, "_") || strings.HasSuffix(san, "_") {
			t.Fatalf("SanitizeChannelName(%q) = %q has leading/trailing underscores", channel, san)
		}

		// Invariant 3: No slashes, backslashes, colons, or whitespace characters
		if strings.ContainsAny(san, "/\\ : \t\r\n") {
			t.Fatalf("SanitizeChannelName(%q) = %q contains forbidden character", channel, san)
		}

		// Invariant 4: Idempotent
		if san != "" {
			if san2 := SanitizeChannelName(san); san2 != san {
				t.Fatalf("SanitizeChannelName is not idempotent: san(%q) = %q, san(%q) = %q", channel, san, san, san2)
			}
		}
	})
}

func FuzzPolicy_ProtoAndJSON(f *testing.F) {
	f.Add([]byte{})
	for _, seed := range fuzzSeedPolicies() {
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
		pb := &policyv1alpha1.WindowsEventLogSourcePolicy{}
		if err := proto.Unmarshal(data, pb); err == nil {
			ps, err := googlepolicy.MakePolicySetFromProtos("projects/p/locations/l/policySets/ps", "fuzz-rev", []proto.Message{pb})
			if err == nil {
				if len(ps.Policies) != 1 {
					t.Fatalf("expected 1 compiled policy, got %d", len(ps.Policies))
				}
				for _, entry := range ps.Policies {
					verifyCompiledPolicy(t, entry.PolicyObj)

					// ProtoJSON round-trip verification
					if rtBytes, err := protojson.Marshal(pb); err == nil {
						var rtMap map[string]any
						if err := json.Unmarshal(rtBytes, &rtMap); err == nil && rtMap != nil {
							rtMap["type"] = PolicyType
							rtPS, err := googlepolicy.MakePolicySet("fuzz-rev", []map[string]any{rtMap})
							rtEntry, ok := rtPS.Policies[entry.PolicyObj.PolicyName()]
							if err != nil || len(rtPS.Policies) != 1 || !ok {
								t.Fatalf("protojson round-trip failed MakePolicySet: err=%v, len=%d, ok=%v", err, len(rtPS.Policies), ok)
							}
							verifyCompiledPolicy(t, rtEntry.PolicyObj)

							// Both proto and JSON representations must produce identical receiver IDs
							pRecIDs := entry.PolicyObj.(googlepolicy.SourcePolicy).LogReceiverIDs()
							jRecIDs := rtEntry.PolicyObj.(googlepolicy.SourcePolicy).LogReceiverIDs()
							if len(pRecIDs) != len(jRecIDs) {
								t.Fatalf("receiver ID count mismatch between proto (%d) and JSON (%d)", len(pRecIDs), len(jRecIDs))
							}
							for i := range pRecIDs {
								if pRecIDs[i] != jRecIDs[i] {
									t.Fatalf("receiver ID[%d] mismatch: proto=%v json=%v", i, pRecIDs[i], jRecIDs[i])
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
					verifyCompiledPolicy(t, entry.PolicyObj)
				}
			}
		}
	})
}

func FuzzPolicy_Consolidate(f *testing.F) {
	seeds := [][]string{
		{"Security", "System"},
		{"Application", "Sysmon"},
		{"security", "system", "SYSTEM"},
		{`Microsoft-Windows-DNS-Client\Operational`, "Active Directory Web Services"},
	}
	for _, s := range seeds {
		b, _ := json.Marshal(s)
		f.Add(b)
	}

	f.Fuzz(func(t *testing.T, channelData []byte) {
		var channels []string
		if err := json.Unmarshal(channelData, &channels); err != nil {
			return
		}

		p1, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
			Id:       "p1",
			Channels: []string{"Security", "System"},
		})
		if err != nil {
			return
		}

		p2, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
			Id:       "p2",
			Channels: channels,
		})
		if err != nil {
			// If channels was invalid, NewPolicyFromProto correctly returns an error
			return
		}

		merged, err := p1.Consolidate([]googlepolicy.SourcePolicy{p2})
		if err != nil {
			t.Fatalf("unexpected error consolidating valid policies: %v", err)
		}

		verifyCompiledPolicy(t, merged)

		// Consolidated receiver IDs must always be deterministically sorted
		recIDs := merged.LogReceiverIDs()
		for i := 1; i < len(recIDs); i++ {
			if strings.Compare(recIDs[i-1].Name(), recIDs[i].Name()) >= 0 {
				t.Fatalf("receiver IDs not strictly sorted: %v before %v", recIDs[i-1], recIDs[i])
			}
		}
	})
}
