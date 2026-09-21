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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
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

	mgr, err := NewXDSPolicyManager(zaptest.NewLogger(t), u, "collector-abc")
	require.NoError(t, err)

	m := mgr.(*xdsPolicyManager)
	m.backoffInitial = time.Millisecond
	m.backoffMax = 2 * time.Millisecond
	// Several tests drive servers that never answer; without this Start would
	// sit out the full production timeout in each of them.
	m.initialSyncTimeout = 10 * time.Millisecond
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
				TypeUrl:     xdsPolicyTypeURL,
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
				TypeUrl:     xdsPolicyTypeURL,
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

// A revision is only rejected when nothing in it is usable. If at least one
// policy loads, it is applied and the revision is ACKed, so a single bad policy
// from the control plane cannot disable all enforcement.
func TestXDSPolicyManager_ACKsRevisionWithSomeUsablePolicies(t *testing.T) {
	resetActivePolicySet(t)

	ackReceived := make(chan *discoveryv3.DiscoveryRequest, 1)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			// One resource the collector understands, one it does not.
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-partial",
				Nonce:       "nonce-partial",
				TypeUrl:     xdsPolicyTypeURL,
				Resources: []*anypb.Any{
					policyResource(t, "log-filter", "mock_transformation"),
					{
						TypeUrl: "type.googleapis.com/does.not.Exist",
						Value:   []byte("garbage"),
					},
				},
			}); err != nil {
				return err
			}
			ack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			ackReceived <- ack
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	var ack *discoveryv3.DiscoveryRequest
	select {
	case ack = <-ackReceived:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for ACK")
	}

	require.Nil(t, ack.GetErrorDetail(), "a revision with at least one usable policy must be ACKed")
	assert.Equal(t, "rev-partial", ack.GetVersionInfo())
	assert.Equal(t, "nonce-partial", ack.GetResponseNonce())

	// The policy that loaded is applied; the undecodable resource is dropped.
	active := ActivePolicySet()
	require.NotNil(t, active)
	assert.Equal(t, "rev-partial", active.RevisionID)
	assert.Contains(t, active.Policies, "log-filter")
	assert.Len(t, active.Policies, 1, "only the usable policy is applied")
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
				VersionInfo: "rev-1", Nonce: "n1", TypeUrl: xdsPolicyTypeURL,
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
				VersionInfo: "rev-3", Nonce: "n3", TypeUrl: xdsPolicyTypeURL,
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
				VersionInfo: "rev-1", Nonce: "n1", TypeUrl: xdsPolicyTypeURL,
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
				VersionInfo: "rev-1", Nonce: "n2", TypeUrl: xdsPolicyTypeURL,
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
				VersionInfo: "rev-1", Nonce: "n1", TypeUrl: xdsPolicyTypeURL,
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
				VersionInfo: "rev-2", Nonce: "n2", TypeUrl: xdsPolicyTypeURL,
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

// TestXDSPolicyManager_StartWaitsForInitialPolicySet asserts the contract the
// provider depends on: by the time Start returns, the first revision the control
// plane served is already active. The provider evaluates the active set
// immediately after Start, so a fire-and-forget Start would race it and silently
// come up on built-in policies even though the control plane was reachable.
func TestXDSPolicyManager_StartWaitsForInitialPolicySet(t *testing.T) {
	_ = RegisterPolicyDriver("mock_transformation", &dummyPolicyDriver{})
	resetActivePolicySet(t)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			// A control plane that answers instantly would let a broken Start
			// pass by luck, so make it visibly slow.
			time.Sleep(50 * time.Millisecond)
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-1", Nonce: "n1", TypeUrl: xdsPolicyTypeURL,
				Resources: []*anypb.Any{policyResource(t, "log-filter", "mock_transformation")},
			}); err != nil {
				return err
			}
			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	// Far longer than the server takes, so a Start that returns quickly proves
	// it was released by the revision landing rather than by the wait expiring.
	m.initialSyncTimeout = 10 * time.Second

	start := time.Now()
	require.NoError(t, m.Start())
	elapsed := time.Since(start)
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	assert.Equal(t, []string{"log-filter"}, activePolicyNames(t),
		"Start must not return before the first revision is applied")
	assert.Less(t, elapsed, m.initialSyncTimeout/2,
		"Start waited out its timeout instead of being released by the first ACK")
}

// TestXDSPolicyManager_StartGivesUpAfterTimeout asserts the wait is bounded. An
// unresponsive control plane must delay collector startup, never block it: Start
// returns nil and the collector comes up on its built-in policies while the
// stream keeps retrying in the background.
func TestXDSPolicyManager_StartGivesUpAfterTimeout(t *testing.T) {
	resetActivePolicySet(t)

	// No handlers, so the server accepts the stream and then says nothing.
	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}

	m := newTestManager(t, startFakeADSServer(t, srv))
	m.initialSyncTimeout = 100 * time.Millisecond

	start := time.Now()
	require.NoError(t, m.Start())
	elapsed := time.Since(start)
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	assert.GreaterOrEqual(t, elapsed, m.initialSyncTimeout, "Start returned before the timeout elapsed")
	assert.Less(t, elapsed, 5*time.Second, "Start blocked well past its timeout")
	assert.Nil(t, ActivePolicySet(), "nothing was served, so nothing should be active")
}

// TestXDSPolicyManager_StopsOnTerminalAuthError asserts that a credential
// rejection ends the loop instead of being retried forever. Unauthenticated and
// PermissionDenied are verdicts on this collector's identity, so reconnecting
// only spams the control plane with a request that cannot start succeeding.
//
// The two cases differ only in whether the server reads the collector's request
// before rejecting it, which decides where gRPC surfaces the status. A server
// that reads first fails the client's Recv with PermissionDenied. A server that
// rejects outright -- what authorization in an interceptor looks like, and so
// the more realistic of the two -- has already torn the stream down by the time
// the client sends, and gRPC reports that to Send as a bare io.EOF with the
// status available only from Recv. Both must reach isTerminalAuthError; the
// second did not before the Send path learned to fall through.
func TestXDSPolicyManager_StopsOnTerminalAuthError(t *testing.T) {
	for _, tc := range []struct {
		name       string
		readsFirst bool
	}{
		{name: "server rejects before reading the request", readsFirst: false},
		{name: "server rejects after reading the request", readsFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetActivePolicySet(t)

			srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
			srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
				func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
					if tc.readsFirst {
						if _, err := srv.recv(stream); err != nil {
							return err
						}
					}
					return grpcstatus.Error(codes.PermissionDenied, "collector is not authorized for this fleet")
				},
			}

			m := newTestManager(t, startFakeADSServer(t, srv))
			// Long enough that a Start returning early proves the loop gave up
			// rather than that the wait simply expired.
			m.initialSyncTimeout = 10 * time.Second

			require.NoError(t, m.Start())
			t.Cleanup(func() { require.NoError(t, m.Stop()) })

			// Backoff here is ~1ms, so any retry would have happened many times over.
			time.Sleep(100 * time.Millisecond)
			assert.Equal(t, 1, srv.attemptCount(), "a rejected collector must not reconnect")
			assert.Nil(t, ActivePolicySet())
		})
	}
}

func TestIsTerminalAuthError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "unauthenticated", err: grpcstatus.Error(codes.Unauthenticated, "no token"), want: true},
		{name: "permission denied", err: grpcstatus.Error(codes.PermissionDenied, "not allowed"), want: true},
		{name: "unavailable is retryable", err: grpcstatus.Error(codes.Unavailable, "down"), want: false},
		{name: "cancelled is retryable", err: grpcstatus.Error(codes.Canceled, "stopped"), want: false},
		// A non-gRPC error maps to codes.Unknown, which must stay retryable:
		// local failures such as a metadata server blip are transient.
		{name: "plain error", err: errors.New("dial failed"), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isTerminalAuthError(tc.err))
		})
	}
}

// TestXDSPolicyManager_StopUnblocksStart asserts that stopping a manager while
// Start is still waiting releases it immediately. Start's wait is released from
// a defer in the stream loop precisely so that shutting down mid-startup does
// not force the caller to sit out the whole initial-sync timeout.
func TestXDSPolicyManager_StopUnblocksStart(t *testing.T) {
	resetActivePolicySet(t)

	// No handlers: the server accepts the stream and then stays silent, so the
	// only thing that can release Start is the Stop below.
	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}

	m := newTestManager(t, startFakeADSServer(t, srv))
	m.initialSyncTimeout = 30 * time.Second

	go func() {
		// Wait until the loop is actually up, so Stop cannot land before Start
		// has registered its cancel func and turn into a no-op.
		<-srv.streamOpened
		assert.NoError(t, m.Stop())
	}()

	start := time.Now()
	require.NoError(t, m.Start())
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 5*time.Second,
		"Stop must release a waiting Start rather than leave it until the timeout")
}

// TestXDSPolicyManager_AppliesRevisionAfterTimeout asserts that giving up on the
// *wait* does not mean giving up on the *stream*. Start returning early is a
// startup concession, not a teardown: a revision that shows up later must still
// be applied, which is what keeps a collector that booted before its control
// plane from being stuck on built-in policies forever.
func TestXDSPolicyManager_AppliesRevisionAfterTimeout(t *testing.T) {
	_ = RegisterPolicyDriver("mock_transformation", &dummyPolicyDriver{})
	resetActivePolicySet(t)

	responded := make(chan *discoveryv3.DiscoveryRequest, 2)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			// Deliberately slower than the manager is willing to wait.
			time.Sleep(300 * time.Millisecond)
			if err := stream.Send(&discoveryv3.DiscoveryResponse{
				VersionInfo: "rev-late", Nonce: "n1", TypeUrl: xdsPolicyTypeURL,
				Resources: []*anypb.Any{policyResource(t, "late-filter", "mock_transformation")},
			}); err != nil {
				return err
			}
			ack, err := srv.recv(stream)
			if err != nil {
				return err
			}
			responded <- ack

			<-stream.Context().Done()
			return stream.Context().Err()
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	m.initialSyncTimeout = 50 * time.Millisecond

	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	// Start gave up before the control plane answered, so the collector came up
	// on built-in policies.
	require.Nil(t, ActivePolicySet(), "the revision cannot have arrived this early")

	// The stream stayed open, so the late revision still lands and is ACKed.
	select {
	case ack := <-responded:
		assert.Equal(t, "rev-late", ack.GetVersionInfo())
		assert.Nil(t, ack.GetErrorDetail(), "the late revision should be ACKed, not NACKed")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the late revision to be applied")
	}
	assert.Equal(t, []string{"late-filter"}, activePolicyNames(t))
}

// TestXDSPolicyManager_StopDoesNotHangWhenStartRaces pins the fix for a
// deadlock: Stop used to clear the manager's cancel func and then wait on a
// WaitGroup shared by every generation of the stream loop. A Start landing in
// that window saw a cleared cancel, concluded nothing was running, and launched
// a fresh loop with an uncancelled context -- which the in-flight Wait then
// blocked on forever.
//
// Stop now waits only on the loop it cancelled, and a racing Start is turned
// away instead of starting a second one.
func TestXDSPolicyManager_StopDoesNotHangWhenStartRaces(t *testing.T) {
	resetActivePolicySet(t)

	// Repeated because the window is small: a single pass can easily have Start
	// take mu before Stop and never exercise the interleaving at all.
	for i := range 20 {
		srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
		m := newTestManager(t, startFakeADSServer(t, srv))

		require.NoError(t, m.Start())
		select {
		case <-srv.streamOpened:
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: timed out waiting for the stream to open", i)
		}

		startDone := make(chan error, 1)
		stopDone := make(chan error, 1)
		go func() { startDone <- m.Start() }()
		go func() { stopDone <- m.Stop() }()

		select {
		case err := <-stopDone:
			assert.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: Stop deadlocked against a concurrent Start", i)
		}
		select {
		case err := <-startDone:
			// Either outcome is correct. Whichever took mu first decides: a
			// Start that got there before Stop started a loop, and one that got
			// there after was told a loop is still being torn down.
			if err != nil {
				assert.ErrorIs(t, err, ErrXDSAlreadyStarted)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: Start never returned", i)
		}

		require.NoError(t, m.Stop())
	}
}

// TestXDSPolicyManager_RestartableAfterTerminalAuthError asserts that giving up
// on remote policies leaves a manager that can still be started again.
//
// The loop returns on a terminal auth error without anyone calling Stop, so
// unless it clears its own run state the manager is left advertising a stream
// loop that no longer exists: every later Start is rejected with
// ErrXDSAlreadyStarted and the collector can never pick up new credentials
// without a process restart.
func TestXDSPolicyManager_RestartableAfterTerminalAuthError(t *testing.T) {
	resetActivePolicySet(t)

	srv := &fakeADSServer{streamOpened: make(chan struct{}, 8)}
	srv.handlers = []func(discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error{
		func(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
			// The request is read before rejecting so the status comes back to
			// the client through Recv. A server that rejects without reading
			// races the client's initial Send into io.EOF instead, which is a
			// separate gap and not what this test is about.
			if _, err := srv.recv(stream); err != nil {
				return err
			}
			return grpcstatus.Error(codes.PermissionDenied, "collector is not authorized for this fleet")
		},
	}

	m := newTestManager(t, startFakeADSServer(t, srv))
	require.NoError(t, m.Start())
	t.Cleanup(func() { require.NoError(t, m.Stop()) })

	// Start returns as soon as the loop gives up, so by here it has exited.
	require.Eventually(t, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.cancel == nil && m.done == nil
	}, 10*time.Second, 10*time.Millisecond,
		"a loop that gave up must clear its run state")

	assert.NoError(t, m.Start(), "the manager must be startable after it gave up on remote policies")
}
