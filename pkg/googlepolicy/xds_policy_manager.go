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
	"bytes"
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
	"time"

	authcredentials "cloud.google.com/go/auth/credentials"
	"cloud.google.com/go/auth/credentials/idtoken"
	"cloud.google.com/go/auth/oauth2adapt"
	"cloud.google.com/go/compute/metadata"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/oauth2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/grpctransport"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/xdsclient"
)

const (
	// xdsResourceType is the fully qualified name of the TelemetryCollector
	// message: the resource type as it appears in an xdstp:// resource name.
	xdsResourceType = "google.telemetry.xds.v1alpha1.TelemetryCollector"

	// xdsPolicyTypeURL is the one resource type this manager subscribes to. It
	// is fixed rather than configurable: the control plane only ever serves
	// TelemetryCollector resources, and the decoding path below is written
	// against that schema, so a different type could be subscribed to but not
	// usefully interpreted.
	xdsPolicyTypeURL = "type.googleapis.com/" + xdsResourceType

	// defaultCollectorResource is the collector resource name suffix in the
	// xdstp:// subscription URI. The control plane organizes collector
	// configurations under /collectors/{name}. Currently all collectors
	// subscribe to "default"; future versions may target specific collector
	// profiles (e.g. OS or environment) or wildcard "*" discovery.
	defaultCollectorResource = "default"

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

	// ProjectQueryParam is the query parameter carrying the project that owns
	// the fleet. Like the fleet ID, it is read through this one constant by
	// both this manager, which names the project in the resource it subscribes
	// to, and the googlecontrolplane provider, which stamps it on the
	// collector's own telemetry as gcp.project_id (when it is an alphanumeric
	// project ID rather than a numeric project number).
	ProjectQueryParam = "project"

	// ProjectNumberQueryParam is the query parameter carrying the numeric
	// project number that owns the fleet. When set, it takes precedence over
	// ProjectQueryParam for the xdstp:// resource subscription while leaving
	// ProjectQueryParam available for the alphanumeric project ID.
	ProjectNumberQueryParam = "project_number"

	// RegionQueryParam is the query parameter carrying the GCP region used in
	// the xdstp:// authority and reported on the xDS node's locality.
	RegionQueryParam = "region"

	// fleetIDEnvVar is the environment variable consulted for the fleet ID. It
	// is consulted before the URI, matching the provider's resolution order.
	fleetIDEnvVar = "FLEET_ID"

	// Environment variables consulted for project number, project ID, and
	// region resolution.
	googleCloudProjectNumberEnvVar = "GOOGLE_CLOUD_PROJECT_NUMBER"
	projectNumberEnvVar            = "PROJECT_NUMBER"
	googleCloudProjectEnvVar       = "GOOGLE_CLOUD_PROJECT"
	cloudRegionEnvVar              = "CLOUD_REGION"
	googleCloudRegionEnvVar        = "GOOGLE_CLOUD_REGION"

	// defaultRegion is the fallback locality region reported to the control
	// plane and embedded in the xdstp:// authority when neither the URI, the
	// environment, nor GCE metadata supplies a region.
	defaultRegion = "asia-east1"

	// defaultMetadataTimeout bounds GCE metadata queries during manager
	// construction so an unresponsive metadata server cannot stall startup.
	defaultMetadataTimeout = 1500 * time.Millisecond

	// Reconnect backoff defaults. The stream is re-established after any error,
	// with the delay growing from defaultBackoffInitial up to defaultBackoffMax.
	defaultBackoffInitial = 1 * time.Second
	defaultBackoffMax     = 120 * time.Second
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

	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
	cloudRunHostSuffix = ".run.app"
)

var (
	// Configuration errors, returned by NewXDSPolicyManager.
	ErrXDSMissingServerAddr   = errors.New("xDS server address cannot be empty in URI")
	ErrXDSMissingCollectorID  = errors.New("a collector ID is required for the xDS policy manager")
	ErrXDSMissingFleetID      = errors.New("a fleet ID is required for the xDS policy manager: set the FLEET_ID environment variable or the 'gcp.fleet_id' URI query parameter")
	ErrXDSInvalidFleetID      = errors.New("invalid fleet ID: expected a short fleet ID or a full resource name 'projects/{project}/locations/{location}/fleets/{fleet_id}'")
	ErrXDSMissingProject      = errors.New("a project is required for the xDS policy manager: set the 'project_number' or 'project' URI query parameter, GOOGLE_CLOUD_PROJECT_NUMBER / GOOGLE_CLOUD_PROJECT environment variable, or run on GCE")
	ErrXDSInvalidInsecureFlag = errors.New("invalid 'insecure' URI query parameter, expected a boolean")

	// Lifecycle errors.
	ErrXDSAlreadyStarted = errors.New("xDS policy manager is already started")

	// Resource decoding errors, reported back to the control plane via NACK.
	ErrXDSResourceDecode    = errors.New("failed to decode xDS resource")
	ErrXDSPolicyDecode      = errors.New("failed to decode policy from xDS resource")
	ErrXDSPolicyMissingBody = errors.New("policy in xDS resource has no typed_config")
)

// gceMetadata abstracts GCE metadata server lookups so unit tests stay fast and
// hermetic even when run on a GCE host.
type gceMetadata interface {
	OnGCE() bool
	ProjectID(ctx context.Context) (string, error)
	NumericProjectID(ctx context.Context) (string, error)
	Zone(ctx context.Context) (string, error)
}

type realGCEMetadata struct{}

func (realGCEMetadata) OnGCE() bool { return metadata.OnGCE() }
func (realGCEMetadata) ProjectID(ctx context.Context) (string, error) {
	return metadata.ProjectIDWithContext(ctx)
}
func (realGCEMetadata) NumericProjectID(ctx context.Context) (string, error) {
	return metadata.NumericProjectIDWithContext(ctx)
}
func (realGCEMetadata) Zone(ctx context.Context) (string, error) {
	return metadata.ZoneWithContext(ctx)
}

// defaultGCEMetadata is the metadata source consulted by NewXDSPolicyManager.
// Tests replace it to stub or disable GCE metadata detection.
var defaultGCEMetadata gceMetadata = realGCEMetadata{}

var (
	_ Manager               = (*xdsPolicyManager)(nil)
	_ FleetMetadataProvider = (*xdsPolicyManager)(nil)
)

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
// xdsclient.Client, parses received policies, updates ActivePolicySet, and
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
	// fleetName is the full Fleet resource name
	// ("projects/{project}/locations/{location}/fleets/{fleet_id}") the
	// collector identifies itself with: it is sent as the xDS node ID and
	// reported as gcp.fleet_id on the collector's own telemetry. The xDS node
	// cluster and metadata keep using the short fleetID.
	fleetName     string
	projectID     string
	projectNumber string
	region        string
	zone          string
	insecure      bool

	// resourceName is the xdstp:// name of the fleet's TelemetryCollector
	// resource. Every DiscoveryRequest on the stream subscribes to it; see
	// xdstpResourceName.
	resourceName string

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
//
// # URI form
//
// In full, as written in the collector's config:
//
//	googlecontrolplane:xds://HOST[:PORT]?gcp.fleet_id=FLEET&project=PROJECT[&insecure=BOOL]
//
// for example:
//
//	googlecontrolplane:xds://telemetrydirector.googleapis.com:443?gcp.fleet_id=my-fleet&project=my-project
//	googlecontrolplane:xds://telemetrydirector.googleapis.com:443?gcp.fleet_id=projects/my-project/locations/us-central1/fleets/my-fleet
//	googlecontrolplane:xds://127.0.0.1:18000?gcp.fleet_id=my-fleet&project=my-project&insecure=true
//
// The `googlecontrolplane:` prefix selects the confmap provider and is stripped
// before the remainder reaches this constructor, so the uri argument here starts
// at `xds://`. The opaque spelling `xds:HOST:PORT` is accepted as well.
//
// # Components
//
//	HOST[:PORT]    - required. Address of the xDS control plane.
//	gcp.fleet_id   - required, unless $FLEET_ID is set; the environment wins over
//	                 the URI, matching how the provider resolves it. Either a
//	                 short fleet ID ("my-fleet") or a full Fleet resource name
//	                 ("projects/{project}/locations/{location}/fleets/{fleet_id}").
//	                 The short ID names the fleet in the subscribed resource (see
//	                 xdstpResourceName) and is sent as this node's xDS cluster.
//	                 The full name is sent as this node's xDS ID and exposed via
//	                 FleetID() for the provider to attribute the collector's own
//	                 telemetry: a full name is used verbatim, and its project and
//	                 location take precedence over the `project`, `project_number`
//	                 and `region` parameters and their environment variables; a
//	                 short ID is completed to
//	                 "projects/{project_number}/locations/{region}/fleets/{id}"
//	                 from the same project number and region as the subscription.
//	project_number - optional. Numeric project number for the xdstp:// resource
//	                 subscription. Can also be supplied via
//	                 $GOOGLE_CLOUD_PROJECT_NUMBER or $PROJECT_NUMBER, a numeric
//	                 `project` parameter, or GCE metadata detection.
//	project        - optional when `project_number` or GCE metadata is available.
//	                 Alphanumeric project ID (used by the provider to stamp
//	                 gcp.project_id on self metrics, and as fallback for the
//	                 xdstp:// subscription in non-GCE / test environments) or
//	                 numeric project number.
//	region         - optional. GCP region for the xdstp:// authority and node
//	                 locality; falls back to $CLOUD_REGION / $GOOGLE_CLOUD_REGION,
//	                 the GCE instance zone, and finally defaultRegion.
//	insecure       - optional bool, default false. False connects with TLS 1.2+ and
//	                 an ADC-derived ID token per RPC; true connects in plaintext with
//	                 no credentials, which is intended for local control planes.
//
// Unrecognized query parameters are ignored. The subscribed resource type is
// deliberately not configurable -- see xdsPolicyTypeURL.
//
// # Arguments
//
// collectorID identifies this collector instance in node.metadata["service.instance.id"].
// It is passed in rather than read from the environment because the confmap
// provider derives it, and that package already depends on this one.
//
// All configuration is validated here, so a manager that constructs
// successfully can always Start.
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

	// A full resource name supplies the short ID for the subscription and the
	// project and location for everything else. They are written into the
	// query so that resolveProjectAndLocality sees them in the slot they fit
	// and ahead of the environment, which is what makes the subscription, the
	// node locality and the reported name agree with the name as written.
	shortFleetID := fleetID
	var fleetLocation string
	fullName := false
	if p, l, f, ok := ParseFleetResourceName(fleetID); ok {
		shortFleetID, fleetLocation, fullName = f, l, true
		if IsNumericProject(p) {
			query.Set(ProjectNumberQueryParam, p)
		} else {
			query.Set(ProjectQueryParam, p)
		}
	} else if strings.Contains(fleetID, "/") {
		return nil, fmt.Errorf("%w: %q", ErrXDSInvalidFleetID, fleetID)
	}

	var isInsecure bool
	if raw := query.Get("insecure"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrXDSInvalidInsecureFlag, raw)
		}
		isInsecure = parsed
	}

	projectNumber, projectID, region, zone, err := resolveProjectAndLocality(query, fleetLocation)
	if err != nil {
		return nil, err
	}

	// A short ID is completed from the same project number and region that
	// name the subscribed resource, so every collector in a fleet reports the
	// same name however it learned its project. A full name is kept as
	// written. The number rather than the ID because it is the one project
	// field always resolved here; the readable ID is still reported as
	// gcp.project_id.
	fleetName := fleetID
	if !fullName {
		fleetName = FormatFleetResourceName(projectNumber, region, shortFleetID)
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	m := &xdsPolicyManager{
		logger:             logger,
		uri:                uri,
		serverAddr:         serverAddr,
		collectorID:        collectorID,
		fleetID:            shortFleetID,
		fleetName:          fleetName,
		projectID:          projectID,
		projectNumber:      projectNumber,
		region:             region,
		zone:               zone,
		insecure:           isInsecure,
		resourceName:       xdstpResourceName(region, projectNumber, shortFleetID),
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

// ParseFleetResourceName parses a full Fleet resource name of the form
// "projects/{project}/locations/{location}/fleets/{fleet_id}". It returns
// ok == false if s is not in exactly that form or any segment is empty.
func ParseFleetResourceName(s string) (project, location, fleetID string, ok bool) {
	parts := strings.Split(s, "/")
	if len(parts) != 6 || parts[0] != "projects" || parts[2] != "locations" || parts[4] != "fleets" {
		return "", "", "", false
	}
	if parts[1] == "" || parts[3] == "" || parts[5] == "" {
		return "", "", "", false
	}
	return parts[1], parts[3], parts[5], true
}

// FormatFleetResourceName formats a full Fleet resource name
// "projects/{project}/locations/{location}/fleets/{fleet_id}".
func FormatFleetResourceName(project, location, fleetID string) string {
	return fmt.Sprintf("projects/%s/locations/%s/fleets/%s", project, location, fleetID)
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
func FleetIDFromURI(uri *url.URL) string {
	if uri == nil {
		return ""
	}
	return uri.Query().Get(FleetIDQueryParam)
}

// resolveProjectAndLocality resolves the numeric project number (for the
// xdstp:// subscription), the alphanumeric project ID (for telemetry
// attribution), and the collector's region and zone from URI query parameters,
// environment variables, and GCE metadata.
//
// fleetLocation, when non-empty, is the location from a full Fleet resource
// name and is the region, ahead of everything else: the subscription must be
// made in the region the fleet lives in, not the one the VM happens to be in.
func resolveProjectAndLocality(query url.Values, fleetLocation string) (projectNumber, projectID, region, zone string, err error) {
	rawProjectNum := query.Get(ProjectNumberQueryParam)
	if rawProjectNum == "" {
		rawProjectNum = firstNonEmptyEnv(googleCloudProjectNumberEnvVar, projectNumberEnvVar)
	}

	rawProject := query.Get(ProjectQueryParam)
	if rawProject == "" {
		rawProject = os.Getenv(googleCloudProjectEnvVar)
	}

	if rawProjectNum != "" {
		projectNumber = rawProjectNum
	}
	if rawProject != "" {
		if IsNumericProject(rawProject) {
			if projectNumber == "" {
				projectNumber = rawProject
			}
		} else {
			projectID = rawProject
		}
	}

	region = fleetLocation
	if region == "" {
		region = firstNonEmptyEnv(cloudRegionEnvVar, googleCloudRegionEnvVar)
	}
	if region == "" {
		region = query.Get(RegionQueryParam)
	}

	if defaultGCEMetadata != nil && defaultGCEMetadata.OnGCE() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultMetadataTimeout)
		defer cancel()

		if metaZone, zErr := defaultGCEMetadata.Zone(ctx); zErr == nil && metaZone != "" {
			zone = cleanZone(metaZone)
			if region == "" {
				region = regionFromZone(zone)
			}
		}

		if projectID == "" || projectNumber == "" {
			metaProjID, _ := defaultGCEMetadata.ProjectID(ctx)
			if projectID == "" && metaProjID != "" {
				projectID = metaProjID
			}
			if projectNumber == "" && (rawProject == "" || rawProject == metaProjID) {
				if metaProjNum, nErr := defaultGCEMetadata.NumericProjectID(ctx); nErr == nil && metaProjNum != "" {
					projectNumber = metaProjNum
				}
			}
		}
	}

	if projectNumber == "" && projectID != "" {
		projectNumber = projectID
	}
	if projectNumber == "" {
		return "", "", "", "", ErrXDSMissingProject
	}
	if region == "" {
		region = defaultRegion
	}
	return projectNumber, projectID, region, zone, nil
}

// IsNumericProject reports whether s is a non-empty string of ASCII digits
// (i.e. a GCP numeric project number rather than an alphanumeric project ID).
func IsNumericProject(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func firstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func cleanZone(z string) string {
	if i := strings.LastIndexByte(z, '/'); i >= 0 {
		return z[i+1:]
	}
	return z
}

// regionFromZone derives the GCP region from a zone name such as
// "asia-east1-a" -> "asia-east1".
func regionFromZone(zone string) string {
	zone = cleanZone(zone)
	if i := strings.LastIndexByte(zone, '-'); i > 0 && strings.Contains(zone[:i], "-") {
		return zone[:i]
	}
	return ""
}

// xdstpResourceName returns the xdstp:// name of the TelemetryCollector
// resource that holds a fleet's policies:
//
//	xdstp://telemetry-director.REGION.xds.googleapis.com/google.telemetry.xds.v1alpha1.TelemetryCollector/projects/PROJECT/fleets/FLEET/collectors/default
func xdstpResourceName(region, project, fleetID string) string {
	return fmt.Sprintf("xdstp://telemetry-director.%s.xds.googleapis.com/%s/projects/%s/fleets/%s/collectors/%s",
		region, xdsResourceType, project, fleetID, defaultCollectorResource)
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
		zap.String("resource_name", m.resourceName),
		zap.String("type_url", xdsPolicyTypeURL),
	)

	go func() {
		defer close(done)
		defer m.finishRun(done)
		m.run(ctx)
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

// FleetID returns the full Fleet resource name
// ("projects/{project}/locations/{location}/fleets/{fleet_id}") this
// collector reports itself under.
func (m *xdsPolicyManager) FleetID() string {
	return m.fleetName
}

// ProjectID returns the resolved alphanumeric GCP project ID, or "" if only a
// numeric project number was available.
func (m *xdsPolicyManager) ProjectID() string {
	return m.projectID
}

// PolicyEvaluationResult satisfies the Manager interface.
func (m *xdsPolicyManager) PolicyEvaluationResult(string, error) {}

// buildNode constructs the xDS Node descriptor sent on the initial DiscoveryRequest.
//
// Id carries the full Fleet resource name
// ("projects/{project}/locations/{location}/fleets/{fleet}") required by the
// API. It is the same fleetName the collector reports as gcp.fleet_id on its
// own telemetry, so the control plane and the telemetry backends see one
// identity for the fleet. Cluster and Metadata["gcp.fleet_id"] keep the short
// fleet ID for backwards compatibility. The collector instance ID is carried
// in Metadata["service.instance.id"]. Locality carries the resolved region and
// zone, and Metadata carries the OpenTelemetry resource attributes describing
// this collector instance.
func (m *xdsPolicyManager) buildNode() *corev3.Node {
	cn := m.buildClientNode()
	metaStruct, _ := cn.Metadata.(*structpb.Struct)
	return &corev3.Node{
		Id:      cn.ID,
		Cluster: cn.Cluster,
		Locality: &corev3.Locality{
			Region: cn.Locality.Region,
			Zone:   cn.Locality.Zone,
		},
		Metadata: metaStruct,
	}
}

func (m *xdsPolicyManager) buildClientNode() clients.Node {
	meta := map[string]any{
		"service.instance.id": m.collectorID,
		"gcp.fleet_id":        m.fleetID,
		"cloud.region":        m.region,
	}
	if m.projectID != "" {
		meta["gcp.project_id"] = m.projectID
	}
	if m.zone != "" {
		meta["cloud.availability_zone"] = m.zone
	}
	metaStruct, _ := structpb.NewStruct(meta)

	return clients.Node{
		ID:      m.fleetName,
		Cluster: m.fleetID,
		Locality: clients.Locality{
			Region: m.region,
			Zone:   m.zone,
		},
		Metadata: metaStruct,
	}
}

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

func (m *xdsPolicyManager) run(ctx context.Context) {
	defer m.markReady()

	metricsReporter := newOTelMetricsReporter(m.meterProvider, m.serverAddr)
	defer metricsReporter.close()

	var credsBundle credentials.Bundle
	if m.insecure {
		credsBundle = insecure.NewBundle()
	} else {
		credsBundle = &tlsCredentialsBundle{
			transportCreds: credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12}),
			perRPCCreds:    &TokenAuth{serverAddr: m.serverAddr},
		}
	}

	tb := grpctransport.NewBuilder(map[string]grpctransport.Config{
		"default": {
			Credentials: credsBundle,
			GRPCNewClient: func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
				opts = append(opts,
					grpc.WithKeepaliveParams(keepalive.ClientParameters{
						Time:                keepaliveTime,
						Timeout:             keepaliveTimeout,
						PermitWithoutStream: false,
					}),
					grpc.WithStreamInterceptor(m.wireLogStreamInterceptor),
				)
				opts = append(opts, m.extraDialOpts...)
				return grpc.NewClient(target, opts...)
			},
		},
	})

	serverCfg := xdsclient.ServerConfig{
		ServerIdentifier: clients.ServerIdentifier{
			ServerURI:  m.serverAddr,
			Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "default"},
		},
	}
	authorityName := fmt.Sprintf("telemetry-director.%s.xds.googleapis.com", m.region)

	rType := xdsclient.ResourceType{
		TypeURL:                    xdsPolicyTypeURL,
		TypeName:                   "TelemetryCollector",
		AllResourcesRequiredInSotW: true,
		Decoder:                    &telemetryCollectorDecoder{m: m},
	}

	cfg := xdsclient.Config{
		Servers: []xdsclient.ServerConfig{serverCfg},
		Authorities: map[string]xdsclient.Authority{
			authorityName: {
				XDSServers: []xdsclient.ServerConfig{serverCfg},
			},
		},
		Node:             m.buildClientNode(),
		TransportBuilder: tb,
		ResourceTypes: map[string]xdsclient.ResourceType{
			xdsPolicyTypeURL: rType,
		},
		MetricsReporter: metricsReporter,
		Logger:          newZapDepthLogger(m.logger),
		Backoff:         m.backoffDelay,
		TimeAfter:       m.timeAfter,
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

	cancelWatch := client.WatchResource(xdsPolicyTypeURL, m.resourceName, &policyResourceWatcher{m: m})
	defer cancelWatch()

	<-ctx.Done()
}

// policyResourceWatcher implements xdsclient.ResourceWatcher.
type policyResourceWatcher struct {
	m *xdsPolicyManager
}

func (w *policyResourceWatcher) ResourceChanged(data xdsclient.ResourceData, done func()) {
	defer done()
	d, ok := data.(*policyResourceData)
	if !ok || d == nil {
		return
	}
	if !d.skip && (d.version == "" || d.version != w.m.LastAppliedVersion()) {
		if active := ActivePolicySet(); len(d.policySet.Policies) == 0 && active != nil && len(active.Policies) > 0 {
			w.m.logger.Warn("xDS revision contains no policies, clearing the active policy set",
				zap.String("version", d.version),
			)
		}
		SetActivePolicySet(d.policySet)
		w.m.setLastAppliedState(d.version, d.rawBytes)
		if d.policySetRevision != "" {
			w.m.logger.Info("Applied xDS policy set",
				zap.String("version", d.version),
				zap.String("policy_set_revision", d.policySetRevision),
				zap.Int("policies_loaded", len(d.policySet.Policies)),
				zap.Int("policies_skipped", d.policiesSkipped),
			)
		}
	}
	w.m.markReady()
}

func (w *policyResourceWatcher) ResourceError(err error, done func()) {
	defer done()
	if err != nil && strings.Contains(err.Error(), "has been removed") {
		if active := ActivePolicySet(); active != nil {
			if len(active.Policies) > 0 {
				w.m.logger.Warn("xDS revision contains no policies, clearing the active policy set")
			}
			SetActivePolicySet(&PolicySet{
				Policies:   map[string]*PolicySetEntry{},
				ReceivedAt: time.Now(),
			})
		}
		w.m.setLastAppliedState("", nil)
	}
	if err != nil && (strings.Contains(err.Error(), "has been removed") || isAuthRejectionError(err)) {
		w.m.markReady()
	}
}

func isAuthRejectionError(err error) bool {
	switch grpcstatus.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, codes.Unauthenticated.String()) ||
		strings.Contains(msg, codes.PermissionDenied.String())
}

func (w *policyResourceWatcher) AmbientError(err error, done func()) {
	defer done()
	if err != nil {
		w.m.logger.Warn("xDS ambient error; retaining cached policy set", zap.Error(err))
	}
}

type policyResourceData struct {
	policySet         *PolicySet
	version           string
	policySetRevision string
	policiesSkipped   int
	rawBytes          []byte
	skip              bool
}

func (p *policyResourceData) Equal(other xdsclient.ResourceData) bool {
	o, ok := other.(*policyResourceData)
	if !ok {
		return false
	}
	return p.version == o.version && bytes.Equal(p.rawBytes, o.rawBytes)
}

func (p *policyResourceData) Bytes() []byte {
	return p.rawBytes
}

// telemetryCollectorDecoder implements xdsclient.Decoder.
type telemetryCollectorDecoder struct {
	m *xdsPolicyManager
}

var _ xdsclient.Decoder = (*telemetryCollectorDecoder)(nil)

func (d *telemetryCollectorDecoder) Decode(resource *xdsclient.AnyProto, options xdsclient.DecodeOptions) (*xdsclient.DecodeResult, error) {
	resourceName := d.m.resourceName
	rawAny := resource.ToAny()
	if rawAny.GetTypeUrl() == "type.googleapis.com/envoy.service.discovery.v3.Resource" {
		var r discoveryv3.Resource
		if err := proto.Unmarshal(rawAny.GetValue(), &r); err == nil {
			if r.GetName() != "" {
				resourceName = r.GetName()
			}
			if r.GetResource() != nil {
				rawAny = r.GetResource()
			}
		}
	}

	d.m.logger.Info("Received xDS TelemetryCollector resource",
		zap.String("version", options.Version),
		zap.String("type_url", rawAny.GetTypeUrl()),
	)

	if version := options.Version; version != "" && resourceName == d.m.resourceName && version == d.m.LastAppliedVersion() {
		d.m.logger.Debug("Re-ACKing an already applied xDS revision", zap.String("version", version))
		return &xdsclient.DecodeResult{
			Name: resourceName,
			Resource: &policyResourceData{
				policySet: ActivePolicySet(),
				version:   version,
				rawBytes:  d.m.getLastAppliedRawBytes(),
				skip:      true,
			},
		}, nil
	}

	// Each SotW DiscoveryResponse carries at most one TelemetryCollector resource
	// for this collector; individual policies live inside TelemetryCollector.Policies.
	resp := &discoveryv3.DiscoveryResponse{
		VersionInfo: options.Version,
		Resources:   []*anypb.Any{rawAny},
	}

	manifest, extractErr := extractManifest(resp)

	if (manifest.decodedCollectors > 0 || len(manifest.policies) > 0) && d.m.logger.Core().Enabled(zapcore.DebugLevel) {
		d.m.logger.Debug("Decoded xDS TelemetryCollector",
			zap.String("version", options.Version),
			zap.String("policy_set_revision", manifest.policySetRevision),
			zap.Time("revision_create_time", manifest.revisionCreateTime),
			zap.Int("policy_count", len(manifest.policies)),
			zap.Strings("policy_ids", policyIDsFromProtos(manifest.policies)),
		)
	}

	if extractErr != nil {
		d.m.logger.Warn("xDS revision could not be decoded, sending NACK",
			zap.String("version", options.Version),
			zap.Error(extractErr),
		)
		return &xdsclient.DecodeResult{Name: resourceName}, extractErr
	}

	// A TelemetryCollector with no policy_set_revision and no policies is the
	// control plane reporting that the fleet has no management-plane policy
	// source (e.g. no active rollout). Like a 0-resource response, that is a
	// legitimate, empty revision.
	if manifest.policySetRevision == "" && len(manifest.policies) == 0 {
		emptySet := &PolicySet{
			Policies:   make(map[string]*PolicySetEntry),
			RevisionID: options.Version,
			ReceivedAt: time.Now(),
		}
		return &xdsclient.DecodeResult{
			Name: resourceName,
			Resource: &policyResourceData{
				policySet: emptySet,
				version:   options.Version,
				rawBytes:  rawAny.GetValue(),
			},
		}, nil
	}

	if manifest.policySetRevision == "" {
		err := errors.New("xDS TelemetryCollector resource is missing required field policy_set_revision")
		d.m.logger.Warn("Rejecting xDS revision", zap.String("version", options.Version), zap.Error(err))
		return &xdsclient.DecodeResult{Name: resourceName}, err
	}

	policySetID, revisionID, ok := ParsePolicySetRevision(manifest.policySetRevision)
	if !ok {
		err := fmt.Errorf("xDS policy_set_revision %q is not in canonical format", manifest.policySetRevision)
		d.m.logger.Warn("Rejecting xDS revision", zap.String("version", options.Version), zap.Error(err))
		return &xdsclient.DecodeResult{Name: resourceName}, err
	}

	policySet, makeErr := MakePolicySetFromProtos(policySetID, revisionID, manifest.policies)
	if !manifest.revisionCreateTime.IsZero() {
		policySet.RevisionCreateTime = manifest.revisionCreateTime
	}
	if makeErr != nil {
		d.m.logger.Warn("xDS revision contains invalid policies, sending NACK",
			zap.String("version", options.Version),
			zap.Error(makeErr),
		)
		return &xdsclient.DecodeResult{Name: resourceName}, makeErr
	}

	rawBytes := marshalCollectorBytes(manifest.policySetRevision, manifest.policies)

	return &xdsclient.DecodeResult{
		Name: resourceName,
		Resource: &policyResourceData{
			policySet:         policySet,
			version:           options.Version,
			policySetRevision: manifest.policySetRevision,
			policiesSkipped:   manifest.skipped + len(policySet.FailedPolicies),
			rawBytes:          rawBytes,
		},
	}, nil
}

func marshalCollectorBytes(policySetRevision string, policyProtos []proto.Message) []byte {
	if len(policyProtos) == 0 && policySetRevision == "" {
		return nil
	}
	policies := make([]*anypb.Any, 0, len(policyProtos))
	for _, p := range policyProtos {
		if a, err := anypb.New(p); err == nil {
			policies = append(policies, a)
		}
	}
	rawBytes, _ := proto.Marshal(&xdsv1alpha1.TelemetryCollector{
		PolicySetRevision: policySetRevision,
		Policies:          policies,
	})
	return rawBytes
}

// handleResponse is used by unit tests to exercise the decode and watch
// pipeline directly with a synthetic DiscoveryResponse.
func (m *xdsPolicyManager) handleResponse(_ any, _ any, resp *discoveryv3.DiscoveryResponse) {
	decoder := &telemetryCollectorDecoder{m: m}
	watcher := &policyResourceWatcher{m: m}
	if len(resp.GetResources()) == 0 {
		watcher.ResourceError(errors.New("resource has been removed"), func() {})
		return
	}
	for _, r := range resp.GetResources() {
		res, err := decoder.Decode(xdsclient.NewAnyProto(r), xdsclient.DecodeOptions{
			Version: resp.GetVersionInfo(),
		})
		if err != nil {
			watcher.ResourceError(err, func() {})
			return
		}
		if res != nil && res.Resource != nil {
			watcher.ResourceChanged(res.Resource, func() {})
		}
	}
}

type wireLogClientStream struct {
	grpc.ClientStream
	m *xdsPolicyManager
}

func (s *wireLogClientStream) SendMsg(msg any) error {
	err := s.ClientStream.SendMsg(msg)
	if err == nil && s.m.logger != nil && s.m.logger.Core().Enabled(zapcore.DebugLevel) {
		if b, ok := msg.([]byte); ok {
			var req discoveryv3.DiscoveryRequest
			if uErr := proto.Unmarshal(b, &req); uErr == nil {
				s.m.logDiscoveryRequest(&req)
			}
		}
	}
	return err
}

func (s *wireLogClientStream) RecvMsg(msg any) error {
	err := s.ClientStream.RecvMsg(msg)
	if err == nil && s.m.logger != nil && s.m.logger.Core().Enabled(zapcore.DebugLevel) {
		if bp, ok := msg.(*[]byte); ok {
			var resp discoveryv3.DiscoveryResponse
			if uErr := proto.Unmarshal(*bp, &resp); uErr == nil {
				s.m.logDiscoveryResponse(&resp)
			}
		}
	}
	return err
}

func (m *xdsPolicyManager) wireLogStreamInterceptor(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	cs, err := streamer(ctx, desc, cc, method, opts...)
	if err != nil {
		return nil, err
	}
	return &wireLogClientStream{ClientStream: cs, m: m}, nil
}

func (m *xdsPolicyManager) logDiscoveryRequest(req *discoveryv3.DiscoveryRequest) {
	kind := "initial"
	switch {
	case req.GetErrorDetail() != nil:
		kind = "nack"
	case req.GetResponseNonce() != "":
		kind = "ack"
	}
	m.logDiscoveryRequestSent(kind, req)
}

func (m *xdsPolicyManager) logDiscoveryRequestSent(kind string, req *discoveryv3.DiscoveryRequest) {
	if m.logger != nil && m.logger.Core().Enabled(zapcore.DebugLevel) {
		m.logger.Debug("xDS DiscoveryRequest sent",
			zap.String("kind", kind),
			zap.String("version", req.GetVersionInfo()),
			zap.String("response_nonce", req.GetResponseNonce()),
			zap.String("discovery_request", formatDiscoveryRequestJSON(req)),
		)
	}
}

func (m *xdsPolicyManager) logDiscoveryResponse(resp *discoveryv3.DiscoveryResponse) {
	if m.logger != nil && m.logger.Core().Enabled(zapcore.DebugLevel) {
		m.logger.Debug("xDS DiscoveryResponse received",
			zap.String("version", resp.GetVersionInfo()),
			zap.String("nonce", resp.GetNonce()),
			zap.String("discovery_response", formatDiscoveryResponseJSON(resp)),
		)
	}
}

func formatDiscoveryRequestJSON(req *discoveryv3.DiscoveryRequest) string {
	if req == nil {
		return "{}"
	}
	b, err := (protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}).Marshal(req)
	if err != nil {
		return fmt.Sprintf("{\"marshal_error\":%q}", err.Error())
	}
	return string(b)
}

func formatDiscoveryResponseJSON(resp *discoveryv3.DiscoveryResponse) string {
	if resp == nil {
		return "{}"
	}
	b, err := (protojson.MarshalOptions{
		UseProtoNames: true,
	}).Marshal(resp)
	if err != nil {
		return fmt.Sprintf("{\"version_info\":%q,\"type_url\":%q,\"nonce\":%q,\"resource_count\":%d,\"marshal_error\":%q}",
			resp.GetVersionInfo(), resp.GetTypeUrl(), resp.GetNonce(), len(resp.GetResources()), err.Error())
	}
	return string(b)
}

func policyIDsFromProtos(msgs []proto.Message) []string {
	ids := make([]string, 0, len(msgs))
	for i, msg := range msgs {
		ids = append(ids, policyIDFromProto(i, msg))
	}
	return ids
}

func policyIDFromProto(i int, msg proto.Message) string {
	if msg == nil {
		return fmt.Sprintf("index_%d", i)
	}
	if idGetter, ok := msg.(interface{ GetId() string }); ok {
		if id := idGetter.GetId(); id != "" {
			return id
		}
	}
	if s, ok := msg.(*structpb.Struct); ok {
		if id := s.GetFields()["id"].GetStringValue(); id != "" {
			return id
		}
		if name := s.GetFields()["name"].GetStringValue(); name != "" {
			return name
		}
	}
	return fmt.Sprintf("index_%d", i)
}

// xdsManifest holds the decoded policies and management-plane PolicySet
// revision metadata extracted from an xDS DiscoveryResponse.
type xdsManifest struct {
	policies           []proto.Message
	policySetRevision  string
	revisionCreateTime time.Time
	skipped            int
	decodedCollectors  int
}

// extractManifest decodes the resources of a DiscoveryResponse into its
// policy protos and manifest-level revision metadata.
func extractManifest(resp *discoveryv3.DiscoveryResponse) (xdsManifest, error) {
	var (
		manifest xdsManifest
		errs     []error
	)

	for i, anyRes := range resp.GetResources() {
		if anyRes.MessageIs(&xdsv1alpha1.TelemetryCollector{}) {
			collector := &xdsv1alpha1.TelemetryCollector{}
			if err := anyRes.UnmarshalTo(collector); err != nil {
				manifest.skipped++
				errs = append(errs, fmt.Errorf("%w at index %d: %w", ErrXDSResourceDecode, i, err))
				continue
			}
			manifest.decodedCollectors++

			if rev := collector.GetPolicySetRevision(); rev != "" {
				manifest.policySetRevision = rev
			}
			if ct := collector.GetPolicySetRevisionCreateTime(); ct.IsValid() {
				manifest.revisionCreateTime = ct.AsTime()
			}

			collectorPolicies, skipped, err := policiesFromCollector(collector)
			manifest.skipped += skipped
			if err != nil {
				errs = append(errs, fmt.Errorf("resource at index %d: %w", i, err))
			}
			manifest.policies = append(manifest.policies, collectorPolicies...)
			continue
		}

		msg, err := protoFromAny(anyRes)
		if err != nil {
			manifest.skipped++
			errs = append(errs, fmt.Errorf("resource at index %d: %w", i, err))
			continue
		}
		manifest.policies = append(manifest.policies, msg)
	}

	return manifest, errors.Join(errs...)
}

func policiesFromCollector(collector *xdsv1alpha1.TelemetryCollector) ([]proto.Message, int, error) {
	var (
		policies []proto.Message
		skipped  int
		errs     []error
	)

	for i, policyAny := range collector.GetPolicies() {
		if policyAny == nil || policyAny.GetTypeUrl() == "" {
			skipped++
			errs = append(errs, fmt.Errorf("%w: policy at index %d", ErrXDSPolicyMissingBody, i))
			continue
		}

		msg, err := protoFromAny(policyAny)
		if err != nil {
			skipped++
			errs = append(errs, fmt.Errorf("policy at index %d (%q): %w", i, policyAny.GetTypeUrl(), err))
			continue
		}

		policies = append(policies, msg)
	}

	return policies, skipped, errors.Join(errs...)
}

func protoFromAny(msgAny *anypb.Any) (proto.Message, error) {
	msg, err := msgAny.UnmarshalNew()
	if err != nil {
		return nil, fmt.Errorf("%w: unknown or unregistered type URL %q: %w", ErrXDSPolicyDecode, msgAny.GetTypeUrl(), err)
	}
	return msg, nil
}

type tlsCredentialsBundle struct {
	transportCreds credentials.TransportCredentials
	perRPCCreds    credentials.PerRPCCredentials
}

var _ credentials.Bundle = (*tlsCredentialsBundle)(nil)

func (b *tlsCredentialsBundle) TransportCredentials() credentials.TransportCredentials {
	return b.transportCreds
}

func (b *tlsCredentialsBundle) PerRPCCredentials() credentials.PerRPCCredentials {
	return b.perRPCCreds
}

func (b *tlsCredentialsBundle) NewWithMode(string) (credentials.Bundle, error) {
	return b, nil
}

// ResolveTokenSource returns an OIDC ID token source for Cloud Run endpoints
// (*.run.app) and an OAuth2 access token source (cloud-platform scope) for all
// other endpoints.
func ResolveTokenSource(ctx context.Context, serverAddr string) (oauth2.TokenSource, error) {
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		host = serverAddr
	}

	if isCloudRunHost(host) {
		return resolveIDTokenSource(ctx, "https://"+host)
	}

	creds, err := authcredentials.DetectDefault(&authcredentials.DetectOptions{
		Scopes: []string{cloudPlatformScope},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve OAuth2 token source: %w", err)
	}
	return oauth2adapt.TokenSourceFromTokenProvider(creds), nil
}

func isCloudRunHost(host string) bool {
	normalized := strings.TrimSuffix(strings.ToLower(host), ".")
	return strings.HasSuffix(normalized, cloudRunHostSuffix)
}

// resolveIDTokenSource returns a TokenSource providing Google OIDC ID tokens
// for the given audience. It supports:
//  1. GCE / GKE / Service Accounts / WIF with service account impersonation: Uses idtoken.NewCredentials
//     (queries VM metadata server, SA key, or impersonates the WIF service account).
//  2. Cloudtop / Developer Workstations: idtoken fails on "authorized_user" credentials, so it
//     falls back to ADC with OpenID scopes and extracts the ID token via GoogleIDTokenSource.
func resolveIDTokenSource(_ context.Context, audience string) (oauth2.TokenSource, error) {
	// Detect ADC once and reuse it for both steps so the ID token identity
	// matches what ADC selected.
	creds, err := authcredentials.DetectDefault(&authcredentials.DetectOptions{
		Scopes: []string{"openid", "email"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ADC: %w", err)
	}

	// 1. GCE / GKE / Service Accounts / WIF with service account impersonation.
	// Pass the ADC JSON explicitly: without it idtoken skips the gcloud
	// well-known file and uses the metadata server identity on GCE.
	idCreds, idErr := idtoken.NewCredentials(&idtoken.Options{
		Audience:        audience,
		CredentialsJSON: creds.JSON(), //nolint:staticcheck // SA1019: JSON was already loaded by ADC (DetectDefault), not from an untrusted source.
	})
	if idErr == nil {
		// auth.Credentials caches and refreshes tokens early; an outer
		// oauth2.ReuseTokenSource would defeat that.
		return oauth2adapt.TokenSourceFromTokenProvider(idCreds), nil
	}

	// 2. Cloudtop / Developer Workstation: ADC user credentials fallback
	wrapped := &GoogleIDTokenSource{Src: oauth2adapt.TokenSourceFromTokenProvider(creds)}
	initialTok, err := wrapped.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve initial ID token: %w (idtoken: %v)", err, idErr)
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
	mu         sync.Mutex
	TS         oauth2.TokenSource
	serverAddr string
}

var _ credentials.PerRPCCredentials = (*TokenAuth)(nil)

func (a *TokenAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	a.mu.Lock()
	ts := a.TS
	if ts == nil && a.serverAddr != "" {
		var err error
		ts, err = ResolveTokenSource(ctx, a.serverAddr)
		if err != nil {
			a.mu.Unlock()
			return nil, grpcstatus.Errorf(codes.Unavailable, "failed to resolve credentials: %v", err)
		}
		a.TS = ts
	}
	a.mu.Unlock()

	if ts == nil {
		return nil, grpcstatus.Errorf(codes.Unavailable, "failed to obtain per-RPC auth token: nil token source")
	}

	tok, err := ts.Token()
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Unavailable, "failed to obtain per-RPC auth token: %v", err)
	}
	return map[string]string{"authorization": "Bearer " + tok.AccessToken}, nil
}

func (a *TokenAuth) RequireTransportSecurity() bool {
	return true
}
