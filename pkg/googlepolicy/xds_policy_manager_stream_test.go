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
	"errors"
	"net"
	"net/url"
	"sort"
	"sync"
	"testing"
	"time"

	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)

// fakeADSServer is an in-memory AggregatedDiscoveryService. Each time a client
// opens a stream, the next handler in `handlers` runs; once they are exhausted,
// the stream blocks until the client goes away. Requests received from the
// client are recorded for assertions.
type fakeADSServer struct {
	discoveryv3.UnimplementedAggregatedDiscoveryServiceServer

	mu       sync.Mutex
	handlers []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error
	attempts int
	requests []*discoveryv3.DiscoveryRequest

	// streamOpened receives a value each time a client opens a stream.
	streamOpened chan struct{}
}

func (s *fakeADSServer) StreamAggregatedResources(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	s.mu.Lock()
	attempt := s.attempts
	s.attempts++
	var handler func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error
	if attempt < len(s.handlers) {
		handler = s.handlers[attempt]
	}
	s.mu.Unlock()

	select {
	case s.streamOpened <- struct{}{}:
	default:
	}

	if handler == nil {
		// No behavior left to script: hold the stream open until the client
		// disconnects, so the manager settles instead of spinning.
		<-stream.Context().Done()
		return stream.Context().Err()
	}
	return handler(stream)
}

// recv reads one request from the stream and records it.
func (s *fakeADSServer) recv(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) (*discoveryv3.DiscoveryRequest, error) {
	req, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	return req, nil
}

func (s *fakeADSServer) attemptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

func (s *fakeADSServer) recordedRequests() []*discoveryv3.DiscoveryRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*discoveryv3.DiscoveryRequest(nil), s.requests...)
}

// startFakeADSServer serves the given fake over an in-memory listener and
// returns a dial option that reaches it.
func startFakeADSServer(t *testing.T, srv *fakeADSServer) grpc.DialOption {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	discoveryv3.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)

	go func() {
		// Errors here are expected once the listener is closed during cleanup.
		_ = grpcServer.Serve(lis)
	}()
	t.Cleanup(grpcServer.Stop)

	return grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	})
}

// newTestManager builds a manager wired to the fake server, with a negligible
// backoff so reconnect tests stay fast.
func newTestManager(t *testing.T, dialOpt grpc.DialOption) *xdsPolicyManager {
	t.Helper()

	u, err := url.Parse("xds://127.0.0.1:8080?insecure=true&gcp.fleet_id=fleet-1")
	require.NoError(t, err)

	mgr, err := NewXDSPolicyManager(zaptest.NewLogger(t), u, "collector-abc", "fleet-1", nil)
	require.NoError(t, err)

	m := mgr.(*xdsPolicyManager)
	m.backoffInitial = time.Millisecond
	m.backoffMax = 2 * time.Millisecond
	m.extraDialOpts = []grpc.DialOption{
		dialOpt,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	return m
}

// policyResource builds a DiscoveryResponse resource carrying a single policy.
func policyResource(t *testing.T, name, policyType string) *anypb.Any {
	t.Helper()

	payload, err := structpb.NewStruct(map[string]any{
		"name": name,
		"type": policyType,
	})
	require.NoError(t, err)

	payloadAny, err := anypb.New(payload)
	require.NoError(t, err)

	collectorAny, err := anypb.New(&xdsv1alpha1.TelemetryCollector{
		Policies: []*anypb.Any{payloadAny},
	})
	require.NoError(t, err)

	return collectorAny
}

// resetActivePolicySet clears the package-global policy state so tests that
// activate a policy set do not leak into each other.
func resetActivePolicySet(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for ActivePolicySet() != nil {
			RollbackActivePolicySet()
		}
	})
}

func TestXDSPolicyManager_ReconnectsWithBackoff(t *testing.T) {
	_ = RegisterPolicyDriver("mock_transformation", &dummyPolicyDriver{})
	resetActivePolicySet(t)

	ackReceived := make(chan *discoveryv3.DiscoveryRequest, 1)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		// Attempt 1: drop the stream immediately, before sending anything.
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			return errors.New("simulated stream failure")
		},
		// Attempt 2: deliver a policy, collect the ACK, then drop the stream.
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-1",
				Nonce:       "nonce-1",
				TypeUrl:     defaultXdsTypeURL,
				Resources:   []*anypb.Any{policyResource(t, "log-filter", "mock_transformation")},
			}); err != nil {
				return err
			}
			ack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			ackReceived <- ack
			return errors.New("simulated stream failure after ACK")
		},
		// Attempt 3: record the reconnect request, then hold the stream open.
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	// The policy from the second attempt must be ACKed with its version+nonce.
	var ack *discoveryv3.DiscoveryRequest
	select {
	case ack = <-ackReceived:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for ACK")
	}
	assert.Equal(t, "rev-1", ack.GetVersionInfo())
	assert.Equal(t, "nonce-1", ack.GetResponseNonce())
	assert.Nil(t, ack.GetErrorDetail(), "a valid policy set must be ACKed, not NACKed")

	// The manager must come back after each drop, reaching the third attempt.
	require.Eventually(t, func() bool {
		return srv.attemptCount() >= 3
	}, 10*time.Second, 10*time.Millisecond, "manager did not reconnect after stream failures")

	// The policy set from the dropped stream stays active across reconnects, so
	// the rest of the pipeline keeps running on it.
	active := ActivePolicySet()
	require.NotNil(t, active)
	assert.Equal(t, "rev-1", active.RevisionID)
	assert.Contains(t, active.Policies, "log-filter")

	// On reconnect the client reports the version it is still running.
	requests := srv.recordedRequests()
	require.GreaterOrEqual(t, len(requests), 3)
	initial := requests[0]
	assert.Empty(t, initial.GetVersionInfo(), "first request has nothing applied yet")
	assert.Equal(t, "collector-abc", initial.GetNode().GetId())
	assert.Equal(t, "fleet-1", initial.GetNode().GetCluster())

	reconnect := requests[len(requests)-1]
	assert.Equal(t, "rev-1", reconnect.GetVersionInfo(), "reconnect must resend the last applied version")
	assert.Empty(t, reconnect.GetResponseNonce(), "a fresh stream carries no nonce")
}

func TestXDSPolicyManager_NACKsUndecodableResource(t *testing.T) {
	resetActivePolicySet(t)

	nackReceived := make(chan *discoveryv3.DiscoveryRequest, 1)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			// A resource whose type URL is not in the proto registry.
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-bad",
				Nonce:       "nonce-bad",
				TypeUrl:     defaultXdsTypeURL,
				Resources: []*anypb.Any{{
					TypeUrl: "type.googleapis.com/does.not.Exist",
					Value:   []byte("garbage"),
				}},
			}); err != nil {
				return err
			}
			nack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			nackReceived <- nack
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	var nack *discoveryv3.DiscoveryRequest
	select {
	case nack = <-nackReceived:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for NACK")
	}

	require.NotNil(t, nack.GetErrorDetail(), "an undecodable resource must be NACKed")
	assert.Contains(t, nack.GetErrorDetail().GetMessage(), "does.not.Exist")
	assert.Equal(t, "nonce-bad", nack.GetResponseNonce())
	assert.Empty(t, nack.GetVersionInfo(), "NACK reports the last applied version, which is none")

	// A rejected response must not become the active policy set.
	assert.Nil(t, ActivePolicySet())
}

func TestXDSPolicyManager_Lifecycle(t *testing.T) {
	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	m := newTestManager(t, startFakeADSServer(t, srv))

	// Stop before Start is a no-op.
	require.NoError(t, m.Stop())

	require.NoError(t, m.Start())

	// Start is idempotent: a second call is rejected rather than leaking the
	// first connection and goroutine.
	require.ErrorIs(t, m.Start(), ErrXDSAlreadyStarted)

	// Wait until the stream is actually up so Stop races with a live loop.
	select {
	case <-srv.streamOpened:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the stream to open")
	}

	require.NoError(t, m.Stop())
	// Stop is idempotent too.
	require.NoError(t, m.Stop())

	// After a clean Stop the manager can be restarted.
	require.NoError(t, m.Start())
	require.NoError(t, m.Stop())
}

// TestXDSPolicyManager_ConcurrentStop asserts that every caller of Stop waits
// for the stream loop to exit, not just whichever one won the race to cancel it.
func TestXDSPolicyManager_ConcurrentStop(t *testing.T) {
	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	m := newTestManager(t, startFakeADSServer(t, srv))

	require.NoError(t, m.Start())
	select {
	case <-srv.streamOpened:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the stream to open")
	}

	const stoppers = 4
	var wg sync.WaitGroup
	wg.Add(stoppers)
	for range stoppers {
		go func() {
			defer wg.Done()
			assert.NoError(t, m.Stop())
		}()
	}
	wg.Wait()

	// Every Stop has returned, so the loop is guaranteed to be finished and the
	// manager is safe to restart.
	require.NoError(t, m.Start())
	require.NoError(t, m.Stop())
}

// activePolicyNames returns the names in the active policy set, or nil if there
// is no active set.
func activePolicyNames(t *testing.T) []string {
	t.Helper()
	ps := ActivePolicySet()
	if ps == nil {
		return nil
	}
	names := make([]string, 0, len(ps.Policies))
	for name := range ps.Policies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestXDSPolicyManager_IgnoresUnrelatedResourceType asserts that a response for
// a resource type we never subscribed to cannot disturb the active policy set.
// An ADS stream is multiplexed, so unrelated responses are expected traffic; an
// empty one previously cleared every policy and was ACKed.
func TestXDSPolicyManager_IgnoresUnrelatedResourceType(t *testing.T) {
	_ = RegisterPolicyDriver("mock_transformation", &dummyPolicyDriver{})
	resetActivePolicySet(t)

	responded := make(chan *discoveryv3.DiscoveryRequest, 4)
	unrelatedSent := make(chan struct{})
	// Closed once the test has observed the rev-1 policy set. Without this
	// gate the server races ahead to rev-3 and the "log-filter" assertion
	// below can observe the replacement instead.
	firstAsserted := make(chan struct{})

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}

			// Establish a policy we can watch for damage.
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-1", Nonce: "n1", TypeUrl: defaultXdsTypeURL,
				Resources: []*anypb.Any{policyResource(t, "log-filter", "mock_transformation")},
			}); err != nil {
				return err
			}
			ack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			responded <- ack
			<-firstAsserted

			// An unrelated, empty response: the shape that used to wipe policies.
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-2", Nonce: "n2",
				TypeUrl:   "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources: nil,
			}); err != nil {
				return err
			}
			close(unrelatedSent)

			// Then a real update, which must still be processed normally. Its
			// arrival proves the unrelated response was handled (ignored) first.
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-3", Nonce: "n3", TypeUrl: defaultXdsTypeURL,
				Resources: []*anypb.Any{policyResource(t, "trace-filter", "mock_transformation")},
			}); err != nil {
				return err
			}
			ack3, err := srv.recv(stream)
			if err != nil {
				return err
			}
			responded <- ack3

			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	first := <-responded
	require.Equal(t, "rev-1", first.GetVersionInfo())
	require.Equal(t, []string{"log-filter"}, activePolicyNames(t))
	close(firstAsserted)

	<-unrelatedSent

	// The next response we get back must be the ACK for rev-3, never one for
	// rev-2: the unrelated response is ignored without a reply.
	third := <-responded
	assert.Equal(t, "rev-3", third.GetVersionInfo(), "the unrelated response must not be ACKed")

	// rev-2 never touched the policy set; rev-3 replaced it as normal.
	assert.Equal(t, []string{"trace-filter"}, activePolicyNames(t))
	assert.Equal(t, "rev-3", ActivePolicySet().RevisionID)
}

// TestXDSPolicyManager_SkipsAlreadyAppliedRevision asserts that a repeat of the
// revision we are already running is ACKed but not re-applied.
func TestXDSPolicyManager_SkipsAlreadyAppliedRevision(t *testing.T) {
	_ = RegisterPolicyDriver("mock_transformation", &dummyPolicyDriver{})
	resetActivePolicySet(t)

	responded := make(chan *discoveryv3.DiscoveryRequest, 4)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-1", Nonce: "n1", TypeUrl: defaultXdsTypeURL,
				Resources: []*anypb.Any{policyResource(t, "log-filter", "mock_transformation")},
			}); err != nil {
				return err
			}
			ack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			responded <- ack

			// Same version, different content. The version is the control
			// plane's identity for a revision, so this must not be applied.
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-1", Nonce: "n2", TypeUrl: defaultXdsTypeURL,
				Resources: []*anypb.Any{policyResource(t, "different-policy", "mock_transformation")},
			}); err != nil {
				return err
			}
			ack2, err := srv.recv(stream)
			if err != nil {
				return err
			}
			responded <- ack2

			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	<-responded
	require.Equal(t, []string{"log-filter"}, activePolicyNames(t))

	// The repeat is still answered, so the control plane is not left waiting.
	repeat := <-responded
	assert.Equal(t, "rev-1", repeat.GetVersionInfo())
	assert.Equal(t, "n2", repeat.GetResponseNonce())
	assert.Nil(t, repeat.GetErrorDetail())

	// ...but the policy set was not rebuilt from it.
	assert.Equal(t, []string{"log-filter"}, activePolicyNames(t))
}

// TestXDSPolicyManager_EmptyRevisionClearsPolicies documents the deliberate
// counterpart to the filter above: an empty revision for *our own* type really
// does mean "drop everything", and is applied.
func TestXDSPolicyManager_EmptyRevisionClearsPolicies(t *testing.T) {
	_ = RegisterPolicyDriver("mock_transformation", &dummyPolicyDriver{})
	resetActivePolicySet(t)

	responded := make(chan *discoveryv3.DiscoveryRequest, 4)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-1", Nonce: "n1", TypeUrl: defaultXdsTypeURL,
				Resources: []*anypb.Any{policyResource(t, "log-filter", "mock_transformation")},
			}); err != nil {
				return err
			}
			ack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			responded <- ack

			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-2", Nonce: "n2", TypeUrl: defaultXdsTypeURL,
				Resources: nil,
			}); err != nil {
				return err
			}
			ack2, err := srv.recv(stream)
			if err != nil {
				return err
			}
			responded <- ack2

			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	<-responded
	require.Equal(t, []string{"log-filter"}, activePolicyNames(t))

	cleared := <-responded
	assert.Equal(t, "rev-2", cleared.GetVersionInfo())
	assert.Nil(t, cleared.GetErrorDetail())
	assert.Empty(t, activePolicyNames(t), "an empty revision for our own type clears the policy set")
}
