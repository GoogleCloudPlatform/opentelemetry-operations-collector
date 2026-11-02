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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
)

func TestPolicy_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		proto   *policyv1alpha1.WindowsEventLogSourcePolicy
		wantErr error
	}{
		{
			name:    "nil proto",
			proto:   nil,
			wantErr: ErrNilProto,
		},
		{
			name: "empty id",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "",
				Channels: []string{"Security"},
			},
			wantErr: ErrMissingID,
		},
		{
			name: "whitespace id",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "   ",
				Channels: []string{"Security"},
			},
			wantErr: ErrMissingID,
		},
		{
			name: "empty channels slice",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "win-1",
				Channels: []string{},
			},
			wantErr: ErrMissingChannels,
		},
		{
			name: "nil channels slice",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "win-1",
				Channels: nil,
			},
			wantErr: ErrMissingChannels,
		},
		{
			name: "channel with empty string",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "win-1",
				Channels: []string{"Security", ""},
			},
			wantErr: ErrEmptyChannelName,
		},
		{
			name: "channel with whitespace only",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "win-1",
				Channels: []string{"Security", "   "},
			},
			wantErr: ErrEmptyChannelName,
		},
		{
			name: "channel with punctuation only",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "win-1",
				Channels: []string{"Security", "///"},
			},
			wantErr: ErrInvalidChannelName,
		},
		{
			name: "channel with colons only",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "win-1",
				Channels: []string{"Security", ":::::"},
			},
			wantErr: ErrInvalidChannelName,
		},
		{
			name: "channel with symbols only",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "win-1",
				Channels: []string{"Security", "©"},
			},
			wantErr: ErrInvalidChannelName,
		},
		{
			name: "valid policy",
			proto: &policyv1alpha1.WindowsEventLogSourcePolicy{
				Id:       "win-1",
				Channels: []string{"Security", "System"},
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := NewPolicyFromProto(tt.proto)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, p)
			} else {
				require.NoError(t, err)
				require.NotNil(t, p)
				assert.Equal(t, tt.proto.Id, p.PolicyName())
				assert.Equal(t, PolicyType, p.PolicyType())
				assert.Equal(t, googlepolicy.PolicyClassSource, p.PolicyClass())
			}
		})
	}
}

func TestSanitizeChannelName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input          string
		expectedPrefix string
	}{
		{"Security", "security"},
		{"System", "system"},
		{"Application", "application"},
		{"Microsoft-Windows-Sysmon/Operational", "microsoft-windows-sysmon_operational"},
		{"Active Directory Web Services", "active_directory_web_services"},
		{`Microsoft-Windows-DNS-Client\Operational`, "microsoft-windows-dns-client_operational"},
		{"Channel:Name/With Spaces", "channel_name_with_spaces"},
		{"  Trimmed Channel  ", "trimmed_channel"},
		{"Multiple///Slashes\\\\Together", "multiple_slashes_together"},
		{"Channel\nWith\nNewlines", "channel_with_newlines"},
		{"Channel\tWith\tTabs", "channel_with_tabs"},
		{"Channel\r\nWith\r\nCRLF", "channel_with_crlf"},
		{"foo+bar|baz", "foo_bar_baz"},
		{"a=b$c", "a_b_c"},
		{"Sec\vurity", "sec_urity"},
		{"Sec\u00a0urity", "sec_urity"},
		{"Sec\u3000urity", "sec_urity"},
		{"Sec\u200burity", "sec_urity"},
		{"Security😀Log", "security_log"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			san := SanitizeChannelName(tt.input)
			assert.True(t, strings.HasPrefix(san, tt.expectedPrefix+"_"))
			h := sha256.Sum256([]byte(canonicalChannel(tt.input)))
			expectedSuffix := "_" + hex.EncodeToString(h[:8])
			assert.True(t, strings.HasSuffix(san, expectedSuffix))
			var id component.ID
			require.NoError(t, id.UnmarshalText([]byte(ReceiverType+"/"+san)))
		})
	}

	t.Run("casing invariance", func(t *testing.T) {
		t.Parallel()
		secLower := SanitizeChannelName("security")
		secUpper := SanitizeChannelName("SECURITY")
		secMixed := SanitizeChannelName("Security")
		assert.Equal(t, secLower, secUpper)
		assert.Equal(t, secLower, secMixed)

		longA := strings.Repeat("A", 1100)
		longa := strings.Repeat("a", 1100)
		assert.Equal(t, SanitizeChannelName(longA), SanitizeChannelName(longa))
	})

	t.Run("capping prefix at 64 bytes", func(t *testing.T) {
		t.Parallel()
		longName := strings.Repeat("a", 1100)
		san := SanitizeChannelName(longName)
		assert.Len(t, san, 64+1+16) // 64 prefix + 1 underscore + 16 hex hash = 81
		assert.True(t, strings.HasPrefix(san, strings.Repeat("a", 64)+"_"))
		h := sha256.Sum256([]byte(canonicalChannel(longName)))
		expectedSuffix := "_" + hex.EncodeToString(h[:8])
		assert.True(t, strings.HasSuffix(san, expectedSuffix))
	})

	t.Run("capping multi-byte utf8 boundary", func(t *testing.T) {
		t.Parallel()
		name := strings.Repeat("é", 100) // 200 bytes
		san := SanitizeChannelName(name)
		require.True(t, utf8.ValidString(san))
		var id component.ID
		require.NoError(t, id.UnmarshalText([]byte(ReceiverType+"/"+san)))
		parts := strings.Split(san, "_")
		require.Len(t, parts, 2)
		assert.LessOrEqual(t, len(parts[0]), 64)
		assert.Len(t, parts[1], 16)
	})
}

func TestPolicy_Channels_Deduplication(t *testing.T) {
	t.Parallel()

	p, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-dedup",
		Channels: []string{"Security", "security", "SYSTEM", "System", "Application", "Sysmon/Operational", "Sysmon_Operational"},
	})
	require.NoError(t, err)

	// Preserves first-seen casing ("Security", "SYSTEM", "Application", "Sysmon/Operational", "Sysmon_Operational")
	// and preserves distinct channels even if they sanitize to the same base name (Sysmon/Operational vs Sysmon_Operational).
	expected := []string{"Security", "SYSTEM", "Application", "Sysmon/Operational", "Sysmon_Operational"}
	assert.Equal(t, expected, p.Channels())
}

func TestPolicy_ReceiverID_DisambiguationAndDeterminism(t *testing.T) {
	t.Parallel()

	// 1. Two distinct channels receive distinct, deterministic IDs
	p, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-disambiguate",
		Channels: []string{"Sysmon/Operational", "Sysmon_Operational"},
	})
	require.NoError(t, err)

	recs := p.channelReceivers()
	require.Len(t, recs, 2)
	assert.Equal(t, SanitizeChannelName("Sysmon/Operational"), recs[0].id.Name())
	assert.Equal(t, SanitizeChannelName("Sysmon_Operational"), recs[1].id.Name())
	assert.NotEqual(t, recs[0].id.Name(), recs[1].id.Name())

	// 2. A channel's ID is identical regardless of input order, other channels, first-seen casing, or policy names
	secID := SanitizeChannelName("Security")
	assert.Equal(t, secID, SanitizeChannelName("security"))
	assert.Equal(t, secID, SanitizeChannelName("SECURITY"))

	p1, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "policy-alpha",
		Channels: []string{"Security"},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "policy-beta",
		Channels: []string{"System", "security", "Sysmon/Operational"},
	})
	require.NoError(t, err)

	// In p1 and p2, Security has identical ID
	assert.Equal(t, secID, p1.channelReceivers()[0].id.Name())
	for _, r := range p2.channelReceivers() {
		if strings.EqualFold(r.channel, "Security") {
			assert.Equal(t, secID, r.id.Name())
		}
	}

	// In consolidated policy, Security has identical ID
	cons, err := p1.Consolidate([]googlepolicy.SourcePolicy{p2})
	require.NoError(t, err)
	wpCons := cons.(*Policy)
	for _, r := range wpCons.channelReceivers() {
		if strings.EqualFold(r.channel, "Security") {
			assert.Equal(t, secID, r.id.Name())
		}
	}

	if isWindows {
		recIDs := p.LogReceiverIDs()
		require.Len(t, recIDs, 2)
	} else {
		assert.Nil(t, p.LogReceiverIDs())
	}
}

func TestPolicy_ReceiverID_CollidingLongUTF8(t *testing.T) {
	t.Parallel()

	ch1 := "a" + strings.Repeat("é", 255) + "/" + strings.Repeat("é", 255)
	ch2 := "a" + strings.Repeat("é", 255) + " " + strings.Repeat("é", 255)
	p, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "collision-long-utf8",
		Channels: []string{ch1, ch2},
	})
	require.NoError(t, err)
	recs := p.channelReceivers()
	require.Len(t, recs, 2)
	require.True(t, utf8.ValidString(recs[0].id.Name()))
	require.True(t, utf8.ValidString(recs[1].id.Name()))
	assert.NotEqual(t, recs[0].id.Name(), recs[1].id.Name())
	var id component.ID
	require.NoError(t, id.UnmarshalText([]byte(ReceiverType+"/"+recs[1].id.Name())))
}

func TestPolicy_ReceiverIDs(t *testing.T) {
	t.Parallel()

	p, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-1",
		Channels: []string{"System", "Security", "Microsoft-Windows-Sysmon/Operational"},
	})
	require.NoError(t, err)

	if isWindows {
		expected := []component.ID{
			component.MustNewIDWithName(ReceiverType, SanitizeChannelName("Microsoft-Windows-Sysmon/Operational")),
			component.MustNewIDWithName(ReceiverType, SanitizeChannelName("Security")),
			component.MustNewIDWithName(ReceiverType, SanitizeChannelName("System")),
		}
		// Deterministic sort by component name
		slices.SortFunc(expected, func(a, b component.ID) int {
			return strings.Compare(a.Name(), b.Name())
		})
		assert.Equal(t, expected, p.LogReceiverIDs())
	} else {
		assert.Nil(t, p.LogReceiverIDs())
	}
	assert.Nil(t, p.MetricReceiverIDs())
	assert.Nil(t, p.TraceReceiverIDs())
}

func TestPolicy_DefensiveProtoClone(t *testing.T) {
	t.Parallel()

	original := &policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-clone-test",
		Channels: []string{"Security", "System"},
	}

	p, err := NewPolicyFromProto(original)
	require.NoError(t, err)

	// Mutate caller's proto after construction
	original.Id = "mutated-id"
	original.Channels[0] = "MutatedChannel"

	// Assert policy remains completely isolated from external caller mutations
	assert.Equal(t, "win-clone-test", p.PolicyName())
	assert.Equal(t, []string{"Security", "System"}, p.Channels())
}

func TestPolicy_ReceiverConfigs(t *testing.T) {
	t.Parallel()

	p, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-cfg",
		Channels: []string{"Security", "Microsoft-Windows-Sysmon/Operational"},
	})
	require.NoError(t, err)

	raw := p.receiverConfigs()
	receivers, ok := raw["receivers"].(map[string]any)
	require.True(t, ok)
	require.Len(t, receivers, 2)

	secKey := fmt.Sprintf("%s/%s", ReceiverType, SanitizeChannelName("Security"))
	secCfg, ok := receivers[secKey].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Security", secCfg["channel"])
	assert.Equal(t, true, secCfg["suppress_rendering_info"])
	assert.Equal(t, 500, secCfg["max_reads"])
	assert.Equal(t, true, secCfg["ignore_channel_errors"])
	assert.Equal(t, []string{DefaultExcludeProvider}, secCfg["exclude_providers"])
	assert.NotContains(t, secCfg, "event_driven_scraping")

	sysmonKey := fmt.Sprintf("%s/%s", ReceiverType, SanitizeChannelName("Microsoft-Windows-Sysmon/Operational"))
	sysmonCfg, ok := receivers[sysmonKey].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Microsoft-Windows-Sysmon/Operational", sysmonCfg["channel"])
	assert.Equal(t, true, sysmonCfg["suppress_rendering_info"])
	assert.Equal(t, 500, sysmonCfg["max_reads"])
	assert.Equal(t, true, sysmonCfg["ignore_channel_errors"])
	assert.Equal(t, []string{DefaultExcludeProvider}, sysmonCfg["exclude_providers"])
	assert.NotContains(t, sysmonCfg, "event_driven_scraping")
}

func TestPolicy_Evaluate(t *testing.T) {
	t.Parallel()

	p, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-eval",
		Channels: []string{"Security", "Microsoft-Windows-Sysmon/Operational"},
	})
	require.NoError(t, err)

	conf, err := p.Evaluate(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conf)

	if !isWindows {
		assert.Empty(t, conf.ToStringMap())
	} else {
		raw := conf.ToStringMap()
		receivers, ok := raw["receivers"].(map[string]any)
		require.True(t, ok)
		require.Len(t, receivers, 2)

		// Verify compiler defaults for Security channel
		secCfg, ok := receivers["windowseventlog/security"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Security", secCfg["channel"])
		assert.Equal(t, true, secCfg["suppress_rendering_info"])
		assert.Equal(t, 500, secCfg["max_reads"])
		assert.Equal(t, true, secCfg["ignore_channel_errors"])
		assert.Equal(t, []string{DefaultExcludeProvider}, secCfg["exclude_providers"])
		assert.NotContains(t, secCfg, "event_driven_scraping")

		// Verify compiler defaults for Sysmon channel
		sysmonCfg, ok := receivers["windowseventlog/microsoft-windows-sysmon_operational"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Microsoft-Windows-Sysmon/Operational", sysmonCfg["channel"])
		assert.Equal(t, true, sysmonCfg["suppress_rendering_info"])
		assert.Equal(t, 500, sysmonCfg["max_reads"])
		assert.Equal(t, true, sysmonCfg["ignore_channel_errors"])
		assert.Equal(t, []string{DefaultExcludeProvider}, sysmonCfg["exclude_providers"])
		assert.NotContains(t, sysmonCfg, "event_driven_scraping")
	}
}

func TestPolicy_Evaluate_InvalidPolicy(t *testing.T) {
	t.Parallel()

	bad := &Policy{proto: &policyv1alpha1.WindowsEventLogSourcePolicy{Id: ""}}
	conf, err := bad.Evaluate(context.Background())
	require.ErrorIs(t, err, ErrMissingID)
	assert.Nil(t, conf)
}

func TestDriver_LoadPolicy(t *testing.T) {
	t.Parallel()

	t.Run("load from valid map", func(t *testing.T) {
		t.Parallel()
		pol, err := googlepolicy.LoadPolicy(PolicyType, map[string]any{
			"type":     PolicyType,
			"id":       "host-windows-logs",
			"channels": []any{"Security", "System"},
		})
		require.NoError(t, err)
		require.NotNil(t, pol)
		assert.Equal(t, "host-windows-logs", pol.PolicyName())
		assert.Equal(t, PolicyType, pol.PolicyType())
		assert.Equal(t, googlepolicy.PolicyClassSource, pol.PolicyClass())

		wp, ok := pol.(*Policy)
		require.True(t, ok)
		assert.Equal(t, []string{"Security", "System"}, wp.Channels())
	})

	t.Run("load from missing id", func(t *testing.T) {
		t.Parallel()
		_, err := googlepolicy.LoadPolicy(PolicyType, map[string]any{
			"type":     PolicyType,
			"channels": []any{"Security"},
		})
		require.Error(t, err)
	})

	t.Run("load from nil map", func(t *testing.T) {
		t.Parallel()
		_, err := googlepolicy.LoadPolicy(PolicyType, nil)
		require.Error(t, err)
	})
}

func TestPolicy_Consolidate(t *testing.T) {
	t.Parallel()

	p1, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-1",
		Channels: []string{"Security", "System"},
	})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-2",
		Channels: []string{"security", "Application"},
	})
	require.NoError(t, err)

	p3, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-3",
		Channels: []string{"SYSTEM", "Microsoft-Windows-Sysmon/Operational"},
	})
	require.NoError(t, err)

	t.Run("empty slice returns receiver policy", func(t *testing.T) {
		t.Parallel()
		res, err := p1.Consolidate(nil)
		require.NoError(t, err)
		assert.Same(t, p1, res)

		res, err = p1.Consolidate([]googlepolicy.SourcePolicy{})
		require.NoError(t, err)
		assert.Same(t, p1, res)
	})

	t.Run("single policy returns consolidated copy", func(t *testing.T) {
		t.Parallel()
		res, err := p1.Consolidate([]googlepolicy.SourcePolicy{p1})
		require.NoError(t, err)
		wp, ok := res.(*Policy)
		require.True(t, ok)
		assert.Equal(t, []string{"Security", "System"}, wp.Channels())
	})

	t.Run("multiple valid policies consolidate case-insensitively with first-seen casing", func(t *testing.T) {
		t.Parallel()
		res, err := p1.Consolidate([]googlepolicy.SourcePolicy{p2, p3})
		require.NoError(t, err)
		require.NotNil(t, res)

		wp, ok := res.(*Policy)
		require.True(t, ok)
		// Expected channels sorted deterministically by lowercase key:
		// "application" -> "Application" (from p2)
		// "microsoft-windows-sysmon/operational" -> "Microsoft-Windows-Sysmon/Operational" (from p3)
		// "security" -> "Security" (from p1, first-seen over p2's "security")
		// "system" -> "System" (from p1, first-seen over p3's "SYSTEM")
		expected := []string{
			"Application",
			"Microsoft-Windows-Sysmon/Operational",
			"Security",
			"System",
		}
		assert.Equal(t, expected, wp.Channels())

		// Verify synthesized receiver IDs
		expectedIDs := []component.ID{
			component.MustNewIDWithName(ReceiverType, "application"),
			component.MustNewIDWithName(ReceiverType, "microsoft-windows-sysmon_operational"),
			component.MustNewIDWithName(ReceiverType, "security"),
			component.MustNewIDWithName(ReceiverType, "system"),
		}
		if isWindows {
			assert.Equal(t, expectedIDs, wp.LogReceiverIDs())
		} else {
			assert.Nil(t, wp.LogReceiverIDs())
		}
	})

	t.Run("invalid receiver returns error", func(t *testing.T) {
		t.Parallel()
		badReceiver := &Policy{proto: &policyv1alpha1.WindowsEventLogSourcePolicy{Id: ""}}
		res, err := badReceiver.Consolidate(nil)
		require.ErrorIs(t, err, ErrMissingID)
		assert.Nil(t, res)

		nilReceiver := &Policy{}
		res, err = nilReceiver.Consolidate([]googlepolicy.SourcePolicy{p1})
		require.ErrorIs(t, err, ErrNilProto)
		assert.Nil(t, res)
	})

	t.Run("invalid policy in slice returns error", func(t *testing.T) {
		t.Parallel()
		bad := &Policy{proto: &policyv1alpha1.WindowsEventLogSourcePolicy{Id: ""}}
		res, err := p1.Consolidate([]googlepolicy.SourcePolicy{bad})
		require.Error(t, err)
		assert.Nil(t, res)
	})

	t.Run("wrong type returns error", func(t *testing.T) {
		t.Parallel()
		mock := &mockOtherSourcePolicy{name: "other"}
		res, err := p1.Consolidate([]googlepolicy.SourcePolicy{mock})
		require.Error(t, err)
		assert.Nil(t, res)
	})
}

type mockOtherSourcePolicy struct {
	name string
}

func (m *mockOtherSourcePolicy) PolicyName() string { return m.name }
func (m *mockOtherSourcePolicy) PolicyType() string { return "mock_other" }
func (m *mockOtherSourcePolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}
func (m *mockOtherSourcePolicy) Validate() error { return nil }
func (m *mockOtherSourcePolicy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	return confmap.New(), nil
}
func (m *mockOtherSourcePolicy) LogReceiverIDs() []component.ID    { return nil }
func (m *mockOtherSourcePolicy) MetricReceiverIDs() []component.ID { return nil }
func (m *mockOtherSourcePolicy) TraceReceiverIDs() []component.ID  { return nil }
