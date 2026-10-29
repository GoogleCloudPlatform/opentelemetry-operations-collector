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
	"strings"
	"testing"

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
		input    string
		expected string
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
			assert.Equal(t, tt.expected, SanitizeChannelName(tt.input))
		})
	}

	t.Run("capping at 1024 chars", func(t *testing.T) {
		t.Parallel()
		longName := strings.Repeat("a", 1100)
		san := SanitizeChannelName(longName)
		require.Len(t, san, 1024)
		h := sha256.Sum256([]byte(longName))
		expectedSuffix := "_" + hex.EncodeToString(h[:4])
		assert.True(t, strings.HasSuffix(san, expectedSuffix))
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

func TestPolicy_ReceiverID_Disambiguation(t *testing.T) {
	t.Parallel()

	p, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-disambiguate",
		Channels: []string{"Sysmon/Operational", "Sysmon_Operational"},
	})
	require.NoError(t, err)

	recIDs := p.LogReceiverIDs()
	require.Len(t, recIDs, 2)
	// Both channels are converted to valid component IDs
	assert.Equal(t, "sysmon_operational", recIDs[0].Name())
	assert.True(t, strings.HasPrefix(recIDs[1].Name(), "sysmon_operational_"))
	assert.NotEqual(t, recIDs[0].Name(), recIDs[1].Name())
}

func TestPolicy_ReceiverIDs(t *testing.T) {
	t.Parallel()

	p, err := NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-1",
		Channels: []string{"System", "Security", "Microsoft-Windows-Sysmon/Operational"},
	})
	require.NoError(t, err)

	expected := []component.ID{
		component.MustNewIDWithName(ReceiverType, "microsoft-windows-sysmon_operational"),
		component.MustNewIDWithName(ReceiverType, "security"),
		component.MustNewIDWithName(ReceiverType, "system"),
	}
	assert.Equal(t, expected, p.LogReceiverIDs())
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
	assert.NotContains(t, secCfg, "event_driven_scraping")

	// Verify compiler defaults for Sysmon channel
	sysmonCfg, ok := receivers["windowseventlog/microsoft-windows-sysmon_operational"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Microsoft-Windows-Sysmon/Operational", sysmonCfg["channel"])
	assert.Equal(t, true, sysmonCfg["suppress_rendering_info"])
	assert.Equal(t, 500, sysmonCfg["max_reads"])
	assert.Equal(t, true, sysmonCfg["ignore_channel_errors"])
	assert.NotContains(t, sysmonCfg, "event_driven_scraping")
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
		assert.Equal(t, expectedIDs, wp.LogReceiverIDs())
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
