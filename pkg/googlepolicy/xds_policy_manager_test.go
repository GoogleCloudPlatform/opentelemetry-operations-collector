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

package googlepolicy

import (
	"net/url"
	"testing"

	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)

func TestNewXDSPolicyManager_Validation(t *testing.T) {
	logger := zap.NewNop()

	mustParse := func(rawURI string) *url.URL {
		t.Helper()
		u, err := url.Parse(rawURI)
		require.NoError(t, err)
		return u
	}
	// newManager builds a manager with valid defaults for everything the test
	// is not exercising.
	newManager := func(rawURI string) (Manager, error) {
		return NewXDSPolicyManager(logger, mustParse(rawURI), "collector-abc")
	}

	t.Run("empty URI host and path", func(t *testing.T) {
		_, err := newManager("xds:")
		require.ErrorIs(t, err, ErrXDSMissingServerAddr)
	})

	t.Run("nil URI", func(t *testing.T) {
		_, err := NewXDSPolicyManager(logger, nil, "collector-abc")
		require.ErrorIs(t, err, ErrXDSMissingServerAddr)
	})

	t.Run("missing collector ID", func(t *testing.T) {
		_, err := NewXDSPolicyManager(logger, mustParse("xds://127.0.0.1:8080"), "")
		require.ErrorIs(t, err, ErrXDSMissingCollectorID)
	})

	t.Run("missing fleet ID", func(t *testing.T) {
		t.Setenv("FLEET_ID", "")
		_, err := NewXDSPolicyManager(logger, mustParse("xds://127.0.0.1:8080"), "collector-abc")
		require.ErrorIs(t, err, ErrXDSMissingFleetID)
	})

	t.Run("fleet ID from environment", func(t *testing.T) {
		t.Setenv("FLEET_ID", "fleet-from-env")
		mgr, err := NewXDSPolicyManager(logger, mustParse("xds://127.0.0.1:8080"), "collector-abc")
		require.NoError(t, err)
		assert.Equal(t, "fleet-from-env", mgr.(*xdsPolicyManager).fleetID)
	})

	t.Run("fleet query parameter wins over environment", func(t *testing.T) {
		t.Setenv("FLEET_ID", "fleet-from-env")
		mgr, err := newManager("xds://127.0.0.1:8080?fleet=fleet-from-uri")
		require.NoError(t, err)
		assert.Equal(t, "fleet-from-uri", mgr.(*xdsPolicyManager).fleetID)
	})

	t.Run("invalid insecure flag", func(t *testing.T) {
		_, err := newManager("xds://127.0.0.1:8080?fleet=fleet-1&insecure=maybe")
		require.ErrorIs(t, err, ErrXDSInvalidInsecureFlag)
	})

	t.Run("valid URI", func(t *testing.T) {
		uri := mustParse("xds://127.0.0.1:8080?fleet=fleet-1&insecure=true")
		mgr, err := NewXDSPolicyManager(logger, uri, "collector-abc")
		require.NoError(t, err)
		assert.Equal(t, uri, mgr.URI())

		m := mgr.(*xdsPolicyManager)
		assert.Equal(t, "127.0.0.1:8080", m.serverAddr)
		assert.Equal(t, "collector-abc", m.collectorID)
		assert.Equal(t, "fleet-1", m.fleetID)
		assert.True(t, m.insecure)

		// Asserted explicitly because every stream test overrides this field to
		// keep itself fast. If the constructor stopped setting it the zero value
		// would make Start's wait expire immediately -- silently disabling the
		// initial sync -- and no other test would notice.
		assert.Equal(t, defaultInitialSyncTimeout, m.initialSyncTimeout)
	})

}

func TestExtractRawPolicies_TelemetryCollector(t *testing.T) {
	policyPayload, err := structpb.NewStruct(map[string]any{
		"name": "test-filter",
		"type": "mock_transformation",
	})
	require.NoError(t, err)

	policyAny, err := anypb.New(policyPayload)
	require.NoError(t, err)

	collector := &xdsv1alpha1.TelemetryCollector{
		Policies: []*anypb.Any{policyAny},
	}

	collectorAny, err := anypb.New(collector)
	require.NoError(t, err)

	resp := &discoveryv3.DiscoveryResponse{
		VersionInfo: "v1",
		Resources:   []*anypb.Any{collectorAny},
	}

	rawPolicies, err := extractRawPolicies(resp)
	require.NoError(t, err)
	require.Len(t, rawPolicies, 1)
	// Both of these come from the policy body itself -- policies are bare
	// google.protobuf.Any values, so there is no enclosing wrapper to read.
	assert.Equal(t, "test-filter", rawPolicies[0]["name"])
	assert.Equal(t, "mock_transformation", rawPolicies[0]["type"])
}

func TestExtractRawPolicies_Errors(t *testing.T) {
	validPayload, err := structpb.NewStruct(map[string]any{
		"name": "good-policy",
		"type": "mock_transformation",
	})
	require.NoError(t, err)
	validPayloadAny, err := anypb.New(validPayload)
	require.NoError(t, err)

	unknownTypeAny := &anypb.Any{
		TypeUrl: "type.googleapis.com/does.not.Exist",
		Value:   []byte("garbage"),
	}

	tests := []struct {
		name        string
		resources   []*anypb.Any
		wantErr     error
		wantErrText string
		wantCount   int
	}{
		{
			name:      "no resources",
			resources: nil,
			wantCount: 0,
		},
		{
			name: "policy with empty Any",
			resources: func() []*anypb.Any {
				collectorAny, err := anypb.New(&xdsv1alpha1.TelemetryCollector{
					Policies: []*anypb.Any{{}},
				})
				require.NoError(t, err)
				return []*anypb.Any{collectorAny}
			}(),
			wantErr: ErrXDSPolicyMissingBody,
			// A bare Any carries no name, so the index is the only identifier.
			wantErrText: "index 0",
		},
		{
			name: "policy with unregistered type URL",
			resources: func() []*anypb.Any {
				collectorAny, err := anypb.New(&xdsv1alpha1.TelemetryCollector{
					Policies: []*anypb.Any{unknownTypeAny},
				})
				require.NoError(t, err)
				return []*anypb.Any{collectorAny}
			}(),
			wantErr:     ErrXDSPolicyDecode,
			wantErrText: "does.not.Exist",
		},
		{
			name: "valid and invalid policies in one collector",
			resources: func() []*anypb.Any {
				collectorAny, err := anypb.New(&xdsv1alpha1.TelemetryCollector{
					Policies: []*anypb.Any{validPayloadAny, unknownTypeAny},
				})
				require.NoError(t, err)
				return []*anypb.Any{collectorAny}
			}(),
			wantErr: ErrXDSPolicyDecode,
			// The decodable policy is still returned alongside the error.
			wantCount: 1,
		},
		{
			name:      "bare resource that is not a policy",
			resources: []*anypb.Any{unknownTypeAny},
			wantErr:   ErrXDSPolicyDecode,
		},
		{
			name: "bare resource with no type field",
			resources: func() []*anypb.Any {
				payload, err := structpb.NewStruct(map[string]any{"name": "typeless"})
				require.NoError(t, err)
				payloadAny, err := anypb.New(payload)
				require.NoError(t, err)
				return []*anypb.Any{payloadAny}
			}(),
			wantErr: ErrXDSResourceNotPolicy,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rawPolicies, err := extractRawPolicies(&discoveryv3.DiscoveryResponse{
				VersionInfo: "v1",
				Resources:   tc.resources,
			})

			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
				if tc.wantErrText != "" {
					assert.Contains(t, err.Error(), tc.wantErrText)
				}
			}
			assert.Len(t, rawPolicies, tc.wantCount)
		})
	}
}

// protoDriver claims a proto the way the real filter drivers do. Those drivers
// cannot be imported here, since they import this package, so the routing path
// is exercised against their protos with a stand-in driver.
type protoDriver struct {
	dummyPolicyDriver
	msg proto.Message
}

func (d *protoDriver) PolicyProto() proto.Message { return d.msg }

// registerProtoDriverForTest registers a driver claiming msg, and unregisters
// it afterwards since the registry is process-global.
func registerProtoDriverForTest(t *testing.T, policyType string, msg proto.Message) {
	t.Helper()
	require.NoError(t, RegisterPolicyDriver(policyType, &protoDriver{msg: msg}))
	t.Cleanup(func() {
		delete(policyRegistry, policyType)
		delete(policyProtoRegistry, msg.ProtoReflect().Descriptor().FullName())
	})
}

// TestExtractRawPolicies_RoutesBareProtoToItsDriver covers a policy shaped the
// way a control plane actually sends one: a bare LogFilterPolicy Any whose body
// carries no "type" field, because the proto has no such field. The policy type
// has to come from the driver that claimed the proto.
//
// Deriving it from the type URL instead produced "LogFilterPolicy", which no
// driver is registered under, so every policy of this shape was rejected and
// took its whole revision down with it. The existing tests missed this because
// they all send either a mock policy type or a structpb.Struct carrying an
// explicit "type" key, neither of which reaches this branch.
func TestExtractRawPolicies_RoutesBareProtoToItsDriver(t *testing.T) {
	const policyType = "log_filter"
	registerProtoDriverForTest(t, policyType, &policyv1alpha1.LogFilterPolicy{})

	policyAny, err := anypb.New(&policyv1alpha1.LogFilterPolicy{Id: "retain-errors"})
	require.NoError(t, err)
	collectorAny, err := anypb.New(&xdsv1alpha1.TelemetryCollector{
		Policies: []*anypb.Any{policyAny},
	})
	require.NoError(t, err)

	rawPolicies, err := extractRawPolicies(&discoveryv3.DiscoveryResponse{
		VersionInfo: "v1",
		Resources:   []*anypb.Any{collectorAny},
	})
	require.NoError(t, err)
	require.Len(t, rawPolicies, 1)

	assert.Equal(t, policyType, rawPolicies[0]["type"])
	// The body survives the round trip alongside the backfilled type.
	assert.Equal(t, "retain-errors", rawPolicies[0]["id"])
	// Nothing synthesizes a name: the TypedExtensionConfig wrapper that used
	// to carry one is gone, and drivers read their own identifier from the body.
	assert.NotContains(t, rawPolicies[0], "name")
}

// TestExtractRawPolicies_UnclaimedProtoIsRejectedByName checks that a policy no
// driver claims is reported by proto name, which is the thing an operator needs
// in order to tell a typo apart from a collector built without that policy.
func TestExtractRawPolicies_UnclaimedProtoIsRejectedByName(t *testing.T) {
	// Nothing registers MetricFilterPolicy in this test binary.
	policyAny, err := anypb.New(&policyv1alpha1.MetricFilterPolicy{})
	require.NoError(t, err)
	collectorAny, err := anypb.New(&xdsv1alpha1.TelemetryCollector{
		Policies: []*anypb.Any{policyAny},
	})
	require.NoError(t, err)

	rawPolicies, err := extractRawPolicies(&discoveryv3.DiscoveryResponse{
		VersionInfo: "v1",
		Resources:   []*anypb.Any{collectorAny},
	})
	require.ErrorIs(t, err, ErrXDSResourceNotPolicy)
	assert.Contains(t, err.Error(), "google.telemetry.policy.v1alpha1.MetricFilterPolicy")
	assert.Empty(t, rawPolicies)
}

func TestTokenAuth_RequireTransportSecurity(t *testing.T) {
	// Bearer tokens must never be sent over a connection without transport
	// security, so gRPC has to refuse the combination on our behalf.
	assert.True(t, (&TokenAuth{}).RequireTransportSecurity())
}

type dummyPolicyDriver struct{}

func (d *dummyPolicyDriver) LoadPolicy(raw map[string]any) (Policy, error) {
	name, _ := raw["name"].(string)
	return &mockTransformationPolicy{name: name, signals: []Signal{SignalLogs}}, nil
}

func TestXDSPolicyLifecycle_SetActivePolicySet(t *testing.T) {
	// Register driver for test policy type
	_ = RegisterPolicyDriver("mock_transformation", &dummyPolicyDriver{})

	policyPayload, err := structpb.NewStruct(map[string]any{
		"name": "log-filter-policy",
		"type": "mock_transformation",
	})
	require.NoError(t, err)

	policyAny, err := anypb.New(policyPayload)
	require.NoError(t, err)

	collector := &xdsv1alpha1.TelemetryCollector{
		Policies: []*anypb.Any{policyAny},
	}

	collectorAny, err := anypb.New(collector)
	require.NoError(t, err)

	resp := &discoveryv3.DiscoveryResponse{
		VersionInfo: "rev-xds-100",
		Resources:   []*anypb.Any{collectorAny},
	}

	rawPolicies, err := extractRawPolicies(resp)
	require.NoError(t, err)

	// Step 1: MakePolicySet
	ps, err := MakePolicySet(resp.GetVersionInfo(), rawPolicies)
	require.NoError(t, err)
	require.NotNil(t, ps)

	// Step 2: SetActivePolicySet
	SetActivePolicySet(ps)
	t.Cleanup(func() {
		for ActivePolicySet() != nil {
			RollbackActivePolicySet()
		}
	})

	active := ActivePolicySet()
	require.NotNil(t, active)
	assert.Equal(t, "rev-xds-100", active.RevisionID)
	assert.Len(t, active.Policies, 1)
	assert.Contains(t, active.Policies, "log-filter-policy")
}
