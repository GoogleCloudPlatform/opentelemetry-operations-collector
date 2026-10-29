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

	metricWindow      = 30 * time.Minute
	queryPollInterval = 30 * time.Second
	queryMaxAttempts  = 20 // 10 minutes.
)

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

// setupTangoCollector installs otelcol-google and restarts it with the
// googlecontrolplane config source added.
func setupTangoCollector(ctx context.Context, logger *log.Logger, vm *gce.VM, s tangoSettings) error {
	if err := installOtelCollector(ctx, logger, vm, locationFromEnvVars()); err != nil {
		return err
	}
	if _, err := gce.RunRemotely(ctx, logger, vm, "sudo mkdir -p "+path.Dir(tangoDropInPath)); err != nil {
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

// waitForPolicySetActive polls Cloud Monitoring until the collector's
// gcp.policy.set.active series reports the expected PolicySet and revision.
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

	// The series is keyed on prometheus_target's instance label, which carries
	// the collector's service.instance.id. The PolicySet ID is checked in code
	// because its label key ("gcp.policy.set.id") contains dots.
	filter := fmt.Sprintf(`metric.type = %q AND resource.type = "prometheus_target" AND resource.labels.instance = %q`,
		policySetActiveMetric, collector)
	logger.Printf("Cloud Monitoring filter: %s", filter)

	last := "no series found"
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
		time.Sleep(queryPollInterval)
	}
	return fmt.Errorf("no %s point for collector %s with PolicySet %q and value %d (revision %s); latest: %s; see journalctl_otelcol-google.txt",
		policySetActiveMetric, collector, s.policySetID, want, s.expectedRevision, last)
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
		t.Cleanup(func() {
			gce.RunRemotely(ctx, dirLog.ToFile("otelcol-google.service.txt"), vm, "sudo systemctl cat otelcol-google")
		})

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
	})
}
