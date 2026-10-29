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
Smoke test for Tango-GBOC: otelcol-google configured exclusively through the
googlecontrolplane provider, talking to a live Control Plane and Data Plane.

The test assumes a long-lived fleet and PolicySet have been pre-provisioned in
the test project and rolled out (see tango/README.md). It never calls the
Telemetry Management API itself.

Environment variables (in addition to those required by TestSmoke):
TANGO_CP_ENDPOINT: host:port of the xDS Control Plane, e.g.
  staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443.
  The test is skipped when unset.
TANGO_FLEET_ID: fleet the collector subscribes to (default gboc-smoke-fleet).
TANGO_POLICY_SET_ID: pre-created PolicySet ID (default gboc-smoke-ps).
TANGO_EXPECTED_REVISION: optional PolicySet revision ID (e.g. 904e9adb). When
  set, the revision ACKed by the collector must match it.
*/

package smoke

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/logging"
	"cloud.google.com/go/logging/logadmin"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/gce"
)

const (
	// probeTag is the syslog identifier of the probe lines. The PolicySet's
	// transform policy matches on it, and its catch-all drop policy drops
	// every journald record that does not carry it.
	probeTag = "gboc-smoke"
	// dropPattern is the substring the PolicySet's filter policy drops on.
	dropPattern = "gboc-smoke=drop"
	// transformAttribute is the attribute the PolicySet's transform policy
	// adds to every probe.
	transformAttribute = "gboc_smoke"

	// policySetActiveMetric is the self-o11y gauge reporting the enforced
	// PolicySet revision. It reaches Cloud Monitoring through
	// telemetry.googleapis.com under the prometheus.googleapis.com namespace.
	policySetActiveMetric = "prometheus.googleapis.com/gcp.policy.set.active/gauge"
	// policySetIDLabel is the metric label carrying gcp.policy.set.id.
	policySetIDLabel = "gcp_policy_set_id"

	tangoConfPath = "/etc/otelcol-google/otelcol-google.conf"

	controlPlaneWait  = 5 * time.Minute
	dataPlaneWindow   = 30 * time.Minute
	dropGracePeriod   = 2 * time.Minute
	queryPollInterval = 30 * time.Second
	queryMaxAttempts  = 20 // 10 minutes.
)

var (
	appliedRevisionRE = regexp.MustCompile(`"policy_set_revision":\s*"([^"]*)"`)
	policiesLoadedRE  = regexp.MustCompile(`"policies_loaded":\s*(\d+)`)
	collectorIDRE     = regexp.MustCompile(`"service\.instance\.id":\s*"([^"]+)"`)

	// controlPlaneFailures are journal fragments that mean the collector will
	// not receive the PolicySet, so there is no point in waiting further.
	controlPlaneFailures = []string{
		"rejected this collector's credentials",
		"sending NACK",
	}
)

// tangoSettings holds the Tango-specific environment for the test.
type tangoSettings struct {
	cpEndpoint       string
	fleetID          string
	policySetID      string
	expectedRevision string
}

func tangoSettingsFromEnv(t *testing.T) tangoSettings {
	t.Helper()
	s := tangoSettings{
		cpEndpoint:       os.Getenv("TANGO_CP_ENDPOINT"),
		fleetID:          envOrDefault("TANGO_FLEET_ID", "gboc-smoke-fleet"),
		policySetID:      envOrDefault("TANGO_POLICY_SET_ID", "gboc-smoke-ps"),
		expectedRevision: os.Getenv("TANGO_EXPECTED_REVISION"),
	}
	if s.cpEndpoint == "" {
		t.Skip("TANGO_CP_ENDPOINT is not set; skipping Tango-GBOC smoke test")
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

// tangoOptions returns the OTELCOL_OPTIONS that configure the collector
// exclusively through the googlecontrolplane provider. Project, project number
// and region of the xDS subscription are taken from the GCE metadata server.
func (s tangoSettings) tangoOptions() string {
	return fmt.Sprintf("--config=googlecontrolplane:xds://%s?gcp.fleet_id=%s", s.cpEndpoint, s.fleetID)
}

// setupTangoCollector installs otelcol-google and restarts it in policy-only
// mode.
func setupTangoCollector(ctx context.Context, logger *log.Logger, vm *gce.VM, s tangoSettings) error {
	if err := installOtelCollector(ctx, logger, vm, locationFromEnvVars()); err != nil {
		return err
	}
	conf := fmt.Sprintf("OTELCOL_OPTIONS=%q\n", s.tangoOptions())
	if err := gce.UploadContent(ctx, logger, vm, strings.NewReader(conf), tangoConfPath); err != nil {
		return fmt.Errorf("failed to upload %s: %v", tangoConfPath, err)
	}
	// The journald receiver runs as the otelcol-google user, which needs
	// read access to the system journal.
	// TODO: drop this once the package grants the access itself, or confirm
	// that it is not needed.
	if _, err := gce.RunRemotely(ctx, logger, vm, "sudo usermod -aG systemd-journal otelcol-google"); err != nil {
		return fmt.Errorf("failed to grant journal access: %v", err)
	}
	return restartOtelCollector(ctx, logger, vm)
}

// appliedPolicySet is what the collector reported when it applied a PolicySet
// received from the Control Plane.
type appliedPolicySet struct {
	revision       string
	policiesLoaded int
	collectorID    string
}

// waitForAppliedPolicySet polls the collector's journal until it reports that
// it applied (and therefore ACKed) a non-empty PolicySet from the Control
// Plane.
func waitForAppliedPolicySet(ctx context.Context, logger *log.Logger, vm *gce.VM) (*appliedPolicySet, error) {
	deadline := time.Now().Add(controlPlaneWait)
	for time.Now().Before(deadline) {
		out, err := gce.RunRemotely(ctx, logger, vm, "sudo journalctl -u otelcol-google --no-pager -o cat")
		if err != nil {
			return nil, err
		}
		for _, failure := range controlPlaneFailures {
			if strings.Contains(out.Stdout, failure) {
				return nil, fmt.Errorf("collector journal reports a Control Plane failure (%q); see journalctl_otelcol-google.txt", failure)
			}
		}
		for _, line := range strings.Split(out.Stdout, "\n") {
			if !strings.Contains(line, "Applied xDS policy set") {
				continue
			}
			applied := &appliedPolicySet{}
			if m := appliedRevisionRE.FindStringSubmatch(line); m != nil {
				applied.revision = path.Base(m[1])
			}
			if m := policiesLoadedRE.FindStringSubmatch(line); m != nil {
				applied.policiesLoaded, _ = strconv.Atoi(m[1])
			}
			if m := collectorIDRE.FindStringSubmatch(line); m != nil {
				applied.collectorID = m[1]
			}
			if applied.policiesLoaded > 0 {
				return applied, nil
			}
			logger.Printf("Collector applied an empty PolicySet, still waiting: %s", line)
		}
		time.Sleep(10 * time.Second)
	}
	return nil, fmt.Errorf("collector did not apply a non-empty PolicySet within %v", controlPlaneWait)
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

// emitProbes writes one KEEP and one DROP probe line into the journal.
func emitProbes(ctx context.Context, logger *log.Logger, vm *gce.VM, marker string) error {
	cmd := fmt.Sprintf(`logger -t %s "KEEP %s" && logger -t %s "%s %s"`, probeTag, marker, probeTag, dropPattern, marker)
	_, err := gce.RunRemotely(ctx, logger, vm, cmd)
	return err
}

// queryLogsByText returns the Cloud Logging entries in the project that
// contain text, over the trailing dataPlaneWindow.
//
// The query is keyed on the run-unique marker rather than on the monitored
// resource, because the resource the Data Plane assigns to OTLP logs is not
// controlled by this test.
func queryLogsByText(ctx context.Context, logger *log.Logger, client *logadmin.Client, text string) ([]*logging.Entry, error) {
	filter := fmt.Sprintf(`timestamp >= %q AND %q`, time.Now().Add(-dataPlaneWindow).Format(time.RFC3339), text)
	logger.Printf("Cloud Logging filter: %s", filter)
	var entries []*logging.Entry
	it := client.Entries(ctx, logadmin.Filter(filter))
	for {
		e, err := it.Next()
		if err == iterator.Done {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		logger.Printf("Found log entry: %+v", e)
		entries = append(entries, e)
	}
}

// hasTransformAttribute reports whether the entry carries the attribute added
// by the PolicySet's transform policy, either as a label or in the payload.
// TODO: tighten to the exact field once the Data Plane mapping of OTLP log
// attributes is confirmed on a live run.
func hasTransformAttribute(e *logging.Entry) bool {
	if v, ok := e.Labels[transformAttribute]; ok {
		return v == "true"
	}
	return strings.Contains(fmt.Sprint(e.Payload), transformAttribute)
}

// DataPlaneTest verifies that the probes are filtered and transformed as the
// PolicySet dictates, exported to telemetry.googleapis.com, and queryable in
// Cloud Logging.
func DataPlaneTest(ctx context.Context, t *testing.T, logger *log.Logger, vm *gce.VM) {
	client, err := logadmin.NewClient(ctx, vm.Project)
	if err != nil {
		t.Fatalf("logadmin.NewClient: %v", err)
	}
	defer client.Close()

	marker := fmt.Sprintf("%s-%s", testRunID, vm.Name)
	keep := "KEEP " + marker

	// Re-emit the probes on every attempt in case the first ones were written
	// before the journald receiver started.
	var kept []*logging.Entry
	for attempt := 1; attempt <= queryMaxAttempts && len(kept) == 0; attempt++ {
		if err := emitProbes(ctx, logger, vm, marker); err != nil {
			t.Fatalf("emitProbes: %v", err)
		}
		time.Sleep(queryPollInterval)
		if kept, err = queryLogsByText(ctx, logger, client, keep); err != nil {
			logger.Printf("Cloud Logging query failed (attempt %d/%d): %v", attempt, queryMaxAttempts, err)
		}
	}
	if len(kept) == 0 {
		t.Fatalf("KEEP probe %q not found in Cloud Logging", keep)
	}
	if !hasTransformAttribute(kept[0]) {
		t.Errorf("KEEP probe was exported without the %q attribute added by the transform policy: %+v", transformAttribute, kept[0])
	}

	// The KEEP probe is there, so the DROP probes emitted alongside it would
	// have arrived by now (plus a grace period) had they not been dropped.
	time.Sleep(dropGracePeriod)
	dropped, err := queryLogsByText(ctx, logger, client, dropPattern+" "+marker)
	if err != nil {
		t.Fatalf("Cloud Logging query for DROP probe: %v", err)
	}
	if len(dropped) > 0 {
		t.Errorf("DROP probe was exported despite the filter policy: %+v", dropped[0])
	}
}

// SelfO11yTest verifies that gcp.policy.set.active is exported to Cloud
// Monitoring for this collector and reports the enforced revision.
func SelfO11yTest(ctx context.Context, t *testing.T, logger *log.Logger, vm *gce.VM, s tangoSettings, applied *appliedPolicySet) {
	want, err := revisionValue(applied.revision)
	if err != nil {
		t.Fatal(err)
	}
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		t.Fatalf("monitoring.NewMetricClient: %v", err)
	}
	defer client.Close()

	filter := fmt.Sprintf(`metric.type = %q AND metric.labels.%s = monitoring.regex.full_match(".*%s.*")`,
		policySetActiveMetric, policySetIDLabel, regexp.QuoteMeta(s.policySetID))
	logger.Printf("Cloud Monitoring filter: %s", filter)

	var lastValue *int64
	for attempt := 1; attempt <= queryMaxAttempts; attempt++ {
		now := time.Now()
		it := client.ListTimeSeries(ctx, &monitoringpb.ListTimeSeriesRequest{
			Name:   "projects/" + vm.Project,
			Filter: filter,
			Interval: &monitoringpb.TimeInterval{
				StartTime: timestamppb.New(now.Add(-dataPlaneWindow)),
				EndTime:   timestamppb.New(now),
			},
			View: monitoringpb.ListTimeSeriesRequest_FULL,
		})
		for {
			ts, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				logger.Printf("ListTimeSeries failed (attempt %d/%d): %v", attempt, queryMaxAttempts, err)
				break
			}
			if !seriesBelongsTo(ts, applied.collectorID) || len(ts.GetPoints()) == 0 {
				continue
			}
			// Points are returned newest first.
			got := ts.GetPoints()[0].GetValue().GetDoubleValue()
			if iv := ts.GetPoints()[0].GetValue().GetInt64Value(); iv != 0 {
				got = float64(iv)
			}
			v := int64(got)
			lastValue = &v
			logger.Printf("Found %s series for collector %s: value=%d labels=%v resource=%v", policySetActiveMetric, applied.collectorID, v, ts.GetMetric().GetLabels(), ts.GetResource().GetLabels())
			if v == want {
				return
			}
		}
		time.Sleep(queryPollInterval)
	}
	if lastValue == nil {
		t.Fatalf("no %s series found for collector %s (filter: %s)", policySetActiveMetric, applied.collectorID, filter)
	}
	t.Fatalf("%s = %d, want %d (revision %s)", policySetActiveMetric, *lastValue, want, applied.revision)
}

// seriesBelongsTo reports whether any resource or metric label of the series
// carries the collector ID (service.instance.id).
// TODO: narrow to the exact label once the prometheus_target mapping of
// service.instance.id is confirmed on a live run.
func seriesBelongsTo(ts *monitoringpb.TimeSeries, collectorID string) bool {
	if collectorID == "" {
		return true
	}
	for _, labels := range []map[string]string{ts.GetResource().GetLabels(), ts.GetMetric().GetLabels()} {
		for _, v := range labels {
			if v == collectorID {
				return true
			}
		}
	}
	return false
}

func TestTangoSmoke(t *testing.T) {
	t.Parallel()
	s := tangoSettingsFromEnv(t)

	gce.RunForEachImage(t, func(t *testing.T, imageSpec string) {
		t.Parallel()
		if gce.IsWindows(imageSpec) {
			t.Skip("Tango-GBOC smoke test uses a journald source policy; Windows is not supported")
		}
		ctx, dirLog, vm := commonSetupWithExtraCreateArgumentsAndMetadata(t, imageSpec, nil, nil)
		logger := dirLog.ToMainLog()
		t.Cleanup(func() {
			gce.RunRemotely(ctx, dirLog.ToFile("otelcol-google.conf.txt"), vm, "sudo cat "+tangoConfPath)
		})

		logger.Printf("Installing otelcol-google with OTELCOL_OPTIONS=%s", s.tangoOptions())
		if err := setupTangoCollector(ctx, logger, vm, s); err != nil {
			t.Fatal(err)
		}

		// Control Plane: the collector authenticates, receives the PolicySet
		// and ACKs it. The ACK is sent right after the "Applied" log line.
		applied, err := waitForAppliedPolicySet(ctx, logger, vm)
		if err != nil {
			t.Fatal(err)
		}
		logger.Printf("Collector %s applied PolicySet revision %q with %d policies", applied.collectorID, applied.revision, applied.policiesLoaded)
		if s.expectedRevision != "" && applied.revision != s.expectedRevision {
			t.Fatalf("collector applied revision %q, want %q", applied.revision, s.expectedRevision)
		}

		t.Run("data_plane", func(t *testing.T) {
			t.Parallel()
			DataPlaneTest(ctx, t, logger, vm)
		})
		t.Run("self_o11y", func(t *testing.T) {
			t.Parallel()
			SelfO11yTest(ctx, t, logger, vm, s, applied)
		})
	})
}
