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
	"strings"
	"testing"
	"time"

	v3adminpb "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)

func validRevisionResponse(t *testing.T, version, nonce, policyName string) *discoveryv3.DiscoveryResponse {
	t.Helper()
	return &discoveryv3.DiscoveryResponse{
		VersionInfo: version,
		Nonce:       nonce,
		TypeUrl:     xdsPolicyTypeURL,
		Resources:   []*anypb.Any{policyResource(t, policyName, version)},
	}
}

func TestXDSPolicyManager_Logging(t *testing.T) {
	registerDummyDriverForTest(t)
	resetActivePolicySet(t)

	core, recorded := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			if err := stream.Send(validRevisionResponse(t, "rev-1", "nonce-1", "log-filter")); err != nil {
				return err
			}
			_, _ = srv.recv(stream)
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	dialOpt := startFakeADSServer(t, srv)
	uri, err := url.Parse("xds://127.0.0.1:8080?gcp.fleet_id=fleet-1&project=my-project&insecure=true")
	require.NoError(t, err)

	mgr, err := NewXDSPolicyManager(logger, uri, "collector-abc")
	require.NoError(t, err)
	m := mgr.(*xdsPolicyManager)
	m.extraDialOpts = []grpc.DialOption{dialOpt}
	m.initialSyncTimeout = 2 * time.Second

	require.NoError(t, m.Start())
	require.NoError(t, m.Stop())

	logs := recorded.All()
	require.NotEmpty(t, logs)

	var sawPrefixLogger, sawManagerLog bool
	for _, entry := range logs {
		if strings.Contains(entry.Message, "[xds-client ") {
			sawPrefixLogger = true
		}
		if strings.Contains(entry.Message, "Starting xDS policy manager") {
			sawManagerLog = true
		}
	}
	assert.True(t, sawPrefixLogger, "expected xdsclient PrefixLogger messages ([xds-client ...]) to be routed to *zap.Logger")
	assert.True(t, sawManagerLog, "expected manager lifecycle logs to be emitted to *zap.Logger")

	dl := newZapDepthLogger(logger)
	assert.True(t, dl.V(2), "V(2) should be enabled when zap is at DebugLevel")
	assert.False(t, dl.V(9), "V(9) per-RPC logging should be disabled")

	infoCore, _ := observer.New(zapcore.InfoLevel)
	infoDL := newZapDepthLogger(zap.New(infoCore))
	assert.False(t, infoDL.V(2), "V(2) should be disabled when zap is at InfoLevel")
}

func TestXDSPolicyManager_CSDSDumpResources(t *testing.T) {
	registerDummyDriverForTest(t)
	resetActivePolicySet(t)

	ackCh := make(chan *discoveryv3.DiscoveryRequest, 1)
	nackCh := make(chan *discoveryv3.DiscoveryRequest, 1)

	barePayload, err := structpb.NewStruct(map[string]any{"name": "bare-policy"})
	require.NoError(t, err)
	bareAny, err := anypb.New(barePayload)
	require.NoError(t, err)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-1",
				Nonce:       "nonce-1",
				TypeUrl:     xdsPolicyTypeURL,
				Resources: []*anypb.Any{
					collectorResource(t, "rev-1",
						policyPayloadAny(t, "log-filter"),
						bareAny,
					),
				},
			}); err != nil {
				return err
			}
			ack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			ackCh <- ack

			// Wait until test triggers the invalid update.
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-bad",
				Nonce:       "nonce-2",
				TypeUrl:     xdsPolicyTypeURL,
				Resources: []*anypb.Any{{
					TypeUrl: "type.googleapis.com/does.not.Exist",
					Value:   []byte("invalid"),
				}},
			}); err != nil {
				return err
			}
			nack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			nackCh <- nack

			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	select {
	case <-ackCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ACK")
	}
	select {
	case <-nackCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for NACK")
	}

	// Wait briefly for authority CallbackSerializer to finish updating CSDS status for rev-bad.
	require.Eventually(t, func() bool {
		resp, err := m.DumpResources()
		if err != nil || len(resp.GetConfig()) == 0 {
			return false
		}
		generics := resp.GetConfig()[0].GetGenericXdsConfigs()
		return len(generics) == 1 && generics[0].GetClientStatus() == v3adminpb.ClientResourceStatus_NACKED
	}, 2*time.Second, 10*time.Millisecond, "expected CSDS DumpResources to report NACKED status")

	resp, err := m.DumpResources()
	require.NoError(t, err)
	require.Len(t, resp.GetConfig(), 1)

	generics := resp.GetConfig()[0].GetGenericXdsConfigs()
	require.Len(t, generics, 1)
	cfg := generics[0]
	assert.Equal(t, xdsPolicyTypeURL, cfg.GetTypeUrl())
	assert.Equal(t, testResourceName, cfg.GetName())
	assert.Equal(t, "rev-1", cfg.GetVersionInfo(), "accepted version rev-1 must be preserved in CSDS after NACK")
	assert.Equal(t, v3adminpb.ClientResourceStatus_NACKED, cfg.GetClientStatus())
	require.NotNil(t, cfg.GetErrorState())
	assert.Equal(t, "rev-bad", cfg.GetErrorState().GetVersionInfo())
	assert.Contains(t, cfg.GetErrorState().GetDetails(), "does.not.Exist")

	// Verify the cached XdsConfig payload unmarshals cleanly into xdsv1alpha1.TelemetryCollector
	// and preserves all resources (including bare policy protos).
	require.NotNil(t, cfg.GetXdsConfig())
	var dumpedCollector xdsv1alpha1.TelemetryCollector
	require.NoError(t, cfg.GetXdsConfig().UnmarshalTo(&dumpedCollector))
	assert.Len(t, dumpedCollector.GetPolicies(), 2)
}
