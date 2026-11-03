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

// Off-GCP mode of TestTangoSmoke (TANGO_OFF_GCP=true).
//
// The VM has no service account and no OAuth scopes, and the collector runs
// with the GCE metadata server disabled, so it has to authenticate with X.509
// Workload Identity Federation. Because the VM has no credentials either, the
// test cannot use the GCS-based gce.UploadContent or installPackageFromGCS:
// files are streamed over SSH instead, and the package is downloaded on the
// test runner first.

package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/logging/logadmin"
	"google.golang.org/api/iterator"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/gce"
)

const (
	// disabledMetadataHost points the collector's GCE metadata clients at an
	// address where nothing listens, so it behaves as if it were off GCP.
	disabledMetadataHost = "127.0.0.1:1"

	// defaultWIFAudience is the X.509 WIF provider in gboc-tango-testing (see
	// tango/README.md).
	defaultWIFAudience = "//iam.googleapis.com/projects/406580743452/locations/global/workloadIdentityPools/gboc-x509-pool/providers/gboc-x509-prov"

	// wifDirLinux and wifDirWindows hold the workload certificate, its key,
	// and the certificate and credential configs on the VM.
	wifDirLinux   = "/etc/otelcol-google/wif"
	wifDirWindows = `C:\ProgramData\gboc-smoke-wif`

	// collectorUser is the user the packaged Linux service runs as.
	collectorUser = "otelcol-google"
)

// wifPath returns the path of the named WIF file on the VM.
func wifPath(windows bool, name string) string {
	if windows {
		return wifDirWindows + `\` + name
	}
	return wifDirLinux + "/" + name
}

// wifCredConfigPath returns the path of the WIF credential config on the VM,
// which GOOGLE_APPLICATION_CREDENTIALS points at.
func wifCredConfigPath(windows bool) string {
	return wifPath(windows, "cred_config.json")
}

// uploadContent writes content to remotePath on vm. Off GCP it streams the
// content over SSH, because the VM has no credentials to read the transfers
// bucket that gce.UploadContent goes through.
func (s tangoSettings) uploadContent(ctx context.Context, logger *log.Logger, vm *gce.VM, content io.Reader, remotePath string) error {
	if !s.offGCP {
		return gce.UploadContent(ctx, logger, vm, content, remotePath)
	}
	if gce.IsWindows(vm.ImageSpec) {
		return fmt.Errorf("uploading %s: off-GCP mode does not support Windows yet", remotePath)
	}
	_, err := gce.RunRemotelyStdin(ctx, logger, vm, content,
		fmt.Sprintf("sudo mkdir -p '%s' && sudo tee '%s' > /dev/null", path.Dir(remotePath), remotePath))
	return err
}

// installOtelCollectorOffGCP is installPackageFromGCS for a VM without
// credentials: the test runner downloads the package and streams it to the VM.
func installOtelCollectorOffGCP(ctx context.Context, logger *log.Logger, vm *gce.VM, location PackageLocation) error {
	if location.packagesInGCS == "" {
		return fmt.Errorf("_BUILD_ARTIFACTS_PACKAGE_GCS must be set")
	}
	out, err := gce.RunRemotely(ctx, logger, vm, "uname --machine")
	if err != nil {
		return err
	}
	var arch string
	switch strings.TrimSpace(out.Stdout) {
	case "x86_64":
		arch = "amd64"
	case "aarch64":
		arch = "arm64"
	default:
		return fmt.Errorf("couldn't look up GOARCH for uname output %q", strings.TrimSpace(out.Stdout))
	}
	ext := ".deb"
	if isRPMBased(vm.ImageSpec) {
		ext = ".rpm"
	}

	localDir, err := os.MkdirTemp("", "tango-offgcp-package-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(localDir)
	if _, err := gce.RunGcloud(ctx, logger, "", []string{"storage", "cp", location.packagesInGCS + "/*" + arch + ext, localDir}); err != nil {
		return fmt.Errorf("error downloading the collector package from GCS: %v", err)
	}
	packages, err := filepath.Glob(filepath.Join(localDir, "*"+ext))
	if err != nil || len(packages) == 0 {
		return fmt.Errorf("no *%s%s package found in %s (err=%v)", arch, ext, location.packagesInGCS, err)
	}
	for _, p := range packages {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, err = gce.RunRemotelyStdin(ctx, logger, vm, f,
			fmt.Sprintf("mkdir -p /tmp/collectorUpload && cat > '/tmp/collectorUpload/%s'", filepath.Base(p)))
		f.Close()
		if err != nil {
			return fmt.Errorf("error streaming %s to the VM: %v", filepath.Base(p), err)
		}
	}
	if isRPMBased(vm.ImageSpec) {
		_, err = gce.RunRemotely(ctx, logger, vm, "sudo rpm --upgrade -v --force /tmp/collectorUpload/*")
	} else {
		_, err = gce.RunRemotely(ctx, logger, vm, "sudo apt-get install --allow-downgrades --yes --verbose-versions /tmp/collectorUpload/*")
	}
	if err != nil {
		return fmt.Errorf("error installing the collector package: %v", err)
	}
	return nil
}

// uploadWIFCredentials uploads the workload certificate and key from
// TANGO_WIF_DIR and generates the certificate and credential configs that
// point at them on the VM. The configs are generated here rather than copied
// so they never reference paths on the test runner.
func uploadWIFCredentials(ctx context.Context, logger *log.Logger, vm *gce.VM, s tangoSettings) error {
	windows := gce.IsWindows(vm.ImageSpec)
	certConfig, err := json.MarshalIndent(map[string]any{
		"cert_configs": map[string]any{
			"workload": map[string]string{
				"cert_path": wifPath(windows, "leaf.pem"),
				"key_path":  wifPath(windows, "leaf.key"),
			},
		},
	}, "", "  ")
	if err != nil {
		return err
	}
	credConfig, err := json.MarshalIndent(map[string]any{
		"universe_domain":    "googleapis.com",
		"type":               "external_account",
		"audience":           s.wifAudience,
		"subject_token_type": "urn:ietf:params:oauth:token-type:mtls",
		"token_url":          "https://sts.mtls.googleapis.com/v1/token",
		"credential_source": map[string]any{
			"certificate": map[string]string{
				"certificate_config_location": wifPath(windows, "cert_config.json"),
			},
		},
	}, "", "  ")
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"cert_config.json": certConfig,
		"cred_config.json": credConfig,
	}
	for _, name := range []string{"leaf.pem", "leaf.key"} {
		b, err := os.ReadFile(filepath.Join(s.wifDir, name))
		if err != nil {
			return fmt.Errorf("reading TANGO_WIF_DIR: %v", err)
		}
		files[name] = b
	}
	for name, b := range files {
		if err := s.uploadContent(ctx, logger, vm, bytes.NewReader(b), wifPath(windows, name)); err != nil {
			return fmt.Errorf("failed to upload %s: %v", name, err)
		}
	}
	// Only root and the collector may read the private key. The files are
	// named explicitly: once the directory is restricted, the SSH user's
	// shell can no longer expand a glob in it.
	var paths []string
	for name := range files {
		paths = append(paths, "'"+wifPath(windows, name)+"'")
	}
	_, err = gce.RunRemotely(ctx, logger, vm, fmt.Sprintf(
		"sudo chown -R root:%[1]s '%[2]s' && sudo chmod 0750 '%[2]s' && sudo chmod 0640 %[3]s",
		collectorUser, wifDirLinux, strings.Join(paths, " ")))
	return err
}

// offGCPLogLine tags a sample log line with the VM's instance ID. Off GCP, the
// collector cannot detect the GCE resource, so its logs do not carry the
// instance_id label that gce.WaitForLog scopes queries to; the tag makes the
// lines unique to this VM instead. A numeric ID cannot change which filter a
// line matches.
func offGCPLogLine(line string, vm *gce.VM) string {
	return line + " vm=" + strconv.FormatInt(vm.ID, 10)
}

// tagSampleLogs applies offGCPLogLine to every line of content.
func tagSampleLogs(content []byte, vm *gce.VM) []byte {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(string(content), "\n"), "\n") {
		b.WriteString(offGCPLogLine(line, vm) + "\n")
	}
	return []byte(b.String())
}

// countLogsInProject counts the log entries in project whose textPayload is
// exactly message, over the trailing logWindow.
func countLogsInProject(ctx context.Context, logger *log.Logger, client *logadmin.Client, project, message string) (int, error) {
	filter := fmt.Sprintf(`logName:"projects/%s/logs/" AND timestamp > %q AND textPayload=%q`,
		project, time.Now().Add(-logWindow).Format(time.RFC3339), message)
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

// verifyFilteredLogsOffGCP is verifyFilteredLogs for off-GCP mode: it queries
// the tagged sample logs in TANGO_PROJECT, where the collector sends them.
func verifyFilteredLogsOffGCP(ctx context.Context, logger *log.Logger, vm *gce.VM, s tangoSettings) error {
	project := s.telemetryProject(vm)
	client, err := logadmin.NewClient(ctx, project)
	if err != nil {
		return fmt.Errorf("logadmin.NewClient(%s): %v", project, err)
	}
	defer client.Close()

	kept := offGCPLogLine(keptMessage, vm)
	found := false
	for attempt := 1; attempt <= gce.LogQueryMaxAttempts && !found; attempt++ {
		n, err := countLogsInProject(ctx, logger, client, project, kept)
		if err != nil {
			logger.Printf("Log query failed (attempt %d/%d): %v", attempt, gce.LogQueryMaxAttempts, err)
		}
		if found = n > 0; !found {
			time.Sleep(queryBackoffDuration)
		}
	}
	if !found {
		return fmt.Errorf("log %q, which the PolicySet keeps, not found in Cloud Logging of %s; see %s", kept, project, collectorLogsFile(vm.ImageSpec))
	}
	// The lines were written together, so the dropped ones would have arrived
	// by now.
	for _, m := range droppedMessages {
		dropped := offGCPLogLine(m, vm)
		n, err := countLogsInProject(ctx, logger, client, project, dropped)
		if err != nil {
			return fmt.Errorf("querying log %q: %v", dropped, err)
		}
		if n > 0 {
			return fmt.Errorf("log %q should have been dropped by the PolicySet, found %d entries in %s", dropped, n, project)
		}
	}
	return nil
}
