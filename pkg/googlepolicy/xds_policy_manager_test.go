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
	"fmt"
	"net/url"
	"testing"

	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
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

	t.Run("environment wins over fleet query parameter", func(t *testing.T) {
		// The provider resolves the fleet environment-first, and the two must
		// agree: this value becomes the xDS node cluster selecting which
		// fleet's policies arrive, while the provider's becomes the
		// gcp.fleet_id attribute on the collector's own telemetry.
		t.Setenv("FLEET_ID", "fleet-from-env")
		mgr, err := newManager("xds://127.0.0.1:8080?gcp.fleet_id=fleet-from-uri")
		require.NoError(t, err)
		assert.Equal(t, "fleet-from-env", mgr.(*xdsPolicyManager).fleetID)
	})

	t.Run("invalid insecure flag", func(t *testing.T) {
		_, err := newManager("xds://127.0.0.1:8080?gcp.fleet_id=fleet-1&insecure=maybe")
		require.ErrorIs(t, err, ErrXDSInvalidInsecureFlag)
	})

	t.Run("valid URI", func(t *testing.T) {
		// Explicit, because the environment now outranks the URI and an
		// ambient FLEET_ID would otherwise decide this test's outcome.
		t.Setenv("FLEET_ID", "")

		uri := mustParse("xds://127.0.0.1:8080?gcp.fleet_id=fleet-1&insecure=true")
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

func TestExtractPolicyProtos_TelemetryCollector(t *testing.T) {
	policyPayload, err := structpb.NewStruct(map[string]any{
		"name": "test-filter",
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

	policies, err := extractPolicyProtos(resp)
	require.NoError(t, err)
	require.Len(t, policies, 1)

	// The policy comes back as the message it was packed as, not as a
	// flattened copy of it: nothing between the wire and the driver needs a
	// generic representation any more.
	got, ok := policies[0].(*structpb.Struct)
	require.True(t, ok)
	assert.True(t, proto.Equal(policyPayload, got))
}

func TestExtractPolicyProtos_Errors(t *testing.T) {
	validPayload, err := structpb.NewStruct(map[string]any{
		"name": "good-policy",
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
			name:      "bare resource of an unknown type",
			resources: []*anypb.Any{unknownTypeAny},
			wantErr:   ErrXDSPolicyDecode,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policies, err := extractPolicyProtos(&discoveryv3.DiscoveryResponse{
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
			assert.Len(t, policies, tc.wantCount)
		})
	}
}

// protoDriver claims a proto the way the real filter drivers do. Those drivers
// cannot be imported here, since they import this package, so the routing path
// is exercised against their protos with a stand-in driver.
type protoDriver struct {
	dummyPolicyDriver
	msg proto.Message

	// got records the message the driver was handed, so a test can assert what
	// actually reached it rather than only what came back out.
	got proto.Message
}

func (d *protoDriver) PolicyProto() proto.Message { return d.msg }

func (d *protoDriver) LoadPolicyProto(msg proto.Message) (Policy, error) {
	d.got = msg
	return &mockTransformationPolicy{name: protoStringField(msg, "id"), signals: []Signal{SignalLogs}}, nil
}

// protoStringField reads a top-level string field by name, or "" if the message
// has no such field. The stand-in driver has no generated accessors to call.
func protoStringField(msg proto.Message, name string) string {
	m := msg.ProtoReflect()
	fd := m.Descriptor().Fields().ByName(protoreflect.Name(name))
	if fd == nil || fd.Kind() != protoreflect.StringKind {
		return ""
	}
	return m.Get(fd).String()
}

// registerProtoDriverForTest registers a driver claiming msg, and unregisters
// it afterwards since the registry is process-global.
func registerProtoDriverForTest(t *testing.T, policyType string, msg proto.Message) *protoDriver {
	t.Helper()
	d := &protoDriver{msg: msg}
	require.NoError(t, RegisterPolicyDriver(policyType, d))
	t.Cleanup(func() {
		delete(policyRegistry, policyType)
		delete(policyProtoRegistry, msg.ProtoReflect().Descriptor().FullName())
	})
	return d
}

// TestMakePolicySetFromProtos_RoutesBareProtoToItsDriver covers a policy shaped
// the way a control plane actually sends one: a bare LogFilterPolicy whose body
// carries no "type" field, because the proto has no such field. The policy type
// has to come from the driver that claimed the proto.
//
// Deriving it from the type URL instead produced "LogFilterPolicy", which no
// driver is registered under, so every policy of this shape was rejected and
// took its whole revision down with it.
func TestMakePolicySetFromProtos_RoutesBareProtoToItsDriver(t *testing.T) {
	d := registerProtoDriverForTest(t, "log_filter", &policyv1alpha1.LogFilterPolicy{})

	policy := &policyv1alpha1.LogFilterPolicy{Id: "retain-errors"}

	ps, err := MakePolicySetFromProtos("v1", []proto.Message{policy})
	require.NoError(t, err)
	require.Len(t, ps.Policies, 1)
	assert.Contains(t, ps.Policies, "retain-errors")

	// The driver is handed the very message that was decoded off the wire.
	// Nothing re-serializes it on the way, which is the point of the path.
	assert.Same(t, policy, d.got)
}

// TestMakePolicySetFromProtos_UnclaimedProtoIsRejectedByName checks that a
// policy no driver claims is reported by proto name, which is the thing an
// operator needs in order to tell a typo apart from a collector built without
// that policy.
func TestMakePolicySetFromProtos_UnclaimedProtoIsRejectedByName(t *testing.T) {
	// Nothing registers MetricFilterPolicy in this test binary.
	ps, err := MakePolicySetFromProtos("v1", []proto.Message{&policyv1alpha1.MetricFilterPolicy{}})

	require.ErrorIs(t, err, ErrPolicyTypeNotFound)
	assert.Contains(t, err.Error(), "google.telemetry.policy.v1alpha1.MetricFilterPolicy")
	assert.Empty(t, ps.Policies)
}

// TestMakePolicySetFromProtos_KeepsUsablePoliciesFromAMixedRevision pins the
// best-effort contract on the proto path: one unroutable policy does not cost
// the revision the policies that were understood. The xDS manager depends on
// this to tell a partially bad revision from a completely bad one.
func TestMakePolicySetFromProtos_KeepsUsablePoliciesFromAMixedRevision(t *testing.T) {
	registerProtoDriverForTest(t, "log_filter", &policyv1alpha1.LogFilterPolicy{})

	ps, err := MakePolicySetFromProtos("v1", []proto.Message{
		&policyv1alpha1.TraceFilterPolicy{Id: "unclaimed"},
		&policyv1alpha1.LogFilterPolicy{Id: "usable"},
	})

	require.ErrorIs(t, err, ErrPolicyTypeNotFound)
	// The failure is reported by position, since a bare proto has no name of
	// its own to quote back.
	assert.Contains(t, err.Error(), "index 0")
	require.Len(t, ps.Policies, 1)
	assert.Contains(t, ps.Policies, "usable")
}

func TestTokenAuth_RequireTransportSecurity(t *testing.T) {
	// Bearer tokens must never be sent over a connection without transport
	// security, so gRPC has to refuse the combination on our behalf.
	assert.True(t, (&TokenAuth{}).RequireTransportSecurity())
}

// dummyPolicyDriver stands in for a policy component across the manager tests.
//
// It claims google.protobuf.Struct so that tests can put an arbitrary policy
// body on the wire without depending on a real policy schema, and still be
// routed the way a real policy is: by the identity of the message, not by a
// field inside it.
type dummyPolicyDriver struct{}

func (d *dummyPolicyDriver) LoadPolicy(raw map[string]any) (Policy, error) {
	name, _ := raw["name"].(string)
	return &mockTransformationPolicy{name: name, signals: []Signal{SignalLogs}}, nil
}

func (d *dummyPolicyDriver) PolicyProto() proto.Message { return &structpb.Struct{} }

func (d *dummyPolicyDriver) LoadPolicyProto(msg proto.Message) (Policy, error) {
	s, ok := msg.(*structpb.Struct)
	if !ok {
		return nil, fmt.Errorf("%w: expected *structpb.Struct, got %T", ErrPolicyProtoMismatch, msg)
	}
	return d.LoadPolicy(s.AsMap())
}

// registerDummyDriverForTest registers dummyPolicyDriver under
// "mock_transformation", tolerating the registration another test already made:
// the registry is process-global and has no removal API, and every caller wants
// the same driver in place.
func registerDummyDriverForTest(t *testing.T) {
	t.Helper()
	if _, ok := policyRegistry["mock_transformation"]; ok {
		return
	}
	require.NoError(t, RegisterPolicyDriver("mock_transformation", &dummyPolicyDriver{}))
}

func TestXDSPolicyLifecycle_SetActivePolicySet(t *testing.T) {
	registerDummyDriverForTest(t)

	policyPayload, err := structpb.NewStruct(map[string]any{
		"name": "log-filter-policy",
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

	policies, err := extractPolicyProtos(resp)
	require.NoError(t, err)

	// Step 1: MakePolicySetFromProtos
	ps, err := MakePolicySetFromProtos(resp.GetVersionInfo(), policies)
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
