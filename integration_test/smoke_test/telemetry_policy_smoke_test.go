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
End-to-end smoke test for the Telemetry Policy loop: Control Plane (Telemetry
Director, over xDS) -> Collector (otelcol-google, configured solely via the
googlecontrolplane config source) -> Data Plane (telemetry.googleapis.com) ->
Cloud Monitoring & Cloud Logging.

The test installs the packaged collector on a GCE VM (Linux or Windows), starts
it with --config=googlecontrolplane:xds://... (without any local config.yaml),
and waits for its gcp.policy.set.active self-observability metric to appear in
Cloud Monitoring with the expected gcp.policy.set.id label and revision value.
googlecontrolplaneextension only updates that gauge after the provider has
evaluated and applied (and ACKed) a revision, so a point carrying the expected
PolicySet and revision proves the whole Control Plane to Data Plane loop.

Once the PolicySet is active, the test writes plain-text sample log lines into
the directory tailed by filelog/googlecontrolplane (synthesized from the
PolicySet's fileLogSourcePolicy), and checks in Cloud Logging that:
  - the PolicySet's two logFilterPolicy rules kept and dropped the expected lines, and
  - the PolicySet's logTransformPolicy added labels.gboc_smoke="true" to the kept line.

Linux and Windows collectors subscribe to separate fleets and OS-specific
PolicySets (telemetry_policy/policyset.json for /var/log/gboc-smoke/*.log on
Linux, and telemetry_policy/policyset_windows.json for
%SystemDrive%/gboc-smoke/*.log on Windows). The same PolicySet definitions are
used on GCP and off GCP.

Environment variables (in addition to those in smoke_test.go):
TELEMETRY_POLICY_CONTROL_PLANE_ENDPOINT: host:port of the xDS Control Plane, e.g.
  staging-telemetrydirector.asia-east1.rep.sandbox.googleapis.com:443.
  The test is skipped when unset.
TELEMETRY_POLICY_EXPECTED_REVISION: revision ID of the pre-provisioned Linux
  PolicySet rolled out to FLEET_ID (also used as fallback on Windows if
  TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS is unset).
TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS: revision ID of the pre-provisioned
  Windows PolicySet rolled out to WINDOWS_FLEET_ID.
FLEET_ID: Linux fleet ID (default gboc-smoke-fleet).
POLICY_SET_ID: Linux PolicySet ID (default gboc-smoke-ps).
WINDOWS_FLEET_ID: Windows fleet ID (default gboc-smoke-fleet-windows).
WINDOWS_POLICY_SET_ID: Windows PolicySet ID (default gboc-smoke-ps-windows).
REGION: region of the fleet and Control Plane endpoint (default asia-east1).
  Passed explicitly so it does not depend on the VM's zone.

Off-GCP mode (see telemetry_policy/README.md):
OFF_GCP: set to "true" to simulate an off-GCP host on Linux or Windows: after
  installing the collector package and writing helper scripts, filtered hosts,
  and X.509 Workload Identity Federation (WIF) credentials from Secret Manager
  directly onto the VM over SSH, the test stops the VM, detaches its GCE service
  account and OAuth scopes, blocks the GCE metadata server for the collector
  process, and boots the VM back up.
PROJECT_NUMBER: project number of the fleet (required when OFF_GCP=true, since
  the metadata server is blocked).
WIF_AUDIENCE: audience of the X.509 WIF provider (default:
  //iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/gboc-x509-pool/providers/gboc-x509-prov).
WIF_CERT_SECRET: Secret Manager secret ID in PROJECT holding the PEM workload
  certificate (default gboc-x509-leaf-cert).
WIF_KEY_SECRET: Secret Manager secret ID in PROJECT holding the PEM workload
  private key (default gboc-x509-leaf-key).
WIF_DIR: optional local directory holding leaf.pem and leaf.key to override
  Secret Manager lookup.
*/

package smoke

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/logging/logadmin"
	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"github.com/google/uuid"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	secretmanager "google.golang.org/api/secretmanager/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/gce"
)

const (
	// collectorUUIDNamespace is googlecontrolplaneprovider's namespace for
	// deriving the collector ID (service.instance.id) from the hostname.
	collectorUUIDNamespace = "5AFFCE1A-3D0A-4220-8EA1-7D64A842BEB4"

	// telemetryPolicyDropInPath is the systemd drop-in written on Linux so
	// otelcol-google runs strictly with the googlecontrolplane config source.
	telemetryPolicyDropInPath = "/etc/systemd/system/otelcol-google.service.d/telemetry-policy.conf"

	// policySetActiveMetric is exported via OTLP to telemetry.googleapis.com,
	// which lands in Cloud Monitoring under prometheus.googleapis.com/<name>/gauge
	// against the prometheus_target monitored resource.
	policySetActiveMetric = "prometheus.googleapis.com/gcp.policy.set.active/gauge"
	policySetIDLabel      = "gcp.policy.set.id"

	// Default collector export interval is 60s; allow several intervals for the
	// Control Plane stream to establish, apply the PolicySet, and export a point.
	metricWindow         = 10 * time.Minute
	queryMaxAttempts     = 20
	queryBackoffDuration = 30 * time.Second

	// Per-OS log directories matching telemetry_policy/policyset.json and
	// telemetry_policy/policyset_windows.json.
	telemetryPolicyLogDirLinux   = "/var/log/gboc-smoke"
	telemetryPolicyLogDirWindows = `C:\gboc-smoke`

	// sampleLogsFile is written to activeLogsFileName over SSH only after the
	// PolicySet is active so filelog/googlecontrolplane (start_at: end) tails
	// the newly created file through the active filter and transform policies.
	sampleLogsFile     = "telemetry_policy/sample_logs.log"
	activeLogsFileName = "test.log"

	// keptMessage is the sample log line kept by logFilterPolicy; logTransformPolicy
	// also stamps transformedAttrKey=transformedAttrValue onto it.
	keptMessage          = "gboc-smoke=keep"
	transformedAttrKey   = "gboc_smoke"
	transformedAttrValue = "true"
	logWindow            = 30 * time.Minute

	// Helper scripts stored under integration_test/smoke_test/telemetry_policy/.
	ensureFirewallLocalScript   = "telemetry_policy/ensure_firewall_linux.sh"
	blockMetadataLocalScript    = "telemetry_policy/block_metadata_linux.sh"
	configureWindowsLocalScript = "telemetry_policy/configure_windows.ps1"

	// Default per-OS fleet and PolicySet IDs.
	defaultLinuxFleetID       = "gboc-smoke-fleet"
	defaultLinuxPolicySetID   = "gboc-smoke-ps"
	defaultWindowsFleetID     = "gboc-smoke-fleet-windows"
	defaultWindowsPolicySetID = "gboc-smoke-ps-windows"

	// Off-GCP simulation paths and defaults on Linux and Windows.
	metadataServerIP          = "169.254.169.254"
	metadataHostAlias         = "metadata.google.internal"
	offGCPHostsPathLinux      = "/etc/otelcol-google/hosts"
	offGCPFirewallScriptLinux = "/etc/otelcol-google/block_metadata.sh"
	wifDirLinux               = "/etc/otelcol-google/wif"
	collectorUserLinux        = "otelcol-google"

	windowsStateDir              = `C:\ProgramData\Google\OpenTelemetry Collector`
	windowsConfigureRemoteScript = `C:\ProgramData\Google\OpenTelemetry Collector\configure_windows.ps1`
	offGCPHostsPathWindows       = `C:\ProgramData\Google\OpenTelemetry Collector\hosts`
	windowsSystemHostsPath       = `C:\Windows\System32\drivers\etc\hosts`
	wifDirWindows                = `C:\ProgramData\Google\OpenTelemetry Collector\wif`

	defaultWIFPoolID     = "gboc-x509-pool"
	defaultWIFProviderID = "gboc-x509-prov"
	defaultWIFCertSecret = "gboc-x509-leaf-cert"
	defaultWIFKeySecret  = "gboc-x509-leaf-key"
)

// droppedMessages are the sample logs that the PolicySet drops.
var droppedMessages = []string{
	"gboc-smoke=drop",
	"smoke-test=other",
}

func defaultWIFAudience(projectNumber string) string {
	return fmt.Sprintf("//iam.googleapis.com/projects/%s/locations/global/workloadIdentityPools/%s/providers/%s",
		projectNumber, defaultWIFPoolID, defaultWIFProviderID)
}

func telemetryPolicyLogPath(imageSpec string) string {
	if gce.IsWindows(imageSpec) {
		return telemetryPolicyLogDirWindows + `\` + activeLogsFileName
	}
	return telemetryPolicyLogDirLinux + "/" + activeLogsFileName
}

// telemetryPolicySettings holds the Telemetry Policy environment for the test.
type telemetryPolicySettings struct {
	cpEndpoint              string
	expectedRevision        string
	windowsExpectedRevision string
	fleetID                 string
	policySetID             string
	windowsFleetID          string
	windowsPolicySetID      string
	region                  string

	// Off-GCP mode only (OFF_GCP=true).
	offGCP        bool
	projectNumber string
	wifDir        string
	wifCertSecret string
	wifKeySecret  string
	wifAudience   string
}

func (s telemetryPolicySettings) fleetIDFor(imageSpec string) string {
	if gce.IsWindows(imageSpec) {
		return s.windowsFleetID
	}
	return s.fleetID
}

func (s telemetryPolicySettings) policySetIDFor(imageSpec string) string {
	if gce.IsWindows(imageSpec) {
		return s.windowsPolicySetID
	}
	return s.policySetID
}

func (s telemetryPolicySettings) expectedRevisionFor(imageSpec string) string {
	if gce.IsWindows(imageSpec) && s.windowsExpectedRevision != "" {
		return s.windowsExpectedRevision
	}
	return s.expectedRevision
}

func telemetryPolicySettingsFromEnv(t *testing.T) telemetryPolicySettings {
	t.Helper()
	s := telemetryPolicySettings{
		cpEndpoint:              os.Getenv("TELEMETRY_POLICY_CONTROL_PLANE_ENDPOINT"),
		expectedRevision:        os.Getenv("TELEMETRY_POLICY_EXPECTED_REVISION"),
		windowsExpectedRevision: os.Getenv("TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS"),
		fleetID:                 envOrDefault("FLEET_ID", defaultLinuxFleetID),
		policySetID:             envOrDefault("POLICY_SET_ID", defaultLinuxPolicySetID),
		windowsFleetID:          envOrDefault("WINDOWS_FLEET_ID", defaultWindowsFleetID),
		windowsPolicySetID:      envOrDefault("WINDOWS_POLICY_SET_ID", defaultWindowsPolicySetID),
		region:                  envOrDefault("REGION", "asia-east1"),
		offGCP:                  os.Getenv("OFF_GCP") == "true",
	}
	if s.offGCP {
		s.projectNumber = os.Getenv("PROJECT_NUMBER")
		s.wifDir = os.Getenv("WIF_DIR")
		s.wifCertSecret = envOrDefault("WIF_CERT_SECRET", defaultWIFCertSecret)
		s.wifKeySecret = envOrDefault("WIF_KEY_SECRET", defaultWIFKeySecret)
		s.wifAudience = envOrDefault("WIF_AUDIENCE", defaultWIFAudience(s.projectNumber))
	}
	if s.cpEndpoint == "" {
		t.Skip("TELEMETRY_POLICY_CONTROL_PLANE_ENDPOINT is not set; skipping Telemetry Policy smoke test")
	}
	if s.expectedRevision == "" && s.windowsExpectedRevision == "" {
		t.Fatal("TELEMETRY_POLICY_EXPECTED_REVISION (or TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS) must be set to the revision rolled out to the fleet")
	}
	if testRunID == "" {
		t.Fatal("This test does not support being run outside of Kokoro.")
	}
	if s.offGCP && s.projectNumber == "" {
		t.Fatal("OFF_GCP=true requires PROJECT_NUMBER")
	}
	return s
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// controlPlaneConfig returns the --config flag that loads policies from the
// Control Plane. On GCP, project and project_number are resolved from the GCE
// metadata server; off GCP, they are passed explicitly because metadata is blocked.
func (s telemetryPolicySettings) controlPlaneConfig(project, imageSpec string) string {
	uri := fmt.Sprintf("--config=googlecontrolplane:xds://%s?gcp.fleet_id=%s&region=%s",
		s.cpEndpoint, s.fleetIDFor(imageSpec), s.region)
	if s.offGCP {
		uri += "&project=" + project + "&project_number=" + s.projectNumber
	}
	return uri
}

// dropIn returns a systemd drop-in that runs otelcol-google strictly from the
// googlecontrolplane config source (without /etc/otelcol-google/config.yaml).
// When offGCP is set, ExecStartPre=+ runs the metadata-blocking firewall script
// as root on boot before the collector starts, BindReadOnlyPaths overrides
// /etc/hosts inside the service namespace so metadata.google.internal maps to
// 127.0.0.1, GOOGLE_APPLICATION_CREDENTIALS configures X.509 WIF, and
// GOOGLE_CLOUD_PROJECT supplies the auth project required by
// googleclientauth/default_gcp_destination for external_account credentials
// (project, project_number, and region on the xds:// URI take precedence for
// control-plane and resource fallback resolution).
func (s telemetryPolicySettings) dropIn(project, imageSpec string) string {
	var extraLines string
	if s.offGCP {
		extraLines = fmt.Sprintf(
			"ExecStartPre=+/bin/sh %s\nBindReadOnlyPaths=%s:/etc/hosts\nEnvironment=GOOGLE_APPLICATION_CREDENTIALS=%s/cred_config.json\nEnvironment=GOOGLE_CLOUD_PROJECT=%s\n",
			offGCPFirewallScriptLinux, offGCPHostsPathLinux, wifDirLinux, project)
	}
	return fmt.Sprintf("[Service]\n%sExecStart=\nExecStart=/usr/bin/otelcol-google %s\n",
		extraLines, s.controlPlaneConfig(project, imageSpec))
}

// filterOffGCPHosts returns a copy of hostsContent with any GCE metadata server
// mappings removed and metadata.google.internal (both with and without a
// trailing dot, for glibc libnss_files under cgo as well as Go's pure-Go and
// Windows resolvers) pointed at 127.0.0.1, so the DNS probe in
// cloud.google.com/go/compute/metadata.OnGCE() returns false.
func filterOffGCPHosts(hostsContent string) string {
	var b strings.Builder
	for _, rawLine := range strings.Split(strings.ReplaceAll(hostsContent, "\r\n", "\n"), "\n") {
		entry, _, _ := strings.Cut(rawLine, "#")
		if strings.Contains(entry, metadataServerIP) || strings.Contains(entry, metadataHostAlias) {
			continue
		}
		if rawLine == "" {
			continue
		}
		b.WriteString(rawLine + "\n")
	}
	b.WriteString("127.0.0.1 " + metadataHostAlias + " " + metadataHostAlias + ". metadata\n")
	return b.String()
}

// accessSecretBytes fetches the latest version of secretID in project from
// Cloud Secret Manager in memory without logging the secret payload.
func accessSecretBytes(ctx context.Context, svc *secretmanager.Service, project, secretID string) ([]byte, error) {
	name := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", project, secretID)
	resp, err := svc.Projects.Secrets.Versions.Access(name).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("accessing secret %s: %v", name, err)
	}
	if resp.Payload == nil {
		return nil, fmt.Errorf("secret %s returned an empty payload", name)
	}
	data, err := base64.StdEncoding.DecodeString(resp.Payload.Data)
	if err != nil {
		return nil, fmt.Errorf("decoding secret %s: %v", name, err)
	}
	return data, nil
}

// loadWIFLeafCertAndKey loads the X.509 workload certificate and private key
// from WIF_DIR when set, or from Secret Manager in project by default.
func loadWIFLeafCertAndKey(ctx context.Context, logger *log.Logger, project string, s telemetryPolicySettings) (certPEM, keyPEM []byte, err error) {
	if s.wifDir != "" {
		logger.Printf("Loading WIF workload certificate and key from WIF_DIR=%s", s.wifDir)
		certPEM, err = os.ReadFile(filepath.Join(s.wifDir, "leaf.pem"))
		if err != nil {
			return nil, nil, fmt.Errorf("reading WIF_DIR/leaf.pem: %v", err)
		}
		keyPEM, err = os.ReadFile(filepath.Join(s.wifDir, "leaf.key"))
		if err != nil {
			return nil, nil, fmt.Errorf("reading WIF_DIR/leaf.key: %v", err)
		}
		return certPEM, keyPEM, nil
	}
	logger.Printf("Fetching WIF workload certificate (%s) and key (%s) from Secret Manager in %s",
		s.wifCertSecret, s.wifKeySecret, project)
	svc, err := secretmanager.NewService(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("secretmanager.NewService: %v", err)
	}
	certPEM, err = accessSecretBytes(ctx, svc, project, s.wifCertSecret)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err = accessSecretBytes(ctx, svc, project, s.wifKeySecret)
	if err != nil {
		return nil, nil, err
	}
	return certPEM, keyPEM, nil
}

// buildWIFCredentialFiles builds the 4 WIF files (cert_config.json,
// cred_config.json, leaf.pem, leaf.key) for the given remote directory.
func buildWIFCredentialFiles(ctx context.Context, logger *log.Logger, project, remoteDir, sep string, s telemetryPolicySettings) (map[string][]byte, error) {
	certConfig, err := json.MarshalIndent(map[string]any{
		"cert_configs": map[string]any{
			"workload": map[string]string{
				"cert_path": remoteDir + sep + "leaf.pem",
				"key_path":  remoteDir + sep + "leaf.key",
			},
		},
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	credConfig, err := json.MarshalIndent(map[string]any{
		"universe_domain":    "googleapis.com",
		"type":               "external_account",
		"audience":           s.wifAudience,
		"subject_token_type": "urn:ietf:params:oauth:token-type:mtls",
		"token_url":          "https://sts.mtls.googleapis.com/v1/token",
		"credential_source": map[string]any{
			"certificate": map[string]string{
				"certificate_config_location": remoteDir + sep + "cert_config.json",
			},
		},
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	certPEM, keyPEM, err := loadWIFLeafCertAndKey(ctx, logger, project, s)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		"cert_config.json": certConfig,
		"cred_config.json": credConfig,
		"leaf.pem":         certPEM,
		"leaf.key":         keyPEM,
	}, nil
}

// writeRemoteFile writes data directly onto vm over SSH stdin (never touching GCS).
func writeRemoteFile(ctx context.Context, logger *log.Logger, vm *gce.VM, remotePath, linuxMode string, data []byte) error {
	if gce.IsWindows(vm.ImageSpec) {
		b64 := base64.StdEncoding.EncodeToString(data)
		cmd := fmt.Sprintf(
			"$ErrorActionPreference = 'Stop'; $p = '%s'; [System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($p)) | Out-Null; [System.IO.File]::WriteAllBytes($p, [System.Convert]::FromBase64String([Console]::In.ReadToEnd().Trim()))",
			remotePath)
		if _, err := gce.RunRemotelyStdin(ctx, logger, vm, strings.NewReader(b64), cmd); err != nil {
			return fmt.Errorf("writing %s: %v", remotePath, err)
		}
		return nil
	}
	cmd := fmt.Sprintf("sudo mkdir -p '%s' && sudo tee '%s' > /dev/null && sudo chmod %s '%s'",
		path.Dir(remotePath), remotePath, linuxMode, remotePath)
	if _, err := gce.RunRemotelyStdin(ctx, logger, vm, bytes.NewReader(data), cmd); err != nil {
		return fmt.Errorf("writing %s: %v", remotePath, err)
	}
	return nil
}

// stageWIFCredentials writes WIF credentials fetched from Secret Manager
// directly onto vm in a single SSH invocation (never touching GCS or command logs).
func stageWIFCredentials(ctx context.Context, logger *log.Logger, vm *gce.VM, s telemetryPolicySettings) error {
	if gce.IsWindows(vm.ImageSpec) {
		files, err := buildWIFCredentialFiles(ctx, logger, vm.Project, wifDirWindows, `\`, s)
		if err != nil {
			return err
		}
		encoded := make(map[string]string, len(files))
		for name, b := range files {
			encoded[name] = base64.StdEncoding.EncodeToString(b)
		}
		payload, err := json.Marshal(encoded)
		if err != nil {
			return err
		}
		cmd := fmt.Sprintf(
			"$ErrorActionPreference = 'Stop'; $dir = '%s'; New-Item -ItemType Directory -Force -Path $dir | Out-Null; "+
				"$m = [Console]::In.ReadToEnd() | ConvertFrom-Json; "+
				"foreach ($p in $m.PSObject.Properties) { [System.IO.File]::WriteAllBytes((Join-Path $dir $p.Name), [System.Convert]::FromBase64String($p.Value)) }; "+
				"icacls $dir /inheritance:r /grant:r 'SYSTEM:(OI)(CI)F' 'Administrators:(OI)(CI)F' | Out-Null; "+
				"if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }",
			wifDirWindows)
		_, err = gce.RunRemotelyStdin(ctx, logger, vm, bytes.NewReader(payload), cmd)
		return err
	}

	files, err := buildWIFCredentialFiles(ctx, logger, vm.Project, wifDirLinux, "/", s)
	if err != nil {
		return err
	}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for name, b := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o640,
			Size: int64(len(b)),
		}); err != nil {
			return err
		}
		if _, err := tw.Write(b); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	cmd := fmt.Sprintf(
		"sudo mkdir -p '%[1]s' && sudo tar -xf - -C '%[1]s' && sudo chown -R root:%[2]s '%[1]s' && sudo chmod 0750 '%[1]s'",
		wifDirLinux, collectorUserLinux)
	_, err = gce.RunRemotelyStdin(ctx, logger, vm, &archive, cmd)
	return err
}

// writeSampleLogs writes sampleLogsFile directly into the *.log path watched by
// filelog/googlecontrolplane over SSH after the PolicySet is active.
func writeSampleLogs(ctx context.Context, logger *log.Logger, vm *gce.VM, s telemetryPolicySettings) error {
	content, err := os.ReadFile(sampleLogsFile)
	if err != nil {
		return err
	}
	if s.offGCP {
		content = tagSampleLogs(content, vm)
	}
	return writeRemoteFile(ctx, logger, vm, telemetryPolicyLogPath(vm.ImageSpec), "0644", content)
}

// setupTelemetryPolicyCollector installs otelcol-google, writes scripts and (in
// off-GCP mode) filtered hosts and WIF credentials directly over SSH,
// optionally detaches the VM's service account and blocks metadata for off-GCP
// simulation, and restarts otelcol-google with --config=googlecontrolplane:xds://...
func setupTelemetryPolicyCollector(ctx context.Context, logger *log.Logger, vm *gce.VM, s telemetryPolicySettings) error {
	if err := installOtelCollector(ctx, logger, vm, locationFromEnvVars()); err != nil {
		return err
	}
	if gce.IsWindows(vm.ImageSpec) {
		// Give the collector time to start up, as setupOtelCollectorFrom does.
		time.Sleep(10 * time.Second)
		configureScript, err := os.ReadFile(configureWindowsLocalScript)
		if err != nil {
			return err
		}
		if err := writeRemoteFile(ctx, logger, vm, windowsConfigureRemoteScript, "", configureScript); err != nil {
			return err
		}
		args := fmt.Sprintf("-ControlPlaneConfig '%s' -LogDir '%s'", s.controlPlaneConfig(vm.Project, vm.ImageSpec), telemetryPolicyLogDirWindows)
		if s.offGCP {
			hostsOut, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("$ErrorActionPreference = 'Stop'; Get-Content -Raw -LiteralPath '%s'", windowsSystemHostsPath))
			if err != nil {
				return fmt.Errorf("reading %s: %v", windowsSystemHostsPath, err)
			}
			if err := writeRemoteFile(ctx, logger, vm, offGCPHostsPathWindows, "", []byte(filterOffGCPHosts(hostsOut.Stdout))); err != nil {
				return err
			}
			if err := stageWIFCredentials(ctx, logger, vm, s); err != nil {
				return err
			}
			args += fmt.Sprintf(" -OffGCP -WifCredConfig '%s\\cred_config.json' -Project '%s' -FilteredHostsPath '%s'",
				wifDirWindows, vm.Project, offGCPHostsPathWindows)
		}
		if _, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("powershell -ExecutionPolicy Bypass -File '%s' %s", windowsConfigureRemoteScript, args)); err != nil {
			return fmt.Errorf("configuring Windows collector service: %v", err)
		}
	} else {
		if _, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("sudo mkdir -p '%s'", telemetryPolicyLogDirLinux)); err != nil {
			return err
		}
		if s.offGCP {
			if _, err := gce.RunRemotely(ctx, logger, vm, "sudo systemctl stop otelcol-google"); err != nil {
				return err
			}
			ensureFirewallScript, err := os.ReadFile(ensureFirewallLocalScript)
			if err != nil {
				return err
			}
			if _, err := gce.RunRemotelyStdin(ctx, logger, vm, bytes.NewReader(ensureFirewallScript), "sudo sh -eu"); err != nil {
				return fmt.Errorf("ensuring firewall CLI is installed: %v", err)
			}
			hostsOut, err := gce.RunRemotely(ctx, logger, vm, "cat /etc/hosts")
			if err != nil {
				return fmt.Errorf("reading /etc/hosts: %v", err)
			}
			if err := writeRemoteFile(ctx, logger, vm, offGCPHostsPathLinux, "0644", []byte(filterOffGCPHosts(hostsOut.Stdout))); err != nil {
				return err
			}
			blockScript, err := os.ReadFile(blockMetadataLocalScript)
			if err != nil {
				return err
			}
			if err := writeRemoteFile(ctx, logger, vm, offGCPFirewallScriptLinux, "0755", blockScript); err != nil {
				return err
			}
			if err := stageWIFCredentials(ctx, logger, vm, s); err != nil {
				return err
			}
		}
		if err := writeRemoteFile(ctx, logger, vm, telemetryPolicyDropInPath, "0644", []byte(s.dropIn(vm.Project, vm.ImageSpec))); err != nil {
			return err
		}
		if _, err := gce.RunRemotely(ctx, logger, vm, "sudo systemctl daemon-reload"); err != nil {
			return err
		}
	}

	if s.offGCP {
		logger.Printf("Stopping %s, detaching service account, and restarting to simulate off-GCP host", vm.Name)
		if err := gce.StopInstance(ctx, logger, vm); err != nil {
			return fmt.Errorf("stopping VM before detaching service account: %v", err)
		}
		if err := gce.DetachServiceAccount(ctx, logger, vm); err != nil {
			return fmt.Errorf("detaching VM service account: %v", err)
		}
		if err := gce.StartInstance(ctx, logger, vm); err != nil {
			return fmt.Errorf("starting VM after detaching service account: %v", err)
		}
		if gce.IsWindows(vm.ImageSpec) {
			// Re-apply the filtered hosts file and flush DNS cache after reboot in
			// case GCEAgent refreshed hosts on boot before otelcol-google restarts.
			if _, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("$ErrorActionPreference = 'Stop'; Copy-Item -Force -LiteralPath '%s' -Destination '%s'; Clear-DnsClientCache",
				offGCPHostsPathWindows, windowsSystemHostsPath)); err != nil {
				return fmt.Errorf("refreshing Windows off-GCP hosts file after reboot: %v", err)
			}
		}
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

// revisionValue converts an 8-hex CCFE revision ID into the
// gcp.policy.set.active gauge value.
func revisionValue(rev string) (int64, error) {
	v, err := strconv.ParseUint(rev, 16, 32)
	if err != nil || len(rev) != 8 {
		return 0, fmt.Errorf("revision %q is not an 8-hex revision ID", rev)
	}
	return int64(v), nil
}

// lookupCollectorMetric is gce.lookupMetric scoped to a collector instead of a VM.
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

func waitForCollectorMetricSeries(ctx context.Context, logger *log.Logger, client *monitoring.MetricClient, vm *gce.VM, metric, collector string, window time.Duration) ([]*monitoringpb.TimeSeries, error) {
	for attempt := 1; attempt <= gce.QueryMaxAttempts; attempt++ {
		tsList, err := nonEmptySeries(logger, lookupCollectorMetric(ctx, client, vm, metric, collector, window))
		if len(tsList) > 0 && err == nil {
			return tsList, nil
		}
		if err != nil && !gce.IsRetriableLookupError(ctx, err) {
			return nil, fmt.Errorf("waitForCollectorMetricSeries(metric=%q, collector=%q): %v", metric, collector, err)
		}
		logger.Printf("waitForCollectorMetricSeries(metric=%q, collector=%q): request_error=%v, retrying (%d/%d)...", metric, collector, err, attempt, gce.QueryMaxAttempts)
		time.Sleep(queryBackoffDuration)
	}
	return nil, fmt.Errorf("waitForCollectorMetricSeries(metric=%q, collector=%q) failed: exhausted retries", metric, collector)
}

// waitForPolicySetActive waits until the collector's gcp.policy.set.active
// series reports the expected PolicySet and revision for vm's OS.
func waitForPolicySetActive(ctx context.Context, logger *log.Logger, vm *gce.VM, s telemetryPolicySettings, collector string) error {
	expectedRev := s.expectedRevisionFor(vm.ImageSpec)
	expectedPS := s.policySetIDFor(vm.ImageSpec)
	want, err := revisionValue(expectedRev)
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
			return fmt.Errorf("%v; see %s", err, collectorLogsFile(vm.ImageSpec))
		}
		for _, ts := range series {
			psID := ts.GetMetric().GetLabels()[policySetIDLabel]
			for _, p := range ts.GetPoints() {
				v := int64(p.GetValue().GetDoubleValue())
				if iv := p.GetValue().GetInt64Value(); iv != 0 {
					v = iv
				}
				if v == want && path.Base(psID) == expectedPS {
					logger.Printf("Found %s point for collector %s: %s=%q value=%d (revision %s)", policySetActiveMetric, collector, policySetIDLabel, psID, v, expectedRev)
					return nil
				}
				last = fmt.Sprintf("%s=%q value=%d", policySetIDLabel, psID, v)
			}
		}
		logger.Printf("Expected point not found yet (attempt %d/%d), latest: %s", attempt, queryMaxAttempts, last)
		time.Sleep(queryBackoffDuration)
	}
	return fmt.Errorf("no %s point for collector %s with PolicySet %q and value %d (revision %s); latest: %s; see %s",
		policySetActiveMetric, collector, expectedPS, want, expectedRev, last, collectorLogsFile(vm.ImageSpec))
}

// offGCPLogLine tags a sample log line with the VM's instance ID because off-GCP
// logs do not carry GCE resource.labels.instance_id.
func offGCPLogLine(line string, vm *gce.VM) string {
	return line + " vm=" + strconv.FormatInt(vm.ID, 10)
}

func tagSampleLogs(content []byte, vm *gce.VM) []byte {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n"), "\n") {
		b.WriteString(offGCPLogLine(line, vm) + "\n")
	}
	return []byte(b.String())
}

func collectorLogsFile(imageSpec string) string {
	if gce.IsWindows(imageSpec) {
		return "otelcol-google-logs.txt"
	}
	return "journalctl_otelcol-google.txt"
}

// messageQuery matches a Cloud Logging entry by exact textPayload.
func messageQuery(message string) string {
	return fmt.Sprintf("textPayload=%q", message)
}

// keptTransformedQuery matches the kept sample log AND verifies that
// logTransformPolicy added labels.gboc_smoke="true".
func keptTransformedQuery(message string) string {
	return fmt.Sprintf("textPayload=%q AND labels.%s=%q", message, transformedAttrKey, transformedAttrValue)
}

func countLogsMatchingFilter(ctx context.Context, logger *log.Logger, client *logadmin.Client, project, extraFilter string) (int, error) {
	filter := fmt.Sprintf(`logName:"projects/%s/logs/" AND timestamp > %q AND %s`,
		project, time.Now().Add(-logWindow).Format(time.RFC3339), extraFilter)
	logger.Println(filter)
	it := client.Entries(ctx, logadmin.Filter(filter))
	n := 0
	for {
		entry, err := it.Next()
		if err == iterator.Done {
			return n, nil
		}
		if err != nil {
			return 0, err
		}
		logger.Printf("Found matching log entry: %v", entry)
		n++
	}
}

func verifyLogsOffGCP(ctx context.Context, logger *log.Logger, vm *gce.VM) error {
	client, err := logadmin.NewClient(ctx, vm.Project)
	if err != nil {
		return fmt.Errorf("logadmin.NewClient(%s): %v", vm.Project, err)
	}
	defer client.Close()

	kept := offGCPLogLine(keptMessage, vm)
	found := false
	for attempt := 1; attempt <= gce.LogQueryMaxAttempts && !found; attempt++ {
		n, err := countLogsMatchingFilter(ctx, logger, client, vm.Project, keptTransformedQuery(kept))
		if err != nil {
			logger.Printf("Log query failed (attempt %d/%d): %v", attempt, gce.LogQueryMaxAttempts, err)
		}
		if found = n > 0; !found {
			time.Sleep(queryBackoffDuration)
		}
	}
	if !found {
		return fmt.Errorf("transformed kept log (%s) not found in Cloud Logging of %s; see %s",
			keptTransformedQuery(kept), vm.Project, collectorLogsFile(vm.ImageSpec))
	}
	for _, m := range droppedMessages {
		dropped := offGCPLogLine(m, vm)
		n, err := countLogsMatchingFilter(ctx, logger, client, vm.Project, messageQuery(dropped))
		if err != nil {
			return fmt.Errorf("querying log %q: %v", dropped, err)
		}
		if n > 0 {
			return fmt.Errorf("log %q should have been dropped by the PolicySet, found %d entries in %s", dropped, n, vm.Project)
		}
	}
	return nil
}

// verifyFilteredAndTransformedLogs checks that the kept sample log reached Cloud
// Logging with labels.gboc_smoke="true" (from logTransformPolicy) and that both
// dropped sample logs were filtered out (by logFilterPolicy).
func verifyFilteredAndTransformedLogs(ctx context.Context, logger *log.Logger, vm *gce.VM, s telemetryPolicySettings) error {
	if s.offGCP {
		return verifyLogsOffGCP(ctx, logger, vm)
	}
	if err := gce.WaitForLog(ctx, logger, vm, ".*", logWindow, keptTransformedQuery(keptMessage)); err != nil {
		return fmt.Errorf("transformed kept log (%s) not found in Cloud Logging: %v", keptTransformedQuery(keptMessage), err)
	}
	for _, m := range droppedMessages {
		if err := gce.AssertLogMissing(ctx, logger, vm, ".*", logWindow, messageQuery(m)); err != nil {
			return fmt.Errorf("log %q should have been dropped by the PolicySet: %v", m, err)
		}
	}
	return nil
}

func TestTelemetryPoliciesSmoke(t *testing.T) {
	t.Parallel()
	s := telemetryPolicySettingsFromEnv(t)

	gce.RunForEachImage(t, func(t *testing.T, imageSpec string) {
		t.Parallel()
		if s.expectedRevisionFor(imageSpec) == "" {
			t.Fatalf("missing expected revision for %s (set TELEMETRY_POLICY_EXPECTED_REVISION / TELEMETRY_POLICY_EXPECTED_REVISION_WINDOWS)", imageSpec)
		}
		// Both on-GCP and off-GCP share the same VM creation call; in off-GCP
		// mode setupTelemetryPolicyCollector subsequently stops the VM and
		// detaches its service account and scopes.
		ctx, dirLog, vm := commonSetupWithExtraCreateArgumentsAndMetadata(t, imageSpec, []string{"--scopes=cloud-platform"}, nil)
		logger := dirLog.ToMainLog()

		logger.Printf("Installing otelcol-google with config source %s", s.controlPlaneConfig(vm.Project, imageSpec))
		if err := setupTelemetryPolicyCollector(ctx, logger, vm, s); err != nil {
			t.Fatal(err)
		}
		collector, err := collectorID(ctx, logger, vm)
		if err != nil {
			t.Fatal(err)
		}
		logger.Printf("Waiting for collector %s to report PolicySet %s at revision %s",
			collector, s.policySetIDFor(imageSpec), s.expectedRevisionFor(imageSpec))
		if err := waitForPolicySetActive(ctx, logger, vm, s, collector); err != nil {
			t.Fatal(err)
		}
		// Write the sample logs only after the PolicySet is active so
		// filelog/googlecontrolplane (start_at: end) tails the newly created
		// test.log file through the active filter and transform policies.
		if err := writeSampleLogs(ctx, logger, vm, s); err != nil {
			t.Fatal(err)
		}
		if err := verifyFilteredAndTransformedLogs(ctx, logger, vm, s); err != nil {
			t.Fatal(err)
		}
	})
}

func TestFilterOffGCPHosts(t *testing.T) {
	in := "127.0.0.1\tlocalhost\r\n" +
		"::1\t\tlocalhost ip6-localhost ip6-loopback\r\n" +
		"10.140.0.12\tvm-1.asia-east1-a.c.off-gcp-test-project.internal vm-1  # Added by Google\r\n" +
		"# comment mentioning 169.254.169.254 is harmless\r\n" +
		"169.254.169.254 metadata.google.internal  # Added by Google\r\n" +
		"169.254.169.254 metadata\r\n"
	got := filterOffGCPHosts(in)
	if strings.Contains(got, "169.254.169.254 metadata") {
		t.Errorf("filterOffGCPHosts left GCE metadata IP mapping:\n%s", got)
	}
	if !strings.Contains(got, "10.140.0.12\tvm-1.asia-east1-a.c.off-gcp-test-project.internal vm-1") {
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
		t.Fatalf("unexpected off-GCP settings when OFF_GCP=true: %+v", offGCP)
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

	dropIn := s.dropIn("off-gcp-test-project", "debian-cloud:debian-12")
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
		"Environment=GOOGLE_CLOUD_PROJECT=off-gcp-test-project",
		"ExecStart=/usr/bin/otelcol-google --config=googlecontrolplane:xds://",
		"gcp.fleet_id=gboc-smoke-fleet",
		"region=asia-east1",
		"project=off-gcp-test-project",
		"project_number=406580743452",
	} {
		if !strings.Contains(dropIn, want) {
			t.Errorf("off-GCP dropIn missing %q:\n%s", want, dropIn)
		}
	}

	winURI := s.controlPlaneConfig("off-gcp-test-project", "windows-cloud:windows-2022")
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

	tagged := string(tagSampleLogs([]byte("gboc-smoke=keep\r\ngboc-smoke=drop\r\n"), &gce.VM{ID: 42}))
	wantTagged := "gboc-smoke=keep vm=42\ngboc-smoke=drop vm=42\n"
	if tagged != wantTagged {
		t.Errorf("tagSampleLogs = %q, want %q", tagged, wantTagged)
	}

	for _, script := range []string{ensureFirewallLocalScript, blockMetadataLocalScript, configureWindowsLocalScript} {
		if _, err := os.Stat(script); err != nil {
			t.Errorf("expected helper script %s to exist: %v", script, err)
		}
	}
}

func TestAccessSecretBytesAndLocalOverride(t *testing.T) {
	wantCert := "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/off-gcp-test-project/secrets/gboc-x509-leaf-cert/versions/latest:access" {
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
	got, err := accessSecretBytes(context.Background(), svc, "off-gcp-test-project", defaultWIFCertSecret)
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
	cert, key, err := loadWIFLeafCertAndKey(context.Background(), log.New(io.Discard, "", 0), "off-gcp-test-project", telemetryPolicySettings{wifDir: dir})
	if err != nil {
		t.Fatalf("loadWIFLeafCertAndKey(wifDir) failed: %v", err)
	}
	if string(cert) != "dir-cert" || string(key) != "dir-key" {
		t.Fatalf("unexpected local override pair: cert=%q key=%q", string(cert), string(key))
	}
}
