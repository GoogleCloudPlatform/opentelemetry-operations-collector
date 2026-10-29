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

The test verifies that:
  - the collector connects to the Control Plane, receives the pre-provisioned
    PolicySet revision and applies (ACKs) it;
  - the collector's self-o11y gauge gcp.policy.set.active is exported to the
    Data Plane and, observed through Cloud Monitoring, reports the same
    PolicySet and revision the collector received.

The test assumes a long-lived fleet and PolicySet have been pre-provisioned in
the test project and rolled out (see tango/README.md). It never calls the
Telemetry Management API itself.

Environment variables (in addition to those required by TestSmoke):
TANGO_CP_ENDPOINT: host:port of the xDS Control Plane, e.g.
  staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443.
  The test is skipped when unset.
TANGO_FLEET_ID: fleet the collector subscribes to (default gboc-smoke-fleet).
TANGO_POLICY_SET_ID: pre-created PolicySet ID (default gboc-smoke-ps).
TANGO_REGION: region of the fleet and Control Plane endpoint (default
  asia-east1). Passed explicitly so it does not depend on the VM's zone.
TANGO_EXPECTED_REVISION: optional PolicySet revision ID (e.g. 3f038775). When
  set, the revision applied by the collector must match it.
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

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
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

	tangoConfPath = "/etc/otelcol-google/otelcol-google.conf"

	controlPlaneWait  = 5 * time.Minute
	metricWindow      = 30 * time.Minute
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
	region           string
	expectedRevision string
}

func tangoSettingsFromEnv(t *testing.T) tangoSettings {
	t.Helper()
	s := tangoSettings{
		cpEndpoint:       os.Getenv("TANGO_CP_ENDPOINT"),
		fleetID:          envOrDefault("TANGO_FLEET_ID", "gboc-smoke-fleet"),
		policySetID:      envOrDefault("TANGO_POLICY_SET_ID", "gboc-smoke-ps"),
		region:           envOrDefault("TANGO_REGION", "asia-east1"),
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
// exclusively through the googlecontrolplane provider. Project and project
// number of the xDS subscription are taken from the GCE metadata server.
func (s tangoSettings) tangoOptions() string {
	return fmt.Sprintf("--config=googlecontrolplane:xds://%s?gcp.fleet_id=%s&region=%s", s.cpEndpoint, s.fleetID, s.region)
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
	return restartOtelCollector(ctx, logger, vm)
}

// appliedPolicySet is what the collector reported when it applied a PolicySet
// received from the Control Plane.
type appliedPolicySet struct {
	// revisionName is the full PolicySetRevision resource name, e.g.
	// projects/123/locations/asia-east1/policySets/gboc-smoke-ps/revisions/3f038775.
	revisionName   string
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
				applied.revisionName = m[1]
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

// SelfO11yTest verifies that gcp.policy.set.active is exported to the Data
// Plane for this collector and, observed through Cloud Monitoring, reports the
// PolicySet and revision the collector received from the Control Plane.
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

	// The series is keyed on prometheus_target's instance label, which carries
	// the collector's service.instance.id. The PolicySet ID is checked in code
	// because its label key ("gcp.policy.set.id") contains dots.
	filter := fmt.Sprintf(`metric.type = %q AND resource.type = "prometheus_target" AND resource.labels.instance = %q`,
		policySetActiveMetric, applied.collectorID)
	logger.Printf("Cloud Monitoring filter: %s", filter)

	var lastValue *int64
	var lastPolicySetID string
	for attempt := 1; attempt <= queryMaxAttempts; attempt++ {
		now := time.Now()
		it := client.ListTimeSeries(ctx, &monitoringpb.ListTimeSeriesRequest{
			Name:   "projects/" + vm.Project,
			Filter: filter,
			Interval: &monitoringpb.TimeInterval{
				StartTime: timestamppb.New(now.Add(-metricWindow)),
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
			if len(ts.GetPoints()) == 0 {
				continue
			}
			// Points are returned newest first.
			got := ts.GetPoints()[0].GetValue().GetDoubleValue()
			if iv := ts.GetPoints()[0].GetValue().GetInt64Value(); iv != 0 {
				got = float64(iv)
			}
			v := int64(got)
			psID := ts.GetMetric().GetLabels()[policySetIDLabel]
			lastValue, lastPolicySetID = &v, psID
			logger.Printf("Found %s series for collector %s: value=%d labels=%v", policySetActiveMetric, applied.collectorID, v, ts.GetMetric().GetLabels())
			if v == want && path.Base(psID) == s.policySetID {
				return
			}
		}
		time.Sleep(queryPollInterval)
	}
	if lastValue == nil {
		t.Fatalf("no %s series found for collector %s (filter: %s)", policySetActiveMetric, applied.collectorID, filter)
	}
	t.Fatalf("%s = %d with %s=%q, want %d (revision %s) for PolicySet %q", policySetActiveMetric, *lastValue, policySetIDLabel, lastPolicySetID, want, applied.revision, s.policySetID)
}

func TestTangoSmoke(t *testing.T) {
	t.Parallel()
	s := tangoSettingsFromEnv(t)

	gce.RunForEachImage(t, func(t *testing.T, imageSpec string) {
		t.Parallel()
		if gce.IsWindows(imageSpec) {
			t.Skip("Tango-GBOC smoke test configures the collector through the Linux systemd environment file; Windows is not supported yet")
		}
		// The collector authenticates to the Control Plane with an OAuth2
		// access token for the cloud-platform scope, which GCE's default VM
		// scopes do not include.
		ctx, dirLog, vm := commonSetupWithExtraCreateArgumentsAndMetadata(t, imageSpec, []string{"--scopes=cloud-platform"}, nil)
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
		logger.Printf("Collector %s applied PolicySet revision %q (%d policies loaded)", applied.collectorID, applied.revisionName, applied.policiesLoaded)
		if path.Base(path.Dir(path.Dir(applied.revisionName))) != s.policySetID {
			t.Fatalf("collector applied PolicySet revision %q, want a revision of PolicySet %q", applied.revisionName, s.policySetID)
		}
		if s.expectedRevision != "" && applied.revision != s.expectedRevision {
			t.Fatalf("collector applied revision %q, want %q", applied.revision, s.expectedRevision)
		}

		// Data Plane: the self-o11y gauge reports the same revision.
		SelfO11yTest(ctx, t, logger, vm, s, applied)
	})
}
