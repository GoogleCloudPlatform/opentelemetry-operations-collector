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
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	grpcstatus "google.golang.org/grpc/status"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"

	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)

const (
	// xdsPolicyTypeURL is the one resource type this manager subscribes to. It
	// is fixed rather than configurable: the control plane only ever serves
	// TelemetryCollector resources, and the decoding path below is written
	// against that schema, so a different type could be subscribed to but not
	// usefully interpreted.
	xdsPolicyTypeURL = "type.googleapis.com/google.telemetry.xds.v1alpha1.TelemetryCollector"

	// FleetIDQueryParam is the query parameter carrying the fleet ID:
	//
	//	googlecontrolplane:xds://telemetrydirector.googleapis.com?fleet=FLEET&project=PROJECT
	//
	// This deliberately matches the parameter the googlecontrolplane provider
	// reads when it resolves the fleet for the self metrics policy. If the two
	// ever diverge, a single URI would subscribe to one fleet's policies while
	// attributing the collector's own telemetry to another -- or, because a
	// missing fleet ID is fatal for self metrics, fail to start at all.
	FleetIDQueryParam = "fleet"

	// fleetIDEnvVar is the environment variable consulted for the fleet ID when
	// the URI carries no fleet query parameter.
	fleetIDEnvVar = "FLEET_ID"

	// defaultRegion is the locality region reported to the control plane.
	// TODO: Derive this from the environment (GCE metadata / GKE topology labels)
	// rather than hardcoding a single region.
	defaultRegion = "asia-east1"

	// Reconnect backoff defaults. The stream is re-established after any error,
	// with the delay growing from defaultBackoffInitial up to defaultBackoffMax.
	defaultBackoffInitial = 1 * time.Second
	defaultBackoffMax     = 30 * time.Second
	backoffMultiplier     = 1.6
	// backoffJitterFraction is the fraction of the delay randomized in either
	// direction, to avoid a fleet of collectors reconnecting in lockstep.
	backoffJitterFraction = 0.2

	// Keepalive ping interval and the window allowed for a reply. Together they
	// bound how long a silently dead connection can go unnoticed.
	//
	// These must be paired with the control plane's gRPC enforcement policy:
	// a server's default keepalive.EnforcementPolicy.MinTime is 5 minutes, and
	// pinging faster than that earns a GOAWAY with ENHANCE_YOUR_CALM. gRPC then
	// doubles Time on its own, so a mismatch degrades to slower detection rather
	// than a reconnect loop, but the control plane should set
	// EnforcementPolicy{MinTime: 30s} so these values are honored.
	keepaliveTime    = 60 * time.Second
	keepaliveTimeout = 20 * time.Second

	// defaultInitialSyncTimeout bounds how long Start blocks waiting for the
	// control plane's first response.
	//
	// Start blocks at all because the confmap provider calls it and then
	// immediately builds the collector's pipelines from the active policy set.
	// Nothing rebuilds that config later, so a policy set that arrives after
	// Retrieve has returned cannot influence the pipelines until the next
	// restart. Waiting briefly here is what lets the first revision take effect.
	//
	// It is bounded, and expiry is not an error, because the alternative -- a
	// collector that refuses to start because its control plane is down -- is
	// far worse than one that starts on its built-in policies and picks up the
	// real ones moments later.
	defaultInitialSyncTimeout = 5 * time.Second
)

var (
	// Configuration errors, returned by NewXDSPolicyManager.
	ErrXDSMissingServerAddr   = errors.New("xDS server address cannot be empty in URI")
	ErrXDSMissingCollectorID  = errors.New("a collector ID is required for the xDS policy manager")
	ErrXDSMissingFleetID      = errors.New("a fleet ID is required for the xDS policy manager: set the 'fleet' URI query parameter or the FLEET_ID environment variable")
	ErrXDSInvalidInsecureFlag = errors.New("invalid 'insecure' URI query parameter, expected a boolean")

	// Lifecycle errors.
	ErrXDSAlreadyStarted = errors.New("xDS policy manager is already started")

	// Resource decoding errors, reported back to the control plane via NACK.
	ErrXDSResourceDecode    = errors.New("failed to decode xDS resource")
	ErrXDSPolicyDecode      = errors.New("failed to decode policy from xDS resource")
	ErrXDSPolicyMissingBody = errors.New("policy in xDS resource has no typed_config")
	ErrXDSResourceNotPolicy = errors.New("xDS resource does not contain a policy")
)

var _ Manager = (*xdsPolicyManager)(nil)

// xdsPolicyManager connects to an xDS control plane, parses received policies,
// updates ActivePolicySet, and sends ACKs/NACKs over the ADS stream.
//
// The manager keeps the stream alive for the lifetime of the collector: if the
// stream drops, it reconnects with exponential backoff. The active policy set is
// never cleared on disconnect, so the rest of the pipeline keeps running on the
// last known good policies while the manager is reconnecting.
type xdsPolicyManager struct {
	logger *zap.Logger

	// Connection settings, resolved once in the constructor and never written
	// again. Being immutable, they are read from the stream goroutine without
	// holding mu.
	uri         *url.URL
	serverAddr  string
	collectorID string
	fleetID     string
	insecure    bool

	// NOTE: there is deliberately no confmap reload hook here. The only policies
	// delivered over xDS today are transformation (filter) policies, which
	// googlepolicyprocessor picks up straight from the active policy set via its
	// own watcher channel, with no collector config regeneration required. Once
	// source or destination policies are delivered over xDS, this manager will
	// need a confmap.WatcherFunc to rebuild the config.

	// Backoff bounds and the sleep function, overridable in tests.
	backoffInitial time.Duration
	backoffMax     time.Duration
	timeAfter      func(time.Duration) <-chan time.Time

	// initialSyncTimeout bounds Start's wait for the first exchange with the
	// control plane. Overridable in tests.
	initialSyncTimeout time.Duration

	// extraDialOpts is appended to the dial options. Tests use it to reach an
	// in-memory listener; it is empty in production.
	extraDialOpts []grpc.DialOption

	// mu guards cancel below. It is never held across a blocking call (stream
	// I/O, or waiting on wg).
	mu     sync.Mutex
	cancel context.CancelFunc

	// ready is closed once the stream loop has either completed one valid
	// exchange with the control plane or stopped trying. Start waits on it.
	// readyClosed guards against closing it twice; a sync.Once is avoided here
	// because the pair is recreated on every Start and a Once cannot be copied.
	ready       chan struct{}
	readyClosed bool

	// wg tracks the stream goroutine so Stop can wait for it to exit.
	wg sync.WaitGroup

	// lastAppliedVersion is written by the stream goroutine and read by Stop's
	// callers, so it takes mu as well.
	lastAppliedVersion string
}

// NewXDSPolicyManager creates a manager for the given `xds://` URI.
//
// # URI form
//
// In full, as written in the collector's config:
//
//	googlecontrolplane:xds://HOST[:PORT][?fleet=FLEET][&project=PROJECT][&insecure=BOOL]
//
// for example:
//
//	googlecontrolplane:xds://telemetrydirector.googleapis.com:443?fleet=my-fleet&project=my-project
//	googlecontrolplane:xds://127.0.0.1:18000?fleet=my-fleet&insecure=true
//
// The `googlecontrolplane:` prefix selects the confmap provider and is stripped
// before the remainder reaches this constructor, so the uri argument here starts
// at `xds://`. The opaque spelling `xds:HOST:PORT` is accepted as well.
//
// # Components
//
//	HOST[:PORT] - required. Address of the xDS control plane.
//	fleet       - required, unless $FLEET_ID is set; the URI wins over the
//	              environment. Used as this node's xDS cluster, and read back off
//	              URI() by the provider to attribute the collector's own telemetry.
//	project     - optional, and NOT read here. The provider reads it off URI() to
//	              stamp gcp.project_id on self metrics; when absent, resource
//	              detection falls back to the project the collector runs in.
//	insecure    - optional bool, default false. False connects with TLS 1.2+ and
//	              an ADC-derived ID token per RPC; true connects in plaintext with
//	              no credentials, which is intended for local control planes.
//
// Unrecognized query parameters are ignored. The subscribed resource type is
// deliberately not configurable -- see xdsPolicyTypeURL.
//
// # Arguments
//
// collectorID identifies this collector to the control plane as the xDS node ID.
// It is passed in rather than read from the environment because the confmap
// provider derives it, and that package already depends on this one.
//
// All configuration is validated here, so a manager that constructs
// successfully can always Start.
func NewXDSPolicyManager(logger *zap.Logger, uri *url.URL, collectorID string) (Manager, error) {
	if uri == nil {
		return nil, ErrXDSMissingServerAddr
	}
	if collectorID == "" {
		return nil, ErrXDSMissingCollectorID
	}

	serverAddr := serverAddrFromURI(uri)
	if serverAddr == "" {
		return nil, ErrXDSMissingServerAddr
	}

	query := uri.Query()

	// Explicit URI configuration wins over the ambient environment.
	fleetID := FleetIDFromURI(uri)
	if fleetID == "" {
		fleetID = os.Getenv(fleetIDEnvVar)
	}
	if fleetID == "" {
		return nil, ErrXDSMissingFleetID
	}

	var isInsecure bool
	if raw := query.Get("insecure"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrXDSInvalidInsecureFlag, raw)
		}
		isInsecure = parsed
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &xdsPolicyManager{
		logger:             logger,
		uri:                uri,
		serverAddr:         serverAddr,
		collectorID:        collectorID,
		fleetID:            fleetID,
		insecure:           isInsecure,
		backoffInitial:     defaultBackoffInitial,
		backoffMax:         defaultBackoffMax,
		timeAfter:          time.After,
		initialSyncTimeout: defaultInitialSyncTimeout,
	}, nil
}

// serverAddrFromURI pulls the host[:port] out of an xDS URI. It may arrive as an
// authority ("xds://host:port") or, when the URI is opaque ("xds:host:port"),
// as the opaque section or the path.
//
// The port may be omitted, as it is in the canonical production form
// "xds://telemetrydirector.googleapis.com". gRPC's DNS resolver defaults a
// portless target to 443, and ResolveTokenSource falls back to the bare host
// when net.SplitHostPort fails, so the OIDC audience still comes out as
// "https://telemetrydirector.googleapis.com".
func serverAddrFromURI(uri *url.URL) string {
	for _, candidate := range []string{uri.Host, uri.Opaque, uri.Path} {
		if addr := strings.TrimPrefix(candidate, "/"); addr != "" {
			return addr
		}
	}
	return ""
}

// FleetIDFromURI returns the fleet ID carried by an xDS URI, or "" if it carries
// none.
//
// It reads the same `fleet` parameter that the googlecontrolplane provider
// reads when resolving the fleet for the self metrics policy, so one URI drives
// both the xDS subscription and the collector's own telemetry attribution.
func FleetIDFromURI(uri *url.URL) string {
	if uri == nil {
		return ""
	}
	return uri.Query().Get(FleetIDQueryParam)
}

// Start launches the background stream loop and waits, briefly, for the control
// plane to answer once.
//
// The wait exists because of how the confmap provider uses this manager: it
// calls Start and then immediately evaluates the active policy set into the
// collector's pipelines. Nothing rebuilds that config afterwards, so a revision
// that lands after Start has returned cannot affect the pipelines until the
// process restarts. Blocking here is what gives the first revision a chance to
// be included.
//
// The wait is bounded by initialSyncTimeout and never turns into an error.
// Whatever happens -- the control plane is down, slow, or rejects us -- Start
// returns nil and the collector comes up on its built-in policies while the
// stream keeps retrying in the background. The only error it can return is
// ErrXDSAlreadyStarted.
func (m *xdsPolicyManager) Start() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return ErrXDSAlreadyStarted
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	// Recreated per Start so the manager can be restarted after Stop.
	m.ready = make(chan struct{})
	m.readyClosed = false
	ready := m.ready
	timeout := m.initialSyncTimeout
	m.wg.Add(1)
	m.mu.Unlock()

	m.logger.Info("Starting xDS policy manager",
		zap.String("server", m.serverAddr),
		zap.String("fleet", m.fleetID),
		zap.String("collector_id", m.collectorID),
		zap.String("type_url", xdsPolicyTypeURL),
	)

	go m.run(ctx)

	select {
	case <-ready:
		// Either the control plane answered, or the loop gave up; in both cases
		// there is nothing further to wait for.
	case <-m.timeAfter(timeout):
		m.logger.Warn("Timed out waiting for the initial xDS policy set; starting on built-in policies",
			zap.String("server", m.serverAddr),
			zap.Duration("timeout", timeout),
		)
	}

	return nil
}

// markReady releases anything blocked in Start. It is safe to call repeatedly
// and from any goroutine.
func (m *xdsPolicyManager) markReady() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ready != nil && !m.readyClosed {
		m.readyClosed = true
		close(m.ready)
	}
}

// Stop terminates the background stream loop and waits for it to exit. It is
// safe to call before Start, more than once, and concurrently; afterwards the
// manager can be started again.
func (m *xdsPolicyManager) Stop() error {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Waited on outside the lock, since the stream loop takes mu itself. This
	// is a no-op if the loop was never started, and callers that lost the race
	// to cancel still block until it has actually exited.
	m.wg.Wait()
	return nil
}

// URI returns the configured URI.
func (m *xdsPolicyManager) URI() *url.URL {
	return m.uri
}

// PolicyEvaluationResult satisfies the Manager interface.
//
// TODO: Defer the ACK until the policy set has actually been evaluated, and NACK
// the corresponding nonce when evaluation fails, so the control plane is not told
// a revision is live when it could not be applied.
func (m *xdsPolicyManager) PolicyEvaluationResult(string, error) {}

// run maintains the ADS stream for the lifetime of the manager, reconnecting
// with exponential backoff whenever it drops.
func (m *xdsPolicyManager) run(ctx context.Context) {
	defer m.wg.Done()
	// Once this loop is gone there will never be an initial sync, so anything
	// still blocked in Start must be released regardless of why we exited:
	// Stop cancelled us, or the control plane rejected our credentials.
	defer m.markReady()

	node := &corev3.Node{
		Id:      m.collectorID,
		Cluster: m.fleetID,
		Locality: &corev3.Locality{
			Region: defaultRegion,
		},
	}

	// A single ClientConn is reused across stream attempts; gRPC reconnects the
	// underlying transport on its own. Only the stream is re-established here.
	var conn *grpc.ClientConn
	defer func() {
		if conn != nil {
			_ = conn.Close()
		}
	}()

	backoff := time.Duration(0)
	for {
		if ctx.Err() != nil {
			return
		}

		if conn == nil {
			newConn, err := m.dial(ctx)
			if err != nil {
				m.logger.Error("Failed to connect to xDS server, retrying",
					zap.String("server", m.serverAddr),
					zap.Error(err),
				)
				var ok bool
				if backoff, ok = m.waitBeforeRetry(ctx, backoff); !ok {
					return
				}
				continue
			}
			conn = newConn
		}

		// The active policy set is intentionally left in place across
		// disconnects so the pipeline keeps running on the last good revision.
		progressed, err := m.streamPolicies(ctx, conn, node)
		if ctx.Err() != nil {
			return
		}

		// A stream that delivered at least one response proves the endpoint is
		// healthy, so the next failure starts over from the minimum delay.
		if progressed {
			backoff = 0
		}

		// Being told we are not allowed to subscribe is a configuration or
		// provisioning problem, not a transient one: retrying cannot fix it and
		// would only spam the control plane. Give up on the stream for good and
		// leave the collector running on whatever policies it already has.
		if isTerminalAuthError(err) {
			m.logger.Error("xDS control plane rejected this collector's credentials; giving up on remote policies and continuing on built-in policies",
				zap.String("server", m.serverAddr),
				zap.String("fleet", m.fleetID),
				zap.String("collector_id", m.collectorID),
				zap.Error(err),
			)
			return
		}

		m.logger.Warn("xDS stream closed, reconnecting",
			zap.String("server", m.serverAddr),
			zap.String("last_applied_version", m.LastAppliedVersion()),
			zap.Error(err),
		)

		var ok bool
		if backoff, ok = m.waitBeforeRetry(ctx, backoff); !ok {
			return
		}
	}
}

// isTerminalAuthError reports whether the control plane refused this collector
// outright. Unauthenticated and PermissionDenied describe the caller, not the
// call, so the same request will keep failing until the collector's identity or
// the control plane's authorization changes -- neither of which a retry can do.
//
// Note that this deliberately covers only rejections that came back from the
// server. Failing to obtain credentials locally (see ResolveTokenSource) stays
// retryable, because that is usually a metadata server blip rather than a
// verdict on this collector.
func isTerminalAuthError(err error) bool {
	if err == nil {
		return false
	}

	switch grpcstatus.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return true
	default:
		return false
	}
}

// streamPolicies opens an ADS stream and processes responses until it fails or
// the context is cancelled. It reports whether any response was received, which
// the caller uses to decide whether to reset the backoff.
func (m *xdsPolicyManager) streamPolicies(ctx context.Context, conn *grpc.ClientConn, node *corev3.Node) (bool, error) {
	client := discoveryv3.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := client.StreamAggregatedResources(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to open xDS stream to %s: %w", m.serverAddr, err)
	}

	// On a fresh stream the client resends the last version it applied with an
	// empty nonce, so the control plane knows what this collector is running and
	// can skip re-sending an unchanged revision.
	if err := stream.Send(&discoveryv3.DiscoveryRequest{
		Node:        node,
		TypeUrl:     xdsPolicyTypeURL,
		VersionInfo: m.LastAppliedVersion(),
	}); err != nil {
		return false, fmt.Errorf("failed to send DiscoveryRequest to %s: %w", m.serverAddr, err)
	}

	m.logger.Info("Connected to xDS server and sent DiscoveryRequest",
		zap.String("server", m.serverAddr),
		zap.String("fleet", m.fleetID),
		zap.String("type_url", xdsPolicyTypeURL),
	)

	progressed := false
	for {
		resp, err := stream.Recv()
		if err != nil {
			return progressed, err
		}
		progressed = true

		m.handleResponse(stream, node, resp)
	}
}

// handleResponse validates a DiscoveryResponse, activates the policies it
// carries, and ACKs or NACKs it.
func (m *xdsPolicyManager) handleResponse(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesClient, node *corev3.Node, resp *discoveryv3.DiscoveryResponse) {
	// An ADS stream is multiplexed across resource types. A response for a type
	// this manager never subscribed to says nothing about our policies, so it is
	// ignored outright: it must not be able to replace, and therefore clear, the
	// active policy set. It is also not ACKed, since we are not a subscriber to
	// that type and the server owns the nonce bookkeeping for it.
	//
	// An empty type URL is treated as our own, since a server that only serves
	// one resource type may omit it.
	if respTypeURL := resp.GetTypeUrl(); respTypeURL != "" && respTypeURL != xdsPolicyTypeURL {
		m.logger.Debug("Ignoring xDS response for an unrelated resource type",
			zap.String("type_url", respTypeURL),
			zap.String("subscribed_type_url", xdsPolicyTypeURL),
		)
		return
	}

	m.logger.Info("Received xDS DiscoveryResponse",
		zap.String("version", resp.GetVersionInfo()),
		zap.String("nonce", resp.GetNonce()),
		zap.Int("resources", len(resp.GetResources())),
	)

	// Already running this revision. Re-applying it would be wasted work, but
	// the control plane is still waiting on a response for this nonce.
	if version := resp.GetVersionInfo(); version != "" && version == m.LastAppliedVersion() {
		m.logger.Debug("Re-ACKing an already applied xDS revision", zap.String("version", version))
		if err := m.sendACK(stream, node, version, resp.GetNonce()); err != nil {
			m.logger.Error("Failed to send xDS ACK", zap.String("version", version), zap.Error(err))
		}
		// The revision the control plane wants is the one already in effect, so
		// as far as Start is concerned the collector is in sync.
		m.markReady()
		return
	}

	rawPolicies, err := extractRawPolicies(resp)
	if err != nil {
		m.logger.Warn("Failed to extract policies from DiscoveryResponse, sending NACK",
			zap.String("version", resp.GetVersionInfo()),
			zap.Error(err),
		)
		m.sendNACK(stream, node, resp.GetNonce(), err)
		return
	}

	// A response for our own type carrying no resources is the control plane
	// telling us to drop everything. That is legitimate, but it disables all
	// policy enforcement, so it is never done quietly.
	if len(rawPolicies) == 0 && ActivePolicySet() != nil {
		m.logger.Warn("xDS revision contains no policies, clearing the active policy set",
			zap.String("version", resp.GetVersionInfo()),
		)
	}

	// 1. Validate & create policy set from DiscoveryResponse.
	policySet, err := MakePolicySet(resp.GetVersionInfo(), rawPolicies)
	if err != nil {
		m.logger.Warn("Failed to make policy set, sending NACK",
			zap.String("version", resp.GetVersionInfo()),
			zap.Error(err),
		)
		m.sendNACK(stream, node, resp.GetNonce(), err)
		return
	}

	// 2. Activate in memory (googlepolicyprocessor immediately picks this up).
	SetActivePolicySet(policySet)

	// 3. Send xDS ACK.
	if err := m.sendACK(stream, node, resp.GetVersionInfo(), resp.GetNonce()); err != nil {
		m.logger.Error("Failed to send xDS ACK",
			zap.String("version", resp.GetVersionInfo()),
			zap.Error(err),
		)
	}

	// The policy set is live, so Start can stop waiting and let the provider
	// evaluate it. Only reached once the revision was accepted; a NACKed
	// revision leaves Start waiting for a good one (or for its timeout).
	m.markReady()
}

// LastAppliedVersion returns the version_info of the most recent policy set that
// was successfully applied, or "" if none has been.
func (m *xdsPolicyManager) LastAppliedVersion() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastAppliedVersion
}

func (m *xdsPolicyManager) setLastAppliedVersion(versionInfo string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastAppliedVersion = versionInfo
}

// waitBeforeRetry blocks for the current backoff delay (with jitter applied) and
// returns the next base delay. It reports false if the context was cancelled
// while waiting, in which case the caller must stop.
func (m *xdsPolicyManager) waitBeforeRetry(ctx context.Context, base time.Duration) (time.Duration, bool) {
	if base <= 0 {
		base = m.backoffInitial
	}
	if base > m.backoffMax {
		base = m.backoffMax
	}

	// Randomize within +/- backoffJitterFraction so a fleet of collectors that
	// lost the control plane together does not stampede it on recovery.
	jitter := 1 + backoffJitterFraction*(2*rand.Float64()-1)
	delay := time.Duration(float64(base) * jitter)

	select {
	case <-ctx.Done():
		return base, false
	case <-m.timeAfter(delay):
	}

	next := min(time.Duration(float64(base)*backoffMultiplier), m.backoffMax)
	return next, true
}

func (m *xdsPolicyManager) sendACK(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesClient, node *corev3.Node, versionInfo, nonce string) error {
	m.setLastAppliedVersion(versionInfo)

	return stream.Send(&discoveryv3.DiscoveryRequest{
		Node:          node,
		TypeUrl:       xdsPolicyTypeURL,
		VersionInfo:   versionInfo,
		ResponseNonce: nonce,
	})
}

// sendNACK rejects a response, reporting the version the collector is still
// running so the control plane knows the update did not take effect.
func (m *xdsPolicyManager) sendNACK(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesClient, node *corev3.Node, nonce string, cause error) {
	err := stream.Send(&discoveryv3.DiscoveryRequest{
		Node:          node,
		TypeUrl:       xdsPolicyTypeURL,
		VersionInfo:   m.LastAppliedVersion(),
		ResponseNonce: nonce,
		ErrorDetail: &status.Status{
			Code:    int32(codes.InvalidArgument),
			Message: cause.Error(),
		},
	})
	if err != nil {
		m.logger.Error("Failed to send xDS NACK", zap.Error(err))
	}
}

// extractRawPolicies converts the resources of a DiscoveryResponse into raw
// policy maps for MakePolicySet.
//
// Every resource that cannot be decoded is reported: a malformed or unknown
// resource means the control plane sent something this collector cannot honor,
// and the caller NACKs the whole response rather than silently applying a
// partial policy set. Successfully decoded policies are still returned alongside
// the error for logging and debugging.
func extractRawPolicies(resp *discoveryv3.DiscoveryResponse) ([]map[string]any, error) {
	var (
		rawPolicies []map[string]any
		errs        []error
	)

	for i, anyRes := range resp.GetResources() {
		// The expected shape: a TelemetryCollector carrying a list of policies.
		if anyRes.MessageIs(&xdsv1alpha1.TelemetryCollector{}) {
			collector := &xdsv1alpha1.TelemetryCollector{}
			if err := anyRes.UnmarshalTo(collector); err != nil {
				errs = append(errs, fmt.Errorf("%w at index %d: %w", ErrXDSResourceDecode, i, err))
				continue
			}

			policies, err := policiesFromCollector(collector)
			if err != nil {
				errs = append(errs, fmt.Errorf("resource at index %d: %w", i, err))
			}
			rawPolicies = append(rawPolicies, policies...)
			continue
		}

		// Fallback: a resource that is a bare policy message rather than a
		// TelemetryCollector wrapper.
		raw, err := rawFromAny(anyRes)
		if err != nil {
			errs = append(errs, fmt.Errorf("resource at index %d: %w", i, err))
			continue
		}
		if _, ok := raw["type"]; !ok {
			errs = append(errs, fmt.Errorf("%w: resource at index %d of type %q has no 'type' field", ErrXDSResourceNotPolicy, i, anyRes.GetTypeUrl()))
			continue
		}
		rawPolicies = append(rawPolicies, raw)
	}

	return rawPolicies, errors.Join(errs...)
}

// policiesFromCollector decodes every policy carried by a TelemetryCollector,
// returning those it could decode along with a joined error for those it could not.
func policiesFromCollector(collector *xdsv1alpha1.TelemetryCollector) ([]map[string]any, error) {
	var (
		rawPolicies []map[string]any
		errs        []error
	)

	for i, policyAny := range collector.GetPolicies() {
		if policyAny == nil || policyAny.GetTypeUrl() == "" {
			errs = append(errs, fmt.Errorf("%w: policy at index %d", ErrXDSPolicyMissingBody, i))
			continue
		}
		typeURL := policyAny.GetTypeUrl()

		raw, err := rawFromAny(policyAny)
		if err != nil {
			errs = append(errs, fmt.Errorf("policy at index %d (%q): %w", i, typeURL, err))
			continue
		}

		// Policies now arrive as a bare google.protobuf.Any rather than being
		// wrapped in an envoy TypedExtensionConfig, so the type URL is the only
		// identity carried outside the body. There is no enclosing name to
		// backfill any more -- a driver that needs one reads it from the body
		// (the filter policies use their own "id" field).
		if _, ok := raw["type"]; !ok {
			policyType := policyTypeFromTypeURL(typeURL)
			if policyType == "" {
				errs = append(errs, fmt.Errorf("%w: policy at index %d has no 'type' field and its type URL %q is unusable",
					ErrXDSResourceNotPolicy, i, typeURL))
				continue
			}
			raw["type"] = policyType
		}

		rawPolicies = append(rawPolicies, raw)
	}

	return rawPolicies, errors.Join(errs...)
}

// rawFromAny decodes a protobuf Any into the generic map representation that the
// policy drivers unmarshal from.
func rawFromAny(msgAny *anypb.Any) (map[string]any, error) {
	typeURL := msgAny.GetTypeUrl()

	// Resolves against the global proto registry, so this fails for a type the
	// collector was not built with.
	msg, err := msgAny.UnmarshalNew()
	if err != nil {
		return nil, fmt.Errorf("%w: unknown or unregistered type URL %q: %w", ErrXDSPolicyDecode, typeURL, err)
	}

	b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("%w: type URL %q: %w", ErrXDSPolicyDecode, typeURL, err)
	}

	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%w: type URL %q is not a JSON object: %w", ErrXDSPolicyDecode, typeURL, err)
	}
	if raw == nil {
		return nil, fmt.Errorf("%w: type URL %q decoded to null", ErrXDSPolicyDecode, typeURL)
	}

	return raw, nil
}

// policyTypeFromTypeURL derives a policy type from a protobuf type URL by taking
// the last segment of the message's full name, e.g.
// "type.googleapis.com/google.telemetry.xds.v1alpha1.LogFilter" -> "LogFilter".
// It returns "" when no usable segment can be found.
func policyTypeFromTypeURL(typeURL string) string {
	name := typeURL
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, "."); idx != -1 {
		name = name[idx+1:]
	}
	return name
}

func (m *xdsPolicyManager) dial(ctx context.Context) (*grpc.ClientConn, error) {
	// Without keepalive, gRPC only notices a dead connection when the peer
	// actively closes it. An ADS stream is idle for long stretches by nature, so
	// a silently dropped connection (NAT eviction, LB idle reaping, a black-holed
	// route) would leave Recv blocked indefinitely and the reconnect loop below
	// would never run. These pings bound that detection at Time+Timeout.
	dialOpts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    keepaliveTime,
			Timeout: keepaliveTimeout,
			// Pings are only needed while the ADS stream is open, which is
			// exactly when a hang would go unnoticed. Leaving this false also
			// keeps us within the default server enforcement policy, which
			// counts pings sent with no active stream as a violation.
			PermitWithoutStream: false,
		}),
	}

	if m.insecure {
		// No credentials are attached in this mode: TokenAuth requires transport
		// security, so bearer tokens are never sent over a plaintext connection.
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tokenSource, err := ResolveTokenSource(ctx, m.serverAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve credentials: %w", err)
		}

		tlsCreds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(tlsCreds))
		if tokenSource != nil {
			dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(&TokenAuth{TS: tokenSource}))
		}
	}

	dialOpts = append(dialOpts, m.extraDialOpts...)

	// This dial is lazy: it sets up the ClientConn but does not wait for the
	// connection to be established, so failures surface on the stream instead
	// and are handled by the reconnect loop.
	return grpc.NewClient(m.serverAddr, dialOpts...)
}

// ResolveTokenSource returns a TokenSource providing Google OIDC ID tokens.
// It supports:
//  1. GCE / GKE / Service Accounts: Uses idtoken.NewTokenSource (queries VM metadata server or SA key).
//  2. Cloudtop / Developer Workstations: idtoken fails on "authorized_user" credentials, so it
//     falls back to ADC with OpenID scopes and extracts the ID token via GoogleIDTokenSource.
func ResolveTokenSource(ctx context.Context, serverAddr string) (oauth2.TokenSource, error) {
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		host = serverAddr
	}
	audience := "https://" + host

	// 1. GCE / GKE / Service Accounts: metadata server or SA key
	if idTS, err := idtoken.NewTokenSource(ctx, audience); err == nil {
		return idTS, nil
	}

	// 2. Cloudtop / Developer Workstation: ADC user credentials fallback
	defTS, err := google.DefaultTokenSource(ctx, "openid", "email")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ADC: %w", err)
	}

	wrapped := &GoogleIDTokenSource{Src: defTS}
	initialTok, err := wrapped.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve initial ID token: %w", err)
	}
	return oauth2.ReuseTokenSource(initialTok, wrapped), nil
}

// GoogleIDTokenSource extracts the OIDC ID token from OAuth2 credentials
// (where it is stored in tok.Extra("id_token")) so tok.AccessToken contains the ID token.
type GoogleIDTokenSource struct {
	Src oauth2.TokenSource
}

func (s *GoogleIDTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.Src.Token()
	if err != nil {
		return nil, err
	}
	idTok, ok := tok.Extra("id_token").(string)
	if !ok || idTok == "" {
		return nil, errors.New("no id_token found in ADC credentials; please run 'gcloud auth application-default login'")
	}
	return &oauth2.Token{
		AccessToken: idTok,
		TokenType:   "Bearer",
		Expiry:      tok.Expiry,
	}, nil
}

// TokenAuth adapts an oauth2.TokenSource to gRPC's credentials.PerRPCCredentials interface,
// injecting the Authorization: Bearer <id_token> header into each outgoing gRPC request.
type TokenAuth struct {
	TS oauth2.TokenSource
}

var _ credentials.PerRPCCredentials = (*TokenAuth)(nil)

func (a *TokenAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	tok, err := a.TS.Token()
	if err != nil {
		return nil, err
	}
	return map[string]string{"authorization": "Bearer " + tok.AccessToken}, nil
}

// RequireTransportSecurity satisfies the credentials.PerRPCCredentials interface.
// It returns true so that gRPC refuses to send the bearer token over a
// connection that is not protected by transport security.
func (a *TokenAuth) RequireTransportSecurity() bool {
	return true
}
