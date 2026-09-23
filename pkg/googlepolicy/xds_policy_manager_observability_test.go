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
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	v3adminpb "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)

func validRevisionResponse(t *testing.T, version, nonce, policyName string) *discoveryv3.DiscoveryResponse {
	t.Helper()
	return &discoveryv3.DiscoveryResponse{
		VersionInfo: version,
		Nonce:       nonce,
		TypeUrl:     xdsPolicyTypeURL,
		Resources:   []*anypb.Any{policyResource(t, policyName)},
	}
}

func findInt64SumPoints(rm metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					return sum.DataPoints
				}
			}
		}
	}
	return nil
}

func findInt64GaugePoints(rm metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if g, ok := m.Data.(metricdata.Gauge[int64]); ok {
					return g.DataPoints
				}
			}
		}
	}
	return nil
}

func attrValue(attrs attribute.Set, key string) string {
	val, ok := attrs.Value(attribute.Key(key))
	if !ok {
		return ""
	}
	return val.AsString()
}

func TestXDSPolicyManager_Metrics(t *testing.T) {
	registerDummyDriverForTest(t)
	resetActivePolicySet(t)

	ackCh := make(chan *discoveryv3.DiscoveryRequest, 1)
	nackCh := make(chan *discoveryv3.DiscoveryRequest, 1)
	closeStreamCh := make(chan struct{})

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		// First stream: fails before receiving any message -> triggers ServerFailure metric.
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			_, _ = srv.recv(stream)
			return grpcstatus.Error(codes.Unavailable, "simulated control plane failure")
		},
		// Second stream: sends valid rev-1 (ACKed), then invalid rev-bad (NACKed), then waits for closeStreamCh.
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			if err := stream.Send(validRevisionResponse(t, "rev-1", "nonce-1", "log-filter")); err != nil {
				return err
			}
			ack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			ackCh <- ack

			// Send an invalid revision to trigger ResourceUpdateInvalid and nacked_but_cached state.
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

			<-closeStreamCh
			return grpcstatus.Error(codes.Unavailable, "stream closed")
		},
		// Subsequent reconnect attempts: block and fail so connected gauge reports 0.
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	dialOpt := startFakeADSServer(t, srv)
	uri, err := url.Parse("xds://127.0.0.1:8080?gcp.fleet_id=fleet-1&insecure=true")
	require.NoError(t, err)

	mgr, err := NewXDSPolicyManager(zap.NewNop(), uri, "collector-abc", WithMeterProvider(mp))
	require.NoError(t, err)
	m := mgr.(*xdsPolicyManager)
	m.extraDialOpts = []grpc.DialOption{dialOpt}
	m.backoffInitial = 10 * time.Millisecond
	m.backoffMax = 50 * time.Millisecond
	m.initialSyncTimeout = 2 * time.Second

	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	// 1. Wait for ACK of rev-1 and NACK of rev-bad.
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

	// Collect metrics while connected and holding a nacked_but_cached resource.
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	validPts := findInt64SumPoints(rm, "grpc.xds_client.resource_updates_valid")
	require.Len(t, validPts, 1, "expected 1 series for grpc.xds_client.resource_updates_valid")
	assert.Equal(t, int64(1), validPts[0].Value)
	assert.Equal(t, "127.0.0.1:8080", attrValue(validPts[0].Attributes, "grpc.target"))
	assert.Equal(t, "127.0.0.1:8080", attrValue(validPts[0].Attributes, "grpc.xds.server"))
	assert.Equal(t, "TelemetryCollector", attrValue(validPts[0].Attributes, "grpc.xds.resource_type"))

	invalidPts := findInt64SumPoints(rm, "grpc.xds_client.resource_updates_invalid")
	require.Len(t, invalidPts, 1, "expected 1 series for grpc.xds_client.resource_updates_invalid")
	assert.Equal(t, int64(1), invalidPts[0].Value)

	failurePts := findInt64SumPoints(rm, "grpc.xds_client.server_failure")
	require.Len(t, failurePts, 1, "expected 1 series for grpc.xds_client.server_failure from stream 1")
	assert.GreaterOrEqual(t, failurePts[0].Value, int64(1))

	connectedPts := findInt64GaugePoints(rm, "grpc.xds_client.connected")
	require.Len(t, connectedPts, 1, "expected 1 series for grpc.xds_client.connected")
	assert.Equal(t, int64(1), connectedPts[0].Value, "connected gauge must be 1 while stream 2 is active")

	resourcePts := findInt64GaugePoints(rm, "grpc.xds_client.resources")
	require.NotEmpty(t, resourcePts, "expected grpc.xds_client.resources gauge points")
	foundNackedButCached := false
	for _, pt := range resourcePts {
		if attrValue(pt.Attributes, "grpc.xds.cache_state") == "nacked_but_cached" && pt.Value == 1 {
			foundNackedButCached = true
		}
	}
	assert.True(t, foundNackedButCached, "expected grpc.xds_client.resources with cache_state=nacked_but_cached and count=1")

	// Now increase backoff so that when stream 2 breaks, stream stays disconnected long enough to observe connected=0.
	m.mu.Lock()
	m.backoffInitial = 5 * time.Second
	m.backoffMax = 5 * time.Second
	m.mu.Unlock()
	close(closeStreamCh)

	require.Eventually(t, func() bool {
		var disconnectedRM metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &disconnectedRM); err != nil {
			return false
		}
		pts := findInt64GaugePoints(disconnectedRM, "grpc.xds_client.connected")
		return len(pts) == 1 && pts[0].Value == 0
	}, 2*time.Second, 10*time.Millisecond, "grpc.xds_client.connected must transition to 0 when stream disconnects")
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
	uri, err := url.Parse("xds://127.0.0.1:8080?gcp.fleet_id=fleet-1&insecure=true")
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
}

func TestXDSPolicyManager_CSDSDumpResources(t *testing.T) {
	registerDummyDriverForTest(t)
	resetActivePolicySet(t)

	ackCh := make(chan *discoveryv3.DiscoveryRequest, 1)
	nackCh := make(chan *discoveryv3.DiscoveryRequest, 1)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			if err := stream.Send(validRevisionResponse(t, "rev-1", "nonce-1", "log-filter")); err != nil {
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
	assert.Equal(t, "rev-1", cfg.GetVersionInfo(), "accepted version rev-1 must be preserved in CSDS after NACK")
	assert.Equal(t, v3adminpb.ClientResourceStatus_NACKED, cfg.GetClientStatus())
	require.NotNil(t, cfg.GetErrorState())
	assert.Equal(t, "rev-bad", cfg.GetErrorState().GetVersionInfo())
	assert.Contains(t, cfg.GetErrorState().GetDetails(), "does.not.Exist")

	// Verify the cached XdsConfig payload unmarshals cleanly into xdsv1alpha1.TelemetryCollector.
	require.NotNil(t, cfg.GetXdsConfig())
	var dumpedCollector xdsv1alpha1.TelemetryCollector
	require.NoError(t, cfg.GetXdsConfig().UnmarshalTo(&dumpedCollector))
	assert.Len(t, dumpedCollector.GetPolicies(), 1)
}
