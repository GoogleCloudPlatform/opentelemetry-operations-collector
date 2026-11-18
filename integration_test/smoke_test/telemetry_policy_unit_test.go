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

package smoke

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/api/option"
	secretmanager "google.golang.org/api/secretmanager/v1"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/gce"
)

func TestFilterOffGCPHosts(t *testing.T) {
	in := "127.0.0.1\tlocalhost\r\n" +
		"::1\t\tlocalhost ip6-localhost ip6-loopback\r\n" +
		"10.140.0.12\tvm-1.asia-east1-a.c.gboc-tango-testing.internal vm-1  # Added by Google\r\n" +
		"# comment mentioning 169.254.169.254 is harmless\r\n" +
		"169.254.169.254 metadata.google.internal  # Added by Google\r\n" +
		"169.254.169.254 metadata\r\n"
	got := filterOffGCPHosts(in)
	if strings.Contains(got, "169.254.169.254 metadata") {
		t.Errorf("filterOffGCPHosts left GCE metadata IP mapping:\n%s", got)
	}
	if !strings.Contains(got, "10.140.0.12\tvm-1.asia-east1-a.c.gboc-tango-testing.internal vm-1") {
		t.Errorf("filterOffGCPHosts dropped VM hostname entry:\n%s", got)
	}
	if !strings.Contains(got, "127.0.0.1 metadata.google.internal metadata.google.internal. metadata\n") {
		t.Errorf("filterOffGCPHosts missing loopback metadata override (including trailing-dot alias for cgo):\n%s", got)
	}
}

func TestTelemetryPolicySettingsFromEnvAndOSSelection(t *testing.T) {
	origRunID := testRunID
	testRunID = "test-run"
	t.Cleanup(func() { testRunID = origRunID })

	t.Setenv("TELEMETRY_POLICY_CONTROL_PLANE_ENDPOINT", "cp.example.com:443")
	t.Setenv("TELEMETRY_POLICY_EXPECTED_REVISION", "1b20a331")
	t.Setenv("TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS", "3d90357c")
	t.Setenv("PROJECT_NUMBER", "406580743452")

	t.Setenv("OFF_GCP", "false")
	onGCP := telemetryPolicySettingsFromEnv(t)
	if onGCP.offGCP || onGCP.projectNumber != "" || onGCP.wifCertSecret != "" || onGCP.wifKeySecret != "" {
		t.Fatalf("expected off-GCP fields to stay empty when OFF_GCP=false, got %+v", onGCP)
	}
	if got := onGCP.fleetIDFor("debian-cloud:debian-12"); got != defaultLinuxFleetID {
		t.Errorf("Linux fleetID = %q, want %q", got, defaultLinuxFleetID)
	}
	if got := onGCP.fleetIDFor("windows-cloud:windows-2022"); got != defaultWindowsFleetID {
		t.Errorf("Windows fleetID = %q, want %q", got, defaultWindowsFleetID)
	}
	if got := onGCP.policySetIDFor("debian-cloud:debian-12"); got != defaultLinuxPolicySetID {
		t.Errorf("Linux policySetID = %q, want %q", got, defaultLinuxPolicySetID)
	}
	if got := onGCP.policySetIDFor("windows-cloud:windows-2022"); got != defaultWindowsPolicySetID {
		t.Errorf("Windows policySetID = %q, want %q", got, defaultWindowsPolicySetID)
	}
	if got := onGCP.expectedRevisionFor("debian-cloud:debian-12"); got != "1b20a331" {
		t.Errorf("Linux expectedRevision = %q, want 1b20a331", got)
	}
	if got := onGCP.expectedRevisionFor("windows-cloud:windows-2022"); got != "3d90357c" {
		t.Errorf("Windows expectedRevision = %q, want 3d90357c", got)
	}

	t.Setenv("OFF_GCP", "true")
	offGCP := telemetryPolicySettingsFromEnv(t)
	wantAudience := "//iam.googleapis.com/projects/406580743452/locations/global/workloadIdentityPools/gboc-x509-pool/providers/gboc-x509-prov"
	if !offGCP.offGCP || offGCP.projectNumber != "406580743452" ||
		offGCP.wifCertSecret != defaultWIFCertSecret || offGCP.wifKeySecret != defaultWIFKeySecret ||
		offGCP.wifAudience != wantAudience || offGCP.wifDir != "" {
		t.Fatalf("unexpected off-GCP settings: %+v", offGCP)
	}
}

func TestTelemetryPolicyConfigHelpers(t *testing.T) {
	s := telemetryPolicySettings{
		cpEndpoint:         "staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443",
		fleetID:            defaultLinuxFleetID,
		windowsFleetID:     defaultWindowsFleetID,
		policySetID:        defaultLinuxPolicySetID,
		windowsPolicySetID: defaultWindowsPolicySetID,
		region:             "asia-east1",
		offGCP:             true,
		projectNumber:      "406580743452",
	}

	dropIn := s.dropIn("gboc-tango-testing", "debian-cloud:debian-12")
	for _, forbidden := range []string{
		"GCE_METADATA_HOST",
		"OTELCOL_OPTIONS",
		"config.yaml",
		"offgcp_override",
		"GOOGLE_CLOUD_PROJECT_NUMBER",
		"GOOGLE_CLOUD_REGION",
	} {
		if strings.Contains(dropIn, forbidden) {
			t.Errorf("off-GCP dropIn must not contain %q:\n%s", forbidden, dropIn)
		}
	}
	for _, want := range []string{
		"ExecStartPre=+/bin/sh /etc/otelcol-google/block_metadata.sh",
		"BindReadOnlyPaths=/etc/otelcol-google/hosts:/etc/hosts",
		"Environment=GOOGLE_APPLICATION_CREDENTIALS=/etc/otelcol-google/wif/cred_config.json",
		"Environment=GOOGLE_CLOUD_PROJECT=gboc-tango-testing",
		"ExecStart=/usr/bin/otelcol-google --config=googlecontrolplane:xds://",
		"gcp.fleet_id=gboc-smoke-fleet",
		"region=asia-east1",
		"project=gboc-tango-testing",
		"project_number=406580743452",
	} {
		if !strings.Contains(dropIn, want) {
			t.Errorf("off-GCP dropIn missing %q:\n%s", want, dropIn)
		}
	}

	winURI := s.controlPlaneConfig("gboc-tango-testing", "windows-cloud:windows-2022")
	if !strings.Contains(winURI, "gcp.fleet_id=gboc-smoke-fleet-windows") || !strings.Contains(winURI, "project_number=406580743452") {
		t.Errorf("unexpected Windows off-GCP URI: %s", winURI)
	}

	onGCP := telemetryPolicySettings{
		cpEndpoint:     "staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443",
		fleetID:        defaultLinuxFleetID,
		windowsFleetID: defaultWindowsFleetID,
		region:         "asia-east1",
	}
	onGCPDropIn := onGCP.dropIn("stackdriver-test-143416", "debian-cloud:debian-12")
	for _, forbidden := range []string{"BindReadOnlyPaths", "OTELCOL_OPTIONS", "config.yaml"} {
		if strings.Contains(onGCPDropIn, forbidden) {
			t.Errorf("on-GCP dropIn must not contain %q:\n%s", forbidden, onGCPDropIn)
		}
	}

	if got, want := keptTransformedQuery("gboc-smoke=keep"), `textPayload="gboc-smoke=keep" AND labels.gboc_smoke="true"`; got != want {
		t.Errorf("keptTransformedQuery = %q, want %q", got, want)
	}
	if got, want := offGCPKeptTransformedQuery("gboc-smoke=keep vm=42"), `resource.type="generic_node" AND textPayload="gboc-smoke=keep vm=42" AND labels.gboc_smoke="true"`; got != want {
		t.Errorf("offGCPKeptTransformedQuery = %q, want %q", got, want)
	}

	tagged := string(tagSampleLogs([]byte("gboc-smoke=keep\r\ngboc-smoke=drop\r\n"), &gce.VM{ID: 42}))
	wantTagged := "gboc-smoke=keep vm=42\ngboc-smoke=drop vm=42\n"
	if tagged != wantTagged {
		t.Errorf("tagSampleLogs = %q, want %q", tagged, wantTagged)
	}

	for _, script := range []string{ensureNetfilterLocalScript, blockMetadataLocalScript, configureWindowsLocalScript} {
		if _, err := os.Stat(script); err != nil {
			t.Errorf("expected helper script %s to exist: %v", script, err)
		}
	}
}

func TestAccessSecretBytesAndLocalOverride(t *testing.T) {
	wantCert := "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/gboc-tango-testing/secrets/gboc-x509-leaf-cert/versions/latest:access" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&secretmanager.AccessSecretVersionResponse{
			Payload: &secretmanager.SecretPayload{
				Data: base64.StdEncoding.EncodeToString([]byte(wantCert)),
			},
		})
	}))
	t.Cleanup(srv.Close)

	svc, err := secretmanager.NewService(context.Background(), option.WithEndpoint(srv.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	got, err := accessSecretBytes(context.Background(), svc, "gboc-tango-testing", defaultWIFCertSecret)
	if err != nil {
		t.Fatalf("accessSecretBytes failed: %v", err)
	}
	if string(got) != wantCert {
		t.Fatalf("accessSecretBytes = %q, want %q", string(got), wantCert)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "leaf.pem"), []byte("dir-cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "leaf.key"), []byte("dir-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, key, err := loadWIFLeafCertAndKey(context.Background(), log.New(io.Discard, "", 0), "gboc-tango-testing", telemetryPolicySettings{wifDir: dir})
	if err != nil {
		t.Fatalf("loadWIFLeafCertAndKey(wifDir) failed: %v", err)
	}
	if string(cert) != "dir-cert" || string(key) != "dir-key" {
		t.Fatalf("unexpected local override pair: cert=%q key=%q", string(cert), string(key))
	}
}
