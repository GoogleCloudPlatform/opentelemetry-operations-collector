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
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/grpctransport"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/xdsclient"
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
	//	googlecontrolplane:xds://telemetrydirector.googleapis.com?gcp.fleet_id=FLEET&project=PROJECT
	//
	// It is spelled the same as the gcp.fleet_id resource attribute the fleet
	// ends up on, so the URI, the xDS node cluster and the collector's own
	// telemetry all name it identically.
	//
	// Both this manager and the googlecontrolplane provider read the fleet
	// through this one constant. If the two ever diverge, a single URI would
	// subscribe to one fleet's policies while attributing the collector's own
	// telemetry to another -- or, because a missing fleet ID is fatal for self
	// metrics, fail to start at all.
	FleetIDQueryParam = "gcp.fleet_id"

	// fleetIDEnvVar is the environment variable consulted for the fleet ID. It
	// is consulted before the URI, matching the provider's resolution order.
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
	keepaliveTime    = 60 * time.Second
	keepaliveTimeout = 20 * time.Second

	// defaultInitialSyncTimeout bounds how long Start blocks waiting for the
	// control plane's first response.
	defaultInitialSyncTimeout = 5 * time.Second
)

var (
	// Configuration errors, returned by NewXDSPolicyManager.
	ErrXDSMissingServerAddr   = errors.New("xDS server address cannot be empty in URI")
	ErrXDSMissingCollectorID  = errors.New("a collector ID is required for the xDS policy manager")
	ErrXDSMissingFleetID      = errors.New("a fleet ID is required for the xDS policy manager: set the FLEET_ID environment variable or the 'gcp.fleet_id' URI query parameter")
	ErrXDSInvalidInsecureFlag = errors.New("invalid 'insecure' URI query parameter, expected a boolean")

	// Lifecycle errors.
	ErrXDSAlreadyStarted = errors.New("xDS policy manager is already started")

	// Resource decoding errors, reported back to the control plane via NACK.
	ErrXDSResourceDecode    = errors.New("failed to decode xDS resource")
	ErrXDSPolicyDecode      = errors.New("failed to decode policy from xDS resource")
	ErrXDSPolicyMissingBody = errors.New("policy in xDS resource has no typed_config")
)

var _ Manager = (*xdsPolicyManager)(nil)

// XDSPolicyManagerOption configures optional settings on xdsPolicyManager.
type XDSPolicyManagerOption func(*xdsPolicyManager)

// WithMeterProvider configures the OpenTelemetry MeterProvider used to record
// grpc.xds_client.* self-observability metrics. If unset, otel.GetMeterProvider()
// is used.
func WithMeterProvider(mp metric.MeterProvider) XDSPolicyManagerOption {
	return func(m *xdsPolicyManager) {
		m.meterProvider = mp
	}
}

// xdsPolicyManager connects to an xDS control plane using grpc-go's generic
// xdsclient.XDSClient, parses received policies, updates ActivePolicySet, and
// sends ACKs/NACKs over the ADS stream.
type xdsPolicyManager struct {
	logger        *zap.Logger
	meterProvider metric.MeterProvider

	// Connection settings, resolved once in the constructor and never written
	// again. Being immutable, they are read from the stream goroutine without
	// holding mu.
	uri         *url.URL
	serverAddr  string
	collectorID string
	fleetID     string
	insecure    bool

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

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}

	ready       chan struct{}
	readyClosed bool

	lastAppliedVersion  string
	lastAppliedRawBytes []byte
	activeClient        *xdsclient.XDSClient
}

// NewXDSPolicyManager creates a manager for the given `xds://` URI.
func NewXDSPolicyManager(logger *zap.Logger, uri *url.URL, collectorID string, opts ...XDSPolicyManagerOption) (Manager, error) {
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

	fleetID := os.Getenv(fleetIDEnvVar)
	if fleetID == "" {
		fleetID = FleetIDFromURI(uri)
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

	m := &xdsPolicyManager{
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
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	return m, nil
}

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
func FleetIDFromURI(uri *url.URL) string {
	if uri == nil {
		return ""
	}
	return uri.Query().Get(FleetIDQueryParam)
}

// Start launches the background xdsclient stream and waits, briefly, for the
// control plane to answer once.
func (m *xdsPolicyManager) Start() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return ErrXDSAlreadyStarted
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.ready = make(chan struct{})
	m.readyClosed = false
	ready := m.ready
	timeout := m.initialSyncTimeout
	done := make(chan struct{})
	m.done = done
	m.mu.Unlock()

	m.logger.Info("Starting xDS policy manager",
		zap.String("server", m.serverAddr),
		zap.String("fleet", m.fleetID),
		zap.String("collector_id", m.collectorID),
		zap.String("type_url", xdsPolicyTypeURL),
	)

	go func() {
		defer close(done)
		defer m.finishRun(done)
		m.run(ctx, cancel)
	}()

	select {
	case <-ready:
	case <-m.timeAfter(timeout):
		m.logger.Warn("Timed out waiting for the initial xDS policy set; starting on built-in policies",
			zap.String("server", m.serverAddr),
			zap.Duration("timeout", timeout),
		)
	}

	return nil
}

func (m *xdsPolicyManager) markReady() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ready != nil && !m.readyClosed {
		m.readyClosed = true
		close(m.ready)
	}
}

func (m *xdsPolicyManager) finishRun(done chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.done == done {
		m.cancel = nil
		m.done = nil
	}
}

// Stop terminates the background stream loop and waits for it to exit.
func (m *xdsPolicyManager) Stop() error {
	m.mu.Lock()
	cancel := m.cancel
	done := m.done
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	return nil
}

// URI returns the configured URI.
func (m *xdsPolicyManager) URI() *url.URL {
	return m.uri
}

// PolicyEvaluationResult satisfies the Manager interface.
func (m *xdsPolicyManager) PolicyEvaluationResult(string, error) {}

// DumpResources returns the current CSDS ClientStatusResponse from the active
// xDSClient, or an error if the manager is not currently running.
func (m *xdsPolicyManager) DumpResources() (*v3statuspb.ClientStatusResponse, error) {
	m.mu.Lock()
	c := m.activeClient
	m.mu.Unlock()
	if c == nil {
		return nil, errors.New("xDS client is not running")
	}
	raw, err := c.DumpResources()
	if err != nil {
		return nil, err
	}
	resp := &v3statuspb.ClientStatusResponse{}
	if err := proto.Unmarshal(raw, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (m *xdsPolicyManager) setClient(c *xdsclient.XDSClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeClient = c
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

func (m *xdsPolicyManager) setLastAppliedState(versionInfo string, rawBytes []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastAppliedVersion = versionInfo
	m.lastAppliedRawBytes = rawBytes
}

func (m *xdsPolicyManager) getLastAppliedRawBytes() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastAppliedRawBytes
}

func (m *xdsPolicyManager) backoffDelay(retries int) time.Duration {
	m.mu.Lock()
	base := m.backoffInitial
	maxDelay := m.backoffMax
	m.mu.Unlock()
	if base <= 0 {
		base = defaultBackoffInitial
	}
	if maxDelay <= 0 {
		maxDelay = defaultBackoffMax
	}
	for i := 0; i < retries && base < maxDelay; i++ {
		base = min(time.Duration(float64(base)*backoffMultiplier), maxDelay)
	}
	if base > maxDelay {
		base = maxDelay
	}
	jitter := 1 + backoffJitterFraction*(2*rand.Float64()-1)
	return time.Duration(float64(base) * jitter)
}

func (m *xdsPolicyManager) run(ctx context.Context, cancel context.CancelFunc) {
	defer m.markReady()

	metricsReporter := newOTelMetricsReporter(m.meterProvider, m.serverAddr)
	defer metricsReporter.close()

	tb := &policyTransportBuilder{
		m:      m,
		mCtx:   ctx,
		cancel: cancel,
	}

	rType := xdsclient.ResourceType{
		TypeURL:                    xdsPolicyTypeURL,
		TypeName:                   "TelemetryCollector",
		AllResourcesRequiredInSotW: true,
		Decoder:                    &telemetryCollectorDecoder{m: m},
		InitialVersion:             m.LastAppliedVersion(),
	}

	cfg := xdsclient.Config{
		Servers: []xdsclient.ServerConfig{{
			ServerIdentifier: clients.ServerIdentifier{ServerURI: m.serverAddr},
		}},
		Node: clients.Node{
			ID:       m.collectorID,
			Cluster:  m.fleetID,
			Locality: clients.Locality{Region: defaultRegion},
		},
		TransportBuilder: tb,
		ResourceTypes: map[string]xdsclient.ResourceType{
			xdsPolicyTypeURL: rType,
		},
		MetricsReporter: metricsReporter,
		Logger:          newZapDepthLogger(m.logger),
		Backoff:         m.backoffDelay,
		TimeAfter:       m.timeAfter,
		Target:          m.serverAddr,
	}

	client, err := xdsclient.New(cfg)
	if err != nil {
		m.logger.Error("Failed to initialize xDS client", zap.Error(err))
		return
	}
	m.setClient(client)
	defer func() {
		m.setClient(nil)
		client.Close()
	}()

	cancelWatch := client.WatchResource(xdsPolicyTypeURL, "", &policyResourceWatcher{})
	defer cancelWatch()

	<-ctx.Done()
}

// policyResourceWatcher implements xdsclient.ResourceWatcher. Because SotW
// policy activation happens synchronously inside telemetryCollectorDecoder.DecodeAll
// prior to sending the ACK/NACK on the wire, this watcher's role is to release
// adsFlowControl by invoking done() on every callback.
type policyResourceWatcher struct{}

func (*policyResourceWatcher) ResourceChanged(_ xdsclient.ResourceData, done func()) {
	done()
}

func (*policyResourceWatcher) ResourceError(_ error, done func()) {
	done()
}

func (*policyResourceWatcher) AmbientError(_ error, done func()) {
	done()
}

type policyResourceData struct {
	version  string
	rawBytes []byte
}

func (p *policyResourceData) Equal(other xdsclient.ResourceData) bool {
	o, ok := other.(*policyResourceData)
	if !ok {
		return false
	}
	return p.version == o.version
}

func (p *policyResourceData) Bytes() []byte {
	return p.rawBytes
}

// telemetryCollectorDecoder implements xdsclient.Decoder and xdsclient.BatchDecoder.
type telemetryCollectorDecoder struct {
	m *xdsPolicyManager
}

var _ xdsclient.Decoder = (*telemetryCollectorDecoder)(nil)
var _ xdsclient.BatchDecoder = (*telemetryCollectorDecoder)(nil)

func (d *telemetryCollectorDecoder) Decode(resource *xdsclient.AnyProto, options xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	return d.DecodeAll([]*xdsclient.AnyProto{resource}, options)
}

func (d *telemetryCollectorDecoder) DecodeAll(resources []*xdsclient.AnyProto, options xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	anyResources := make([]*anypb.Any, len(resources))
	for i, r := range resources {
		anyResources[i] = r.ToAny()
	}

	d.m.logger.Info("Received xDS DiscoveryResponse",
		zap.String("version", options.Version),
		zap.Int("resources", len(anyResources)),
	)

	if version := options.Version; version != "" && version == d.m.LastAppliedVersion() {
		d.m.logger.Debug("Re-ACKing an already applied xDS revision", zap.String("version", version))
		d.m.markReady()
		return &xdsclient.DecodeResult{
			Name: "",
			Resource: &policyResourceData{
				version:  version,
				rawBytes: d.m.getLastAppliedRawBytes(),
			},
		}, nil
	}

	policyProtos, extractErr := extractPolicyProtosFromAnys(anyResources)
	if extractErr != nil {
		d.m.logger.Warn("Some xDS resources could not be decoded and will be skipped",
			zap.String("version", options.Version),
			zap.Error(extractErr),
		)
	}

	policySet, makeErr := MakePolicySetFromProtos(options.Version, policyProtos)
	if makeErr != nil {
		d.m.logger.Warn("Some policies in the xDS revision could not be loaded and will be skipped",
			zap.String("version", options.Version),
			zap.Error(makeErr),
		)
	}

	if err := errors.Join(extractErr, makeErr); err != nil && len(policySet.Policies) == 0 {
		d.m.logger.Warn("xDS revision contains no usable policies, sending NACK",
			zap.String("version", options.Version),
			zap.Error(err),
		)
		return nil, err
	}

	if active := ActivePolicySet(); len(policySet.Policies) == 0 && active != nil && len(active.Policies) > 0 {
		d.m.logger.Warn("xDS revision contains no policies, clearing the active policy set",
			zap.String("version", options.Version),
		)
	}

	var rawBytes []byte
	if len(anyResources) > 0 && anyResources[0] != nil {
		rawBytes = anyResources[0].GetValue()
	}

	SetActivePolicySet(policySet)
	d.m.setLastAppliedState(options.Version, rawBytes)
	d.m.markReady()

	return &xdsclient.DecodeResult{
		Name: "",
		Resource: &policyResourceData{
			version:  options.Version,
			rawBytes: rawBytes,
		},
	}, nil
}

// policyTransportBuilder implements clients.TransportBuilder with lazy dialing
// and synchronous terminal auth error interception.
type policyTransportBuilder struct {
	m      *xdsPolicyManager
	mCtx   context.Context
	cancel context.CancelFunc
}

func (b *policyTransportBuilder) Build(_ clients.ServerIdentifier) (clients.Transport, error) {
	return &policyTransport{
		m:      b.m,
		mCtx:   b.mCtx,
		cancel: b.cancel,
	}, nil
}

type policyTransport struct {
	m                  *xdsPolicyManager
	mCtx               context.Context
	cancel             context.CancelFunc
	terminalAuthFailed atomic.Bool

	mu sync.Mutex
	cc *grpc.ClientConn
}

func (pt *policyTransport) abortOnTerminalAuth(err error) {
	if pt.terminalAuthFailed.CompareAndSwap(false, true) {
		pt.m.logger.Error("xDS control plane rejected this collector's credentials; giving up on remote policies and continuing on built-in policies",
			zap.String("server", pt.m.serverAddr),
			zap.String("fleet", pt.m.fleetID),
			zap.String("collector_id", pt.m.collectorID),
			zap.Error(err),
		)
		pt.m.markReady()
		pt.cancel()
	}
}

func (pt *policyTransport) NewStream(streamCtx context.Context, method string) (clients.Stream, error) {
	if pt.mCtx.Err() != nil || pt.terminalAuthFailed.Load() {
		return nil, context.Canceled
	}

	pt.mu.Lock()
	cc := pt.cc
	if cc == nil {
		var err error
		cc, err = pt.m.dial(pt.mCtx)
		if err != nil {
			pt.mu.Unlock()
			if isTerminalAuthError(err) {
				pt.abortOnTerminalAuth(err)
				return nil, err
			}
			pt.m.logger.Error("Failed to connect to xDS server, retrying",
				zap.String("server", pt.m.serverAddr),
				zap.Error(err),
			)
			return nil, err
		}
		pt.cc = cc
	}
	pt.mu.Unlock()

	combinedCtx, cancelStream := context.WithCancel(streamCtx)
	stopWatch := context.AfterFunc(pt.mCtx, cancelStream)

	s, err := cc.NewStream(combinedCtx, &grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, method)
	if err != nil {
		stopWatch()
		cancelStream()
		if isTerminalAuthError(err) {
			pt.abortOnTerminalAuth(err)
		}
		return nil, err
	}

	return &policyStream{
		inner:        grpctransport.NewClientStream(s),
		pt:           pt,
		stopWatch:    stopWatch,
		cancelStream: cancelStream,
	}, nil
}

func (pt *policyTransport) Close() {
	pt.mu.Lock()
	cc := pt.cc
	pt.cc = nil
	pt.mu.Unlock()
	if cc != nil {
		_ = cc.Close()
	}
}

type policyStream struct {
	inner        clients.Stream
	pt           *policyTransport
	stopWatch    func() bool
	cancelStream context.CancelFunc
}

func (ps *policyStream) Send(msg []byte) error {
	return ps.inner.Send(msg)
}

func (ps *policyStream) Recv() ([]byte, error) {
	msg, err := ps.inner.Recv()
	if err != nil {
		ps.stopWatch()
		ps.cancelStream()
		if isTerminalAuthError(err) {
			ps.pt.abortOnTerminalAuth(err)
		} else if ps.pt.mCtx.Err() == nil {
			ps.pt.m.logger.Warn("xDS stream closed, reconnecting",
				zap.String("server", ps.pt.m.serverAddr),
				zap.String("last_applied_version", ps.pt.m.LastAppliedVersion()),
				zap.Error(err),
			)
		}
		return nil, err
	}
	return msg, nil
}

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

// extractPolicyProtos decodes the resources of a DiscoveryResponse into the
// policy protos they carry, ready for MakePolicySetFromProtos.
func extractPolicyProtos(resp *discoveryv3.DiscoveryResponse) ([]proto.Message, error) {
	return extractPolicyProtosFromAnys(resp.GetResources())
}

func extractPolicyProtosFromAnys(resources []*anypb.Any) ([]proto.Message, error) {
	var (
		policies []proto.Message
		errs     []error
	)

	for i, anyRes := range resources {
		if anyRes.MessageIs(&xdsv1alpha1.TelemetryCollector{}) {
			collector := &xdsv1alpha1.TelemetryCollector{}
			if err := anyRes.UnmarshalTo(collector); err != nil {
				errs = append(errs, fmt.Errorf("%w at index %d: %w", ErrXDSResourceDecode, i, err))
				continue
			}

			collectorPolicies, err := policiesFromCollector(collector)
			if err != nil {
				errs = append(errs, fmt.Errorf("resource at index %d: %w", i, err))
			}
			policies = append(policies, collectorPolicies...)
			continue
		}

		msg, err := protoFromAny(anyRes)
		if err != nil {
			errs = append(errs, fmt.Errorf("resource at index %d: %w", i, err))
			continue
		}
		policies = append(policies, msg)
	}

	return policies, errors.Join(errs...)
}

func policiesFromCollector(collector *xdsv1alpha1.TelemetryCollector) ([]proto.Message, error) {
	var (
		policies []proto.Message
		errs     []error
	)

	for i, policyAny := range collector.GetPolicies() {
		if policyAny == nil || policyAny.GetTypeUrl() == "" {
			errs = append(errs, fmt.Errorf("%w: policy at index %d", ErrXDSPolicyMissingBody, i))
			continue
		}

		msg, err := protoFromAny(policyAny)
		if err != nil {
			errs = append(errs, fmt.Errorf("policy at index %d (%q): %w", i, policyAny.GetTypeUrl(), err))
			continue
		}

		policies = append(policies, msg)
	}

	return policies, errors.Join(errs...)
}

func protoFromAny(msgAny *anypb.Any) (proto.Message, error) {
	msg, err := msgAny.UnmarshalNew()
	if err != nil {
		return nil, fmt.Errorf("%w: unknown or unregistered type URL %q: %w", ErrXDSPolicyDecode, msgAny.GetTypeUrl(), err)
	}
	return msg, nil
}

func (m *xdsPolicyManager) dial(ctx context.Context) (*grpc.ClientConn, error) {
	dialOpts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                keepaliveTime,
			Timeout:             keepaliveTimeout,
			PermitWithoutStream: false,
		}),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpctransport.ByteCodec())),
	}

	if m.insecure {
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

	return grpc.NewClient(m.serverAddr, dialOpts...)
}

// ResolveTokenSource returns a TokenSource providing Google OIDC ID tokens.
func ResolveTokenSource(ctx context.Context, serverAddr string) (oauth2.TokenSource, error) {
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		host = serverAddr
	}
	audience := "https://" + host

	if idTS, err := idtoken.NewTokenSource(ctx, audience); err == nil {
		return idTS, nil
	}

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

// GoogleIDTokenSource extracts the OIDC ID token from OAuth2 credentials.
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

// TokenAuth adapts an oauth2.TokenSource to gRPC's credentials.PerRPCCredentials interface.
type TokenAuth struct {
	TS oauth2.TokenSource
}

var _ credentials.PerRPCCredentials = (*TokenAuth)(nil)

func (a *TokenAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	tok, err := a.TS.Token()
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Unavailable, "failed to obtain per-RPC auth token: %v", err)
	}
	return map[string]string{"authorization": "Bearer " + tok.AccessToken}, nil
}

func (a *TokenAuth) RequireTransportSecurity() bool {
	return true
}
