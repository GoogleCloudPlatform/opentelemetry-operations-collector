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
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	secretmanager "google.golang.org/api/secretmanager/v1"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/gce"
)

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

func setupWindowsCollector(ctx context.Context, logger *log.Logger, vm *gce.VM, s telemetryPolicySettings) error {
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
		if err := writeRemoteFile(ctx, logger, vm, windowsSystemHostsPath, "", []byte(filterOffGCPHosts(hostsOut.Stdout))); err != nil {
			return err
		}
		if _, err := gce.RunRemotely(ctx, logger, vm, "$ErrorActionPreference = 'Stop'; Clear-DnsClientCache"); err != nil {
			return fmt.Errorf("clearing Windows DNS client cache: %v", err)
		}
		if err := stageWIFCredentials(ctx, logger, vm, s); err != nil {
			return err
		}
		args += fmt.Sprintf(` -OffGCP -WifCredConfig '%s\cred_config.json' -Project '%s'`, wifDirWindows, vm.Project)
	}
	if _, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("powershell -ExecutionPolicy Bypass -File '%s' %s", windowsConfigureRemoteScript, args)); err != nil {
		return fmt.Errorf("configuring Windows collector service: %v", err)
	}
	return nil
}

func setupLinuxCollector(ctx context.Context, logger *log.Logger, vm *gce.VM, s telemetryPolicySettings) error {
	if _, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("sudo mkdir -p '%s'", telemetryPolicyLogDirLinux)); err != nil {
		return err
	}
	if s.offGCP {
		if _, err := gce.RunRemotely(ctx, logger, vm, "sudo systemctl stop otelcol-google"); err != nil {
			return err
		}
		ensureNetfilterScript, err := os.ReadFile(ensureNetfilterLocalScript)
		if err != nil {
			return err
		}
		if _, err := gce.RunRemotelyStdin(ctx, logger, vm, bytes.NewReader(ensureNetfilterScript), "sudo sh -eu"); err != nil {
			return fmt.Errorf("ensuring netfilter CLI is installed: %v", err)
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
		if err := writeRemoteFile(ctx, logger, vm, offGCPNetfilterScriptLinux, "0755", blockScript); err != nil {
			return err
		}
		if err := stageWIFCredentials(ctx, logger, vm, s); err != nil {
			return err
		}
	}
	if err := writeRemoteFile(ctx, logger, vm, telemetryPolicyDropInPath, "0644", []byte(s.dropIn(vm.Project, vm.ImageSpec))); err != nil {
		return err
	}
	_, err := gce.RunRemotely(ctx, logger, vm, "sudo systemctl daemon-reload")
	return err
}

// setupTelemetryPolicyCollector installs otelcol-google, configures the service
// (plus off-GCP metadata blocks and WIF credentials when s.offGCP is set), and
// restarts otelcol-google with --config=googlecontrolplane:xds://...
func setupTelemetryPolicyCollector(ctx context.Context, logger *log.Logger, vm *gce.VM, s telemetryPolicySettings) error {
	if err := installOtelCollectorWithTransferMode(ctx, logger, vm, locationFromEnvVars(), s.offGCP); err != nil {
		return err
	}
	if gce.IsWindows(vm.ImageSpec) {
		if err := setupWindowsCollector(ctx, logger, vm, s); err != nil {
			return err
		}
	} else {
		if err := setupLinuxCollector(ctx, logger, vm, s); err != nil {
			return err
		}
	}
	return restartOtelCollector(ctx, logger, vm)
}
