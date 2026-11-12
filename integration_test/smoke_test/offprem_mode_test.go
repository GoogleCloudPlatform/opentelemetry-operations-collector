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
Off-prem mode: the tests of this package, on a VM that resembles a machine
outside GCE.

OFFPREM_MODE turns the mode on for every test of the package. The tests and
their names are the same in both modes. Two helpers make the difference:

  - installOtelCollector (in smoke_test.go) changes the VM with
    gce.SimulateOffPrem, after the VM has downloaded the collector package
    and before the package is installed.
  - uploadContent sends files to the VM over ssh, because the VM has no
    gcloud from then on.

The mode is also checked and recorded. requireOffPremEnv fails a test whose
environment the mode cannot work with, before the test creates a VM.
offPremLabels and logOffPremMode write the mode to the VM and to the log of
the test.

If OFFPREM_MODE is not set, the tests do what they did before the mode
existed. See tango/README.md for what the mode does and does not prove.
*/

package smoke

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/gce"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/logging"
)

const (
	// offPremModeGuestEnv is the OFFPREM_MODE that removes the Google guest
	// environment from the VM, gcloud included (see gce.OffPremOptions), and
	// keeps the metadata server and the VM's service account.
	offPremModeGuestEnv = "guest-env"
	// offPremModeLabel is the label of a VM that was created in off-prem
	// mode. Its value is the mode.
	offPremModeLabel = "offprem_mode"
	// offPremLogFile is the file, in the log directory of a test, that holds
	// what gce.SimulateOffPrem did to the VM, including the journal of the
	// removal.
	offPremLogFile = "simulate_off_prem.txt"
)

// offPremMode is OFFPREM_MODE: the mode that the tests of this package run
// in, or "" if they run without off-prem mode. It is read once, here.
var offPremMode = os.Getenv("OFFPREM_MODE")

// offPremOptions returns what offPremMode asks gce.SimulateOffPrem to change
// on a VM. It returns an error if offPremMode is not a mode that this package
// supports.
func offPremOptions() (gce.OffPremOptions, error) {
	switch offPremMode {
	case offPremModeGuestEnv:
		return gce.OffPremOptions{RemoveGuestEnvironment: true}, nil
	default:
		return gce.OffPremOptions{}, fmt.Errorf("OFFPREM_MODE=%q is not supported; the only supported mode is %q", offPremMode, offPremModeGuestEnv)
	}
}

// requireOffPremEnv fails the test if off-prem mode is on and cannot work: if
// OFFPREM_MODE is not a supported mode, or if environment variables that the
// mode needs are not set. One failure names all of these variables that are
// missing. Every test needs the location of the collector package, the id of
// the test run, PROJECT and IMAGE_SPECS; testVars lists the variables that
// the calling test needs on top of them.
//
// A test calls it before it reads its environment. So in off-prem mode the
// test fails before it creates a VM, and it fails where it would otherwise be
// skipped. Without off-prem mode it does nothing: the tests then find out
// about a missing variable as they always did, some of them only after they
// have created a VM.
//
// PROJECT and IMAGE_SPECS are read by the gce package. They are checked here
// so that the one failure names them too. The gce package reports a missing
// IMAGE_SPECS only when the test reaches gce.RunForEachImage. It does not
// report a missing PROJECT at all: creating the VM then fails with an error
// that does not name it.
//
// ZONES cannot be checked here: the gce package reads it when the test binary
// starts, and exits if it is empty.
func requireOffPremEnv(t *testing.T, testVars ...string) {
	t.Helper()
	if offPremMode == "" {
		return
	}
	if _, err := offPremOptions(); err != nil {
		t.Fatal(err)
	}
	names := append([]string{"_BUILD_ARTIFACTS_PACKAGE_GCS", "KOKORO_BUILD_ID"}, testVars...)
	names = append(names, "PROJECT", "IMAGE_SPECS")
	var missing []string
	for _, name := range names {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("OFFPREM_MODE is set, so %s must be set as well; see tango/README.md", strings.Join(missing, ", "))
	}
}

// offPremLabels returns the label that marks a VM as created in off-prem
// mode, and nil without off-prem mode. The name of a test is the same in both
// modes, so the VM says which one it was created in.
func offPremLabels() map[string]string {
	if offPremMode == "" {
		return nil
	}
	return map[string]string{offPremModeLabel: offPremMode}
}

// logOffPremMode writes the off-prem mode to the main log of a test, and
// nothing without off-prem mode. The name of a test is the same in both
// modes, so its log directory says which one it ran in.
func logOffPremMode(dirLog *logging.DirectoryLogger) {
	if offPremMode == "" {
		return
	}
	dirLog.ToMainLog().Printf("OFFPREM_MODE=%s: this test runs in off-prem mode. The VM is changed after it has downloaded the collector package and before the package is installed; see %s. From then on, files are uploaded over ssh.", offPremMode, offPremLogFile)
}

// dirLogKey is the context key of the DirectoryLogger of a test. See
// withDirLog.
type dirLogKey struct{}

// withDirLog returns a context that carries the DirectoryLogger of a test.
// A helper that takes a *log.Logger gets only the main log of the test. With
// this context, it can write a log file of its own into the log directory of
// the test, as simulateOffPrem does.
func withDirLog(ctx context.Context, dirLog *logging.DirectoryLogger) context.Context {
	return context.WithValue(ctx, dirLogKey{}, dirLog)
}

// simulateOffPrem changes vm as offPremMode says, with gce.SimulateOffPrem.
// What that does to the VM is written to offPremLogFile in the log directory
// of the test, and logger gets a line that points there. If ctx does not
// carry the DirectoryLogger of the test (see withDirLog), all of it is
// written to logger instead.
func simulateOffPrem(ctx context.Context, logger *log.Logger, vm *gce.VM) error {
	options, err := offPremOptions()
	if err != nil {
		return err
	}
	offPremLogger := logger
	if dirLog, ok := ctx.Value(dirLogKey{}).(*logging.DirectoryLogger); ok && dirLog != nil {
		logger.Printf("Calling SimulateOffPrem() with %+v. For details, see %s.", options, offPremLogFile)
		offPremLogger = dirLog.ToFile(offPremLogFile)
	}
	return gce.SimulateOffPrem(ctx, offPremLogger, vm, options)
}

// uploadContent uploads content as a file to remotePath on vm, with
// gce.UploadContent. In off-prem mode it uses gce.UploadContentSSH instead:
// gce.UploadContent needs gcloud on the VM, and installOtelCollector has
// removed it by then. The tests upload their files through this function,
// with one exception: the script that gce.RunScriptRemotely sends to a
// Windows VM goes through GCS in both modes (see setupTangoCollector).
func uploadContent(ctx context.Context, logger *log.Logger, vm *gce.VM, content io.Reader, remotePath string) error {
	if offPremMode != "" {
		return gce.UploadContentSSH(ctx, logger, vm, content, remotePath)
	}
	return gce.UploadContent(ctx, logger, vm, content, remotePath)
}
