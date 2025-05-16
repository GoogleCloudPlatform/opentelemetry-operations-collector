// Copyright 2025 Google LLC
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

// Env variables:
// - ZONES
// - IMAGE_SPECS
// - PROJECT
// - OTELCOL_CONFIGS_DIR
// - _BUILD_ARTIFACTS_PACKAGE_GCS (optional)

package smoke

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/gce"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/logging"
)

const (
	resourceType = "gce_instance"
)

var (
	testRunID = os.Getenv("KOKORO_BUILD_ID")
)

// recommendedMachineType returns a reasonable setting for a VM's machine type
// (https://cloud.google.com/compute/docs/machine-types). Windows instances
// are configured to be larger because they need more CPUs to start up in a
// reasonable amount of time.
func recommendedMachineType(imageSpec string) string {
	if gce.IsWindows(imageSpec) {
		return "e2-standard-4"
	}
	if gce.IsARM(imageSpec) {
		return "t2a-standard-2"
	}
	return "e2-standard-2"
}

// collectorConfigPath returns the platform-specific filesystem location where
// the config is stored.
func collectorConfigPath(imageSpec string) string {
	if gce.IsWindows(imageSpec) {
		panic("Unimplemented call to collectorConfigPath for Windows")
	}
	return "/etc/otelcol-google/config.yaml"
}

// runDiagnostics will fetch as much debugging info as it can from the
// given VM.
// All the commands run and their output are dumped to various files in the
// directory managed by the given DirectoryLogger.
func runDiagnostics(ctx context.Context, logger *logging.DirectoryLogger, vm *gce.VM) {
	logger.ToMainLog().Printf("Starting runDiagnostics()...")
	if gce.IsWindows(vm.ImageSpec) {
		panic("Unimplemented call to runDiagnostics for Windows")
	}

	gce.RunRemotely(ctx, logger.ToFile("journalctl_otelcol-google.txt"), vm, "sudo journalctl -u otelcol-google")
	gce.RunRemotely(ctx, logger.ToFile("journalctl_full_output.txt"), vm, "sudo journalctl -xe")

	// This suffix helps Kokoro set the right Content-type for log files. See b/202432085.
	txtSuffix := ".txt"

	fileList := []string{
		gce.SyslogLocation(vm.ImageSpec),
		collectorConfigPath(vm.ImageSpec),
	}
	for _, log := range fileList {
		_, basename := path.Split(log)
		gce.RunRemotely(ctx, logger.ToFile(basename+txtSuffix), vm, "sudo cat "+log)
	}
}

// commonSetupWithExtraCreateArgumentsAndMetadata sets up the VM for testing with extra creation arguments for the `gcloud compute instances create` command and additional metadata.
func commonSetupWithExtraCreateArgumentsAndMetadata(t *testing.T, imageSpec string, extraCreateArguments []string, additionalMetadata map[string]string) (context.Context, *logging.DirectoryLogger, *gce.VM) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), gce.SuggestedTimeout)
	t.Cleanup(cancel)
	gcloudConfigDir := t.TempDir()
	if err := gce.SetupGcloudConfigDir(ctx, gcloudConfigDir); err != nil {
		t.Fatalf("Unable to set up a gcloud config directory: %v", err)
	}
	ctx = gce.WithGcloudConfigDir(ctx, gcloudConfigDir)

	logger := gce.SetupLogger(t)
	logger.ToMainLog().Println("Calling SetupVM(). For details, see VM_initialization.txt.")
	options := gce.VMOptions{
		ImageSpec:            imageSpec,
		TimeToLive:           "3h",
		MachineType:          recommendedMachineType(imageSpec),
		ExtraCreateArguments: extraCreateArguments,
		Metadata:             additionalMetadata,
	}
	vm := gce.SetupVM(ctx, t, logger.ToFile("VM_initialization.txt"), options)
	logger.ToMainLog().Printf("VM is ready: %#v", vm)
	t.Cleanup(func() {
		runDiagnostics(ctx, logger, vm)
	})
	return ctx, logger, vm
}

// PackageLocation describes a location where packages live.
// It could someday grow to include Artifact Registry locations,
// but for now it just represents a GCS bucket location.
type PackageLocation struct {
	// If provided, a URL for a directory in GCS containing .deb/.rpm/.goo files
	// to install on the testing VMs.
	packagesInGCS string
}

// locationFromEnvVars assembles a PackageLocation from environment variables.
func locationFromEnvVars() PackageLocation {
	return PackageLocation{
		packagesInGCS: os.Getenv("_BUILD_ARTIFACTS_PACKAGE_GCS"),
	}
}

func restartCommandForPlatform(platform string) string {
	if gce.IsWindows(platform) {
		panic("Unimplemented call to restartCommandForPlatform for Windows")
	}
	return "sudo systemctl restart otelcol-google"
}

// isRPMBased checks if the image spec is RPM based.
func isRPMBased(imageSpec string) bool {
	return strings.HasPrefix(imageSpec, "centos-cloud") ||
		strings.HasPrefix(imageSpec, "rhel-") ||
		strings.HasPrefix(imageSpec, "rocky-linux-cloud") ||
		strings.HasPrefix(imageSpec, "suse-cloud") ||
		strings.HasPrefix(imageSpec, "suse-sap-cloud") ||
		strings.HasPrefix(imageSpec, "opensuse-cloud") ||
		strings.Contains(imageSpec, "sles-")
}

// Installs the collector package from GCS (see packagesInGCS) onto the given Windows VM.
func installWindowsPackageFromGCS(ctx context.Context, logger *log.Logger, vm *gce.VM, gcsPath string) error {
	if _, err := gce.RunRemotely(ctx, logger, vm, "New-Item -ItemType directory -Path C:\\collectorUpload"); err != nil {
		return err
	}
	if _, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("gsutil cp -r %s/*.goo C:\\collectorUpload", gcsPath)); err != nil {
		return fmt.Errorf("error copying down collector package from GCS: %v", err)
	}
	if _, err := gce.RunRemotely(ctx, logger, vm, "googet -noconfirm -verbose install -reinstall (Get-ChildItem C:\\collectorUpload\\*.goo | Select-Object -Expand FullName)"); err != nil {
		return fmt.Errorf("error installing collector from .goo file: %v", err)
	}
	return nil
}

// installPackageFromGCS installs the collector package from GCS onto the given Linux VM.
//
// gcsPath must point to a GCS Path that contains .deb/.rpm/.goo files to install on the testing VMs.
func installPackageFromGCS(ctx context.Context, logger *log.Logger, vm *gce.VM, gcsPath string) error {
	if gce.IsWindows(vm.ImageSpec) {
		return installWindowsPackageFromGCS(ctx, logger, vm, gcsPath)
	}
	if _, err := gce.RunRemotely(ctx, logger, vm, "mkdir -p /tmp/collectorUpload"); err != nil {
		return err
	}
	if err := gce.InstallGsutilIfNeeded(ctx, logger, vm); err != nil {
		return err
	}

	unameOutput, err := gce.RunRemotely(ctx, logger, vm, "uname --machine")
	if err != nil {
		return err
	}
	arch := ""
	switch strings.TrimSpace(unameOutput.Stdout) {
	case "x86_64":
		arch = "amd64"
	case "aarch64":
		arch = "arm64"
	default:
		return fmt.Errorf("Couldn't look up corresponding GOARCH for uname output %q", strings.TrimSpace(unameOutput.Stdout))
	}

	ext := ".deb"
	if isRPMBased(vm.ImageSpec) {
		ext = ".rpm"
	}
	pkgSelector := gcsPath + "/*" + arch + ext
	if _, err := gce.RunRemotely(ctx, logger, vm, "sudo gsutil cp -r "+pkgSelector+" /tmp/collectorUpload"); err != nil {
		return fmt.Errorf("error copying down collector package from GCS: %v", err)
	}
	// Print the contents of /tmp/collectorUpload into the logs.
	if _, err := gce.RunRemotely(ctx, logger, vm, "ls /tmp/collectorUpload"); err != nil {
		return err
	}
	if isRPMBased(vm.ImageSpec) {
		if _, err := gce.RunRemotely(ctx, logger, vm, "sudo rpm --upgrade -v --force /tmp/collectorUpload/*"); err != nil {
			return fmt.Errorf("error installing collector from .rpm file: %v", err)
		}
		return nil
	}
	// --allow-downgrades is marked as dangerous, but I don't see another way
	// to get the following sequence to work:
	// 1. install stable package from Artifact Registry
	// 2. install just-built package from GCS
	// Nor do I know why apt considers that sequence to be a downgrade.
	if _, err := gce.RunRemotely(ctx, logger, vm, "sudo apt-get install --allow-downgrades --yes --verbose-versions /tmp/collectorUpload/*"); err != nil {
		return fmt.Errorf("error installing collector from .deb file: %v", err)
	}
	return nil
}

// installOtelCollector installs the Otel collector on the given VM. Consults the given
// PackageLocation to determine where to install it from. For details
// about PackageLocation, see the documentation for the PackageLocation struct.
func installOtelCollector(ctx context.Context, logger *log.Logger, vm *gce.VM, location PackageLocation) error {
	if location.packagesInGCS != "" {
		return installPackageFromGCS(ctx, logger, vm, location.packagesInGCS)
	}
	return errors.New("Unimplemented handling of empty value for location.packagesInGCS (_BUILD_ARTIFACTS_PACKAGE_GCS)")
}

// restartOtelCollector restarts the collector and waits for it to become available.
func restartOtelCollector(ctx context.Context, logger *log.Logger, vm *gce.VM) error {
	if _, err := gce.RunRemotely(ctx, logger, vm, restartCommandForPlatform(vm.ImageSpec)); err != nil {
		return fmt.Errorf("restartOtelCollector() failed to restart otel collector: %v", err)
	}
	// Give it a bit of time to shut down.
	time.Sleep(5 * time.Second)
	return nil
}

// setupOtelCollectorFrom is an overload of setupOtelCollector that allows the callsite to
// decide which version of the collector gets installed.
func setupOtelCollectorFrom(ctx context.Context, logger *log.Logger, vm *gce.VM, config string, location PackageLocation) error {
	if err := installOtelCollector(ctx, logger, vm, location); err != nil {
		return err
	}
	startupDelay := 20 * time.Second
	if len(config) > 0 {
		if gce.IsWindows(vm.ImageSpec) {
			// Sleep to avoid some flaky errors when restarting the collector because the
			// services have not fully started up yet.
			time.Sleep(startupDelay)
		}
		if err := gce.UploadContent(ctx, logger, vm, strings.NewReader(config), collectorConfigPath(vm.ImageSpec)); err != nil {
			return fmt.Errorf("setupOtelCollectorFrom() failed to upload config file: %v", err)
		}
	}

	// The collector only needs a restart if the config is not empty.
	if len(config) > 0 {
		return restartOtelCollector(ctx, logger, vm)
	}
	// Give the collector time to start up.
	time.Sleep(startupDelay)
	return nil
}

// setupOtelCollector installs the Otel collector with the given config (leave it empty for the default config).
// The version of the collector to install is determined by _BUILD_ARTIFACTS_PACKAGE_GCS.
func setupOtelCollector(ctx context.Context, logger *log.Logger, vm *gce.VM, config string) error {
	return setupOtelCollectorFrom(ctx, logger, vm, config, locationFromEnvVars())
}

func MetricsTest(ctx context.Context, t *testing.T, logger *log.Logger, vm *gce.VM) {
	representativeMetric := "workload.googleapis.com/otelcol_exporter_sent_metric_points"

	window := 10 * time.Minute
	filters := []string{
		fmt.Sprintf("resource.type = %q", resourceType),
		fmt.Sprintf("metric.labels.otelcol_google_e2e = %q", testRunID),
	}
	_, err := gce.WaitForMetric(ctx, logger, vm, representativeMetric, window, filters, false /*isPrometheus*/)
	if err != nil {
		logger.Printf("Could not find representative metric: %q", representativeMetric)
		t.Fatal(err)
	}
	logger.Print("Found metric; subtest complete.")
}

func LoggingTest(ctx context.Context, t *testing.T, logger *log.Logger, vm *gce.VM) {
	window := 10 * time.Minute
	query := fmt.Sprintf(`resource.type="%s" AND labels.otelcol_google_e2e="%s"`, resourceType, testRunID)
	if err := gce.WaitForLog(ctx, logger, vm, "google-otelcol-smoke-test", window, query); err != nil {
		t.Fatal(err)
	}
	logger.Print("Found log; subtest complete.")
}

func TracesTest(ctx context.Context, t *testing.T, logger *log.Logger, vm *gce.VM) {
	options := gce.WaitForTraceOptions{
		Window:  10 * time.Minute,
		Filters: []string{fmt.Sprintf("+otelcol_google_e2e:%s", testRunID)},
	}
	trace, err := gce.WaitForTrace(ctx, logger, vm, options)
	if err != nil {
		t.Fatalf("Could not find any matching traces: %v", err)
	}
	logger.Printf("Found trace, subtest complete: %+v", trace)
}

// getSmokeOtelcolConfig reads smoke.yaml from OTEL_CONFIGS_DIR and returns it
// after substituting in TestRunID.
func getSmokeOtelcolConfig(t *testing.T) string {
	configDir := os.Getenv("OTELCOL_CONFIGS_DIR")
	if configDir == "" {
		t.Fatal("Must pass nonempty value for OTELCOL_CONFIGS_DIR")
	}
	configPath := path.Join(configDir, "smoke.yaml")
	t.Logf("Reading otelcol config from %q", configPath)

	temp, err := template.New("smoke.yaml").ParseFiles(configPath)
	if err != nil {
		t.Fatal(err)
	}

	type Data struct {
		TestRunID string
	}
	data := Data{
		TestRunID: os.Getenv("KOKORO_BUILD_ID"),
	}
	if data.TestRunID == "" {
		t.Fatal("This test does not support being run outside of Kokoro.")
	}
	var builder strings.Builder
	if err := temp.Execute(&builder, data); err != nil {
		t.Fatal(err)
	}

	return builder.String()
}

func TestSmoke(t *testing.T) {
	t.Parallel()

	config := getSmokeOtelcolConfig(t)

	gce.RunForEachImage(t, func(t *testing.T, imageSpec string) {
		t.Parallel()
		if gce.IsWindows(imageSpec) {
			t.SkipNow()
		}
		ctx, dirLog, vm := commonSetupWithExtraCreateArgumentsAndMetadata(t, imageSpec, nil, nil)
		logger := dirLog.ToMainLog()

		logger.Printf("Installing otelcol with the following config: \n%s", config)
		if err := setupOtelCollector(ctx, logger, vm, config); err != nil {
			t.Fatal(err)
		}

		t.Run("metrics", func(t *testing.T) {
			t.Parallel()
			MetricsTest(ctx, t, logger, vm)
		})
		t.Run("logging", func(t *testing.T) {
			t.Parallel()
			LoggingTest(ctx, t, logger, vm)
		})
		t.Run("traces", func(t *testing.T) {
			t.Parallel()
			TracesTest(ctx, t, logger, vm)
		})
	})
}
