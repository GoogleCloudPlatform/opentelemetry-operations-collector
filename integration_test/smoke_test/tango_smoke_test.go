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

//go:build integration_test

/*
Smoke test for Tango-GBOC: the packaged otelcol-google service with an
additional googlecontrolplane config source, talking to a live Control Plane
and Data Plane.

The test verifies that the collector receives the pre-provisioned PolicySet
revision from the Control Plane and reports it through its self-o11y gauge
gcp.policy.set.active, which is exported to the Data Plane and observed through
Cloud Monitoring. The gauge is only updated once the collector has applied (and
ACKed) a revision, so a point carrying the expected PolicySet and revision
proves the whole loop.

Once the PolicySet is active, the test writes JSON log lines to a file tailed
by a filelog receiver declared in the collector's config.yaml, and checks in
Cloud Logging that the PolicySet's log filter policies, enforced by the
googlepolicy processor, kept and dropped the expected lines. The receiver is
declared locally because the Control Plane does not yet translate source
policies into collector config.

The test assumes a long-lived fleet and PolicySet have been pre-provisioned in
the test project and rolled out (see tango/README.md). It never calls the
Telemetry Management API itself.

Environment variables (in addition to those required by TestSmoke):
TANGO_CP_ENDPOINT: host:port of the xDS Control Plane, e.g.
  staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443.
  The test is skipped when unset.
TANGO_EXPECTED_REVISION: revision ID of the pre-provisioned PolicySet that is
  rolled out to the fleet (e.g. 3f038775). Required.
TANGO_FLEET_ID: fleet the collector subscribes to (default gboc-smoke-fleet).
TANGO_POLICY_SET_ID: pre-created PolicySet ID (default gboc-smoke-ps).
TANGO_REGION: region of the fleet and Control Plane endpoint (default
  asia-east1). Passed explicitly so it does not depend on the VM's zone.
*/

package smoke

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/gce"
)

const (
	// policySetActiveMetric is the self-o11y gauge reporting the enforced
	// PolicySet revision. It reaches Cloud Monitoring through
	// telemetry.googleapis.com under the prometheus.googleapis.com namespace.
	policySetActiveMetric = "prometheus.googleapis.com/gcp.policy.set.active/gauge"
	// policySetIDLabel is the metric label carrying the enforced PolicySet
	// resource name.
	policySetIDLabel = "gcp.policy.set.id"

	// collectorUUIDNamespace is googlecontrolplaneprovider's namespace for
	// deriving the collector ID (service.instance.id) from the hostname.
	collectorUUIDNamespace = "5AFFCE1A-3D0A-4220-8EA1-7D64A842BEB4"

	// tangoDropInPath is a systemd drop-in that keeps the packaged service's
	// command line and appends the googlecontrolplane config source to it.
	tangoDropInPath = "/etc/systemd/system/otelcol-google.service.d/tango.conf"

	metricWindow = 30 * time.Minute
	// queryBackoffDuration mirrors gce's backoff between Monitoring queries.
	queryBackoffDuration = 10 * time.Second
	// queryMaxAttempts bounds how long the test waits for a point with the
	// expected revision once the series exists (about 3 minutes plus query time).
	queryMaxAttempts = 20

	// tangoLogPath is the file tailed by the filelog receiver declared in
	// tangoCollectorConfig. The test writes it only after the PolicySet is
	// active.
	tangoLogPath = "/var/log/gboc-smoke/test.log"
	// sampleLogsFile holds the JSON lines uploaded to tangoLogPath.
	sampleLogsFile = "tango/sample_logs.jsonl"
	// keptMessage is the MESSAGE of the sample log that the PolicySet keeps.
	keptMessage = "gboc-smoke=keep"
	logWindow   = 30 * time.Minute
)

// droppedMessages are the MESSAGEs of the sample logs that the PolicySet drops.
var droppedMessages = []string{
	// Dropped by the MESSAGE contains "gboc-smoke=drop" filter.
	"gboc-smoke=drop",
	// Dropped by the SYSLOG_IDENTIFIER != "gboc-smoke" filter.
	"gboc-smoke=other",
}

// tangoCollectorConfig replaces the packaged config.yaml. It is merged with the
// config from the googlecontrolplane config source.
//
// The Control Plane does not yet translate source policies into collector
// config, so the log source is declared here: a filelog receiver that parses
// each JSON line into a map body. The PolicySet's log filter policies match on
// body fields (SYSLOG_IDENTIFIER, MESSAGE), which only match map bodies.
//
// googlepolicy enforces the PolicySet activated by the googlecontrolplane
// config source, whichever config declares the processor. The other processors
// and the exporter are the fixed-name components that the googlecontrolplane
// config source always generates; the exporter sends to
// telemetry.googleapis.com.
const tangoCollectorConfig = `receivers:
  filelog/tango_smoke:
    include: [` + tangoLogPath + `]
    start_at: beginning
    operators:
      - type: json_parser
        parse_to: body
processors:
  googlepolicy:
service:
  pipelines:
    logs/tango_smoke:
      receivers: [filelog/tango_smoke]
      processors:
        - resourcedetection/googlecontrolplane
        - transform/googlecontrolplane
        - googlepolicy
        - queue_batch/default_gcp_destination_batch_logs
      exporters: [otlp_grpc/default_gcp_destination]
`

// tangoSettings holds the Tango-specific environment for the test.
type tangoSettings struct {
	cpEndpoint       string
	expectedRevision string
	fleetID          string
	policySetID      string
	region           string
}

func tangoSettingsFromEnv(t *testing.T) tangoSettings {
	t.Helper()
	s := tangoSettings{
		cpEndpoint:       os.Getenv("TANGO_CP_ENDPOINT"),
		expectedRevision: os.Getenv("TANGO_EXPECTED_REVISION"),
		fleetID:          envOrDefault("TANGO_FLEET_ID", "gboc-smoke-fleet"),
		policySetID:      envOrDefault("TANGO_POLICY_SET_ID", "gboc-smoke-ps"),
		region:           envOrDefault("TANGO_REGION", "asia-east1"),
	}
	if s.cpEndpoint == "" {
		t.Skip("TANGO_CP_ENDPOINT is not set; skipping Tango-GBOC smoke test")
	}
	if s.expectedRevision == "" {
		t.Fatal("TANGO_EXPECTED_REVISION must be set to the revision rolled out to the fleet")
	}
	if testRunID == "" {
		t.Fatal("This test does not support being run outside of Kokoro.")
	}
	return s
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// controlPlaneConfig returns the additional --config flag that loads policies
// from the Control Plane. Project and project number of the xDS subscription
// are taken from the GCE metadata server.
func (s tangoSettings) controlPlaneConfig() string {
	return fmt.Sprintf("--config=googlecontrolplane:xds://%s?gcp.fleet_id=%s&region=%s", s.cpEndpoint, s.fleetID, s.region)
}

// dropIn returns a systemd drop-in that reruns the packaged service's own
// command line (the regular file config from OTELCOL_OPTIONS) with the
// googlecontrolplane config source appended. Later sources win on conflicts.
func (s tangoSettings) dropIn() string {
	return fmt.Sprintf("[Service]\nExecStart=\nExecStart=/usr/bin/otelcol-google $OTELCOL_OPTIONS %s\n", s.controlPlaneConfig())
}

// setupTangoCollector installs otelcol-google with tangoCollectorConfig and
// restarts it with the googlecontrolplane config source added.
func setupTangoCollector(ctx context.Context, logger *log.Logger, vm *gce.VM, s tangoSettings) error {
	if err := installOtelCollector(ctx, logger, vm, locationFromEnvVars()); err != nil {
		return err
	}
	configPath := collectorConfigPath(vm.ImageSpec)
	if err := gce.UploadContent(ctx, logger, vm, strings.NewReader(tangoCollectorConfig), configPath); err != nil {
		return fmt.Errorf("failed to upload %s: %v", configPath, err)
	}
	if _, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("sudo mkdir -p %s %s", path.Dir(tangoDropInPath), path.Dir(tangoLogPath))); err != nil {
		return err
	}
	if err := gce.UploadContent(ctx, logger, vm, strings.NewReader(s.dropIn()), tangoDropInPath); err != nil {
		return fmt.Errorf("failed to upload %s: %v", tangoDropInPath, err)
	}
	if _, err := gce.RunRemotely(ctx, logger, vm, "sudo systemctl daemon-reload"); err != nil {
		return err
	}
	return restartOtelCollector(ctx, logger, vm)
}

// collectorID returns the service.instance.id the collector on vm reports,
// which googlecontrolplaneprovider derives from the hostname.
func collectorID(ctx context.Context, logger *log.Logger, vm *gce.VM) (string, error) {
	out, err := gce.RunRemotely(ctx, logger, vm, "hostname")
	if err != nil {
		return "", err
	}
	return uuid.NewSHA1(uuid.MustParse(collectorUUIDNamespace), []byte(strings.TrimSpace(out.Stdout))).String(), nil
}

// revisionValue mirrors googlecontrolplaneextension's parsing of an 8-hex
// CCFE revision ID into the gcp.policy.set.active gauge value.
func revisionValue(rev string) (int64, error) {
	v, err := strconv.ParseUint(rev, 16, 32)
	if err != nil || len(rev) != 8 {
		return 0, fmt.Errorf("revision %q is not an 8-hex revision ID", rev)
	}
	return int64(v), nil
}

// lookupCollectorMetric is gce.lookupMetric scoped to a collector instead of a
// VM. The collector's self-o11y metrics are ingested over OTLP as
// prometheus_target series whose instance label is the collector's
// service.instance.id; they carry neither the instance_id nor the
// "<vm.ID>/<vm.Name>" namespace that gce.lookupMetric filters on.
func lookupCollectorMetric(ctx context.Context, client *monitoring.MetricClient, vm *gce.VM, metric, collector string, window time.Duration) *monitoring.TimeSeriesIterator {
	now := time.Now()
	filter := fmt.Sprintf(`metric.type = %q AND resource.type = "prometheus_target" AND resource.labels.instance = %q`, metric, collector)
	return client.ListTimeSeries(ctx, &monitoringpb.ListTimeSeriesRequest{
		Name:   "projects/" + vm.Project,
		Filter: filter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(now.Add(-window)),
			EndTime:   timestamppb.New(now),
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	})
}

// nonEmptySeries is gce.nonEmptySeriesList with a minimum of one series: it
// returns the series that have points, or (nil, nil) if there are none.
func nonEmptySeries(logger *log.Logger, it *monitoring.TimeSeriesIterator) ([]*monitoringpb.TimeSeries, error) {
	var tsList []*monitoringpb.TimeSeries
	for {
		series, err := it.Next()
		if err == iterator.Done {
			return tsList, nil
		}
		if err != nil {
			return nil, err
		}
		logger.Printf("nonEmptySeries() found series %v", series)
		if len(series.GetPoints()) > 0 {
			tsList = append(tsList, series)
		}
	}
}

// isRetriableLookupError mirrors gce.isRetriableLookupError.
func isRetriableLookupError(ctx context.Context, err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	if st.Code() == codes.Canceled {
		return ctx.Err() == nil
	}
	return st.Code() == codes.NotFound || st.Code() == codes.Internal || st.Code() == codes.ResourceExhausted
}

// waitForCollectorMetricSeries is gce.WaitForMetricSeries using
// lookupCollectorMetric. It retries "no data" and retriable errors up to
// gce.QueryMaxAttempts times.
func waitForCollectorMetricSeries(ctx context.Context, logger *log.Logger, client *monitoring.MetricClient, vm *gce.VM, metric, collector string, window time.Duration) ([]*monitoringpb.TimeSeries, error) {
	for attempt := 1; attempt <= gce.QueryMaxAttempts; attempt++ {
		tsList, err := nonEmptySeries(logger, lookupCollectorMetric(ctx, client, vm, metric, collector, window))
		if len(tsList) > 0 && err == nil {
			return tsList, nil
		}
		if err != nil && !isRetriableLookupError(ctx, err) {
			return nil, fmt.Errorf("waitForCollectorMetricSeries(metric=%q, collector=%q): %v", metric, collector, err)
		}
		logger.Printf("waitForCollectorMetricSeries(metric=%q, collector=%q): request_error=%v, retrying (%d/%d)...", metric, collector, err, attempt, gce.QueryMaxAttempts)
		time.Sleep(queryBackoffDuration)
	}
	return nil, fmt.Errorf("waitForCollectorMetricSeries(metric=%q, collector=%q) failed: exhausted retries", metric, collector)
}

// waitForPolicySetActive waits until the collector's gcp.policy.set.active
// series reports the expected PolicySet and revision. The series may exist
// before the PolicySet is applied (reporting -1), so this keeps polling until
// a matching point shows up.
func waitForPolicySetActive(ctx context.Context, logger *log.Logger, vm *gce.VM, s tangoSettings, collector string) error {
	want, err := revisionValue(s.expectedRevision)
	if err != nil {
		return err
	}
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		return fmt.Errorf("monitoring.NewMetricClient: %v", err)
	}
	defer client.Close()

	last := "no matching point"
	for attempt := 1; attempt <= queryMaxAttempts; attempt++ {
		series, err := waitForCollectorMetricSeries(ctx, logger, client, vm, policySetActiveMetric, collector, metricWindow)
		if err != nil {
			return fmt.Errorf("%v; see journalctl_otelcol-google.txt", err)
		}
		for _, ts := range series {
			// The label key contains dots, so it is matched here rather than
			// in the Monitoring filter.
			psID := ts.GetMetric().GetLabels()[policySetIDLabel]
			for _, p := range ts.GetPoints() {
				v := int64(p.GetValue().GetDoubleValue())
				if iv := p.GetValue().GetInt64Value(); iv != 0 {
					v = iv
				}
				if v == want && path.Base(psID) == s.policySetID {
					logger.Printf("Found %s point for collector %s: %s=%q value=%d (revision %s)", policySetActiveMetric, collector, policySetIDLabel, psID, v, s.expectedRevision)
					return nil
				}
				last = fmt.Sprintf("%s=%q value=%d", policySetIDLabel, psID, v)
			}
		}
		logger.Printf("Expected point not found yet (attempt %d/%d), latest: %s", attempt, queryMaxAttempts, last)
		time.Sleep(queryBackoffDuration)
	}
	return fmt.Errorf("no %s point for collector %s with PolicySet %q and value %d (revision %s); latest: %s; see journalctl_otelcol-google.txt",
		policySetActiveMetric, collector, s.policySetID, want, s.expectedRevision, last)
}

// uploadSampleLogs uploads sampleLogsFile to tangoLogPath, which the filelog
// receiver picks up on its next poll.
func uploadSampleLogs(ctx context.Context, logger *log.Logger, vm *gce.VM) error {
	f, err := os.Open(sampleLogsFile)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := gce.UploadContent(ctx, logger, vm, f, tangoLogPath); err != nil {
		return fmt.Errorf("failed to upload %s to %s: %v", sampleLogsFile, tangoLogPath, err)
	}
	// The collector does not run as root.
	_, err = gce.RunRemotely(ctx, logger, vm, "sudo chmod 0644 "+tangoLogPath)
	return err
}

// messageQuery is a Cloud Logging filter for the log with the given MESSAGE.
// The map body is ingested as jsonPayload.
func messageQuery(message string) string {
	return fmt.Sprintf("jsonPayload.MESSAGE=%q", message)
}

// verifyFilteredLogs checks that the kept sample log reached Cloud Logging and
// the dropped ones did not. The logs were written together, so once the kept
// log is found the dropped ones would have arrived too. Queries are scoped to
// the VM, so the fixed messages are unambiguous.
func verifyFilteredLogs(ctx context.Context, logger *log.Logger, vm *gce.VM) error {
	// The log name is chosen by telemetry.googleapis.com, so match any.
	if err := gce.WaitForLog(ctx, logger, vm, ".*", logWindow, messageQuery(keptMessage)); err != nil {
		return fmt.Errorf("log %q, which the PolicySet keeps, not found in Cloud Logging: %v", keptMessage, err)
	}
	for _, m := range droppedMessages {
		if err := gce.AssertLogMissing(ctx, logger, vm, ".*", logWindow, messageQuery(m)); err != nil {
			return fmt.Errorf("log %q should have been dropped by the PolicySet: %v", m, err)
		}
	}
	return nil
}

func TestTangoSmoke(t *testing.T) {
	t.Parallel()
	s := tangoSettingsFromEnv(t)

	gce.RunForEachImage(t, func(t *testing.T, imageSpec string) {
		t.Parallel()
		if gce.IsWindows(imageSpec) {
			t.Skip("Tango-GBOC smoke test configures the collector through a systemd drop-in; Windows is not supported yet")
		}
		// The collector authenticates to the Control Plane with an OAuth2
		// access token for the cloud-platform scope, which GCE's default VM
		// scopes do not include.
		ctx, dirLog, vm := commonSetupWithExtraCreateArgumentsAndMetadata(t, imageSpec, []string{"--scopes=cloud-platform"}, nil)
		logger := dirLog.ToMainLog()

		logger.Printf("Installing otelcol-google with drop-in:\n%s", s.dropIn())
		if err := setupTangoCollector(ctx, logger, vm, s); err != nil {
			t.Fatal(err)
		}
		collector, err := collectorID(ctx, logger, vm)
		if err != nil {
			t.Fatal(err)
		}
		if err := waitForPolicySetActive(ctx, logger, vm, s, collector); err != nil {
			t.Fatal(err)
		}
		// Logs written before the PolicySet is active pass through
		// unfiltered, so they are only written now.
		if err := uploadSampleLogs(ctx, logger, vm); err != nil {
			t.Fatal(err)
		}
		if err := verifyFilteredLogs(ctx, logger, vm); err != nil {
			t.Fatal(err)
		}
	})
}
