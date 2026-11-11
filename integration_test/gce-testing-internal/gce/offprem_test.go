// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gce

import (
	"bytes"
	"errors"
	"log"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestParseDpkgStatus(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []string
		wantErr  bool
	}{
		{
			// The output on a new Debian 12 VM.
			name: "installed packages are listed, names that dpkg only knows about are not",
			output: `installed gce-disk-expand
installed google-cloud-cli
not-installed google-cloud-cli-app-engine-java
not-installed google-cloud-cli-app-engine-python
not-installed google-cloud-cli-bigtable-emulator
not-installed google-cloud-cli-datastore-emulator
not-installed google-cloud-cli-pubsub-emulator
installed google-cloud-packages-archive-keyring
not-installed google-cloud-sdk
not-installed google-compute-daemon
installed google-compute-engine
not-installed google-compute-engine-init-jessie
not-installed google-compute-engine-init-stretch
not-installed google-compute-engine-jessie
installed google-compute-engine-oslogin
not-installed google-compute-engine-stretch
not-installed google-config-jessie
not-installed google-config-stretch
installed google-guest-agent
installed google-osconfig-agent
not-installed google-startup-scripts
`,
			expected: []string{
				"gce-disk-expand",
				"google-cloud-cli",
				"google-cloud-packages-archive-keyring",
				"google-compute-engine",
				"google-compute-engine-oslogin",
				"google-guest-agent",
				"google-osconfig-agent",
			},
		},
		{
			name: "only names that dpkg knows about",
			output: `not-installed google-cloud-sdk
not-installed google-compute-daemon
not-installed google-startup-scripts
`,
			expected: nil,
		},
		{
			// After a purge, dpkg-query prints nothing but complaints on its
			// standard error.
			name:     "no package matched",
			output:   "",
			expected: nil,
		},
		{
			name: "packages that are not fully installed count as installed",
			output: `half-configured google-guest-agent
half-installed google-compute-engine
triggers-pending google-cloud-cli
unpacked google-osconfig-agent
`,
			expected: []string{"google-cloud-cli", "google-compute-engine", "google-guest-agent", "google-osconfig-agent"},
		},
		{
			name: "removed packages with configuration files left behind are not installed",
			output: `config-files google-fluentd
installed google-guest-agent
`,
			expected: []string{"google-guest-agent"},
		},
		{
			name: "result is sorted",
			output: `installed google-guest-agent
installed gce-disk-expand
`,
			expected: []string{"gce-disk-expand", "google-guest-agent"},
		},
		{
			name:     "architecture-qualified name",
			output:   "installed google-example:amd64\n",
			expected: []string{"google-example:amd64"},
		},
		{
			name:    "line without a package name",
			output:  "installed\n",
			wantErr: true,
		},
		{
			name:    "line with too many fields",
			output:  "install ok installed google-guest-agent\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDpkgStatus(tc.output)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseDpkgStatus(%q) error = %v, wantErr = %v", tc.output, err, tc.wantErr)
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("parseDpkgStatus(%q) = %v, want %v", tc.output, got, tc.expected)
			}
		})
	}
}

func TestParseUnitList(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []string
		wantErr  bool
	}{
		{
			// The output on a new Debian 12 VM.
			name: "loaded units are listed whatever their activity",
			output: `google-disk-expand.service          loaded inactive dead    Resize root filesystem if needed
google-guest-agent-manager.service  loaded active   running Google Compute Engine Guest Agent Plugin Manager
google-guest-compat-manager.service loaded active   running Google Compute Engine Guest Compat Manager
google-osconfig-agent.service       loaded active   running Google OSConfig Agent
google-oslogin-cache.service        loaded inactive dead    NSS cache refresh
google-shutdown-scripts.service     loaded active   exited  Google Compute Engine Shutdown Scripts
google-startup-scripts.service      loaded inactive dead    Google Compute Engine Startup Scripts
google-oslogin-cache.timer          loaded active   waiting NSS cache refresh timer
`,
			expected: []string{
				"google-disk-expand.service",
				"google-guest-agent-manager.service",
				"google-guest-compat-manager.service",
				"google-osconfig-agent.service",
				"google-oslogin-cache.service",
				"google-oslogin-cache.timer",
				"google-shutdown-scripts.service",
				"google-startup-scripts.service",
			},
		},
		{
			name: "units without a unit file are skipped",
			output: `google-guest-agent-manager.service loaded    active   running Google Compute Engine Guest Agent Plugin Manager
google-network-setup.service       not-found inactive dead    google-network-setup.service
`,
			expected: []string{"google-guest-agent-manager.service"},
		},
		{
			// The output on the same VM once its guest environment is gone.
			name:     "masked units are skipped",
			output:   "google-osconfig-agent.service masked inactive dead google-osconfig-agent.service\n",
			expected: nil,
		},
		{
			name:     "no unit matched",
			output:   "",
			expected: nil,
		},
		{
			name:     "escaped unit name",
			output:   `google-startup\x2dscripts.service loaded active running Escaped` + "\n",
			expected: []string{`google-startup\x2dscripts.service`},
		},
		{
			name:    "line without a load state",
			output:  "google-guest-agent.service\n",
			wantErr: true,
		},
		{
			name:    "line that does not start with a unit name",
			output:  "● google-guest-agent.service loaded failed failed Google Compute Engine Guest Agent\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUnitList(tc.output)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseUnitList(%q) error = %v, wantErr = %v", tc.output, err, tc.wantErr)
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("parseUnitList(%q) = %v, want %v", tc.output, got, tc.expected)
			}
		})
	}
}

func TestDetachedScriptFinished(t *testing.T) {
	tests := []struct {
		name         string
		subState     string
		wantFinished bool
		wantErr      bool
	}{
		{name: "script is running", subState: "running"},
		{name: "script is being started", subState: "start"},
		{name: "script exited with status 0", subState: "exited", wantFinished: true},
		{name: "script failed", subState: "failed", wantFinished: true, wantErr: true},
		{name: "unit is not loaded", subState: "dead", wantFinished: true, wantErr: true},
		{name: "no state reported", subState: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			finished, err := detachedScriptFinished(tc.subState)
			if finished != tc.wantFinished {
				t.Errorf("detachedScriptFinished(%q) finished = %v, want %v", tc.subState, finished, tc.wantFinished)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("detachedScriptFinished(%q) error = %v, wantErr = %v", tc.subState, err, tc.wantErr)
			}
		})
	}
}

func TestSimulateOffPremUnsupportedOS(t *testing.T) {
	tests := []struct {
		name      string
		imageSpec string
	}{
		{name: "windows", imageSpec: "windows-cloud:windows-2022"},
		{name: "rhel", imageSpec: "rhel-cloud:rhel-9"},
		{name: "rocky linux", imageSpec: "rocky-linux-cloud:rocky-linux-8"},
		{name: "sles", imageSpec: "suse-cloud:sles-15"},
		{name: "unknown os family", imageSpec: "cos-cloud:cos-stable"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.New(&bytes.Buffer{}, "", 0)
			// The VM has no address: nothing may be run on it.
			vm := &VM{Name: "test-vm", ImageSpec: tc.imageSpec}

			err := SimulateOffPrem(t.Context(), logger, vm, OffPremOptions{RemoveGuestEnvironment: true})
			if !errors.Is(err, ErrOffPremUnsupported) {
				t.Fatalf("SimulateOffPrem() on %q returned %v, want an error wrapping ErrOffPremUnsupported", tc.imageSpec, err)
			}
			if !strings.Contains(err.Error(), tc.imageSpec) {
				t.Errorf("SimulateOffPrem() on %q returned %q, which does not name the image spec", tc.imageSpec, err)
			}
		})
	}
}

func TestSimulateOffPremWithoutOptions(t *testing.T) {
	logger := log.New(&bytes.Buffer{}, "", 0)
	// Nothing is asked for, so nothing is done, not even on a VM that
	// SimulateOffPrem cannot change.
	vm := &VM{Name: "test-vm", ImageSpec: "windows-cloud:windows-2022"}

	if err := SimulateOffPrem(t.Context(), logger, vm, OffPremOptions{}); err != nil {
		t.Errorf("SimulateOffPrem() with no options set returned %v, want nil", err)
	}
}

func TestRemovalScript(t *testing.T) {
	tests := []struct {
		name     string
		list     removalList
		sudoUser string
		expected string
	}{
		{
			// What is found on a new Debian 12 VM.
			name: "units and packages",
			list: removalList{
				packages: []string{
					"gce-disk-expand",
					"google-cloud-cli",
					"google-cloud-packages-archive-keyring",
					"google-compute-engine",
					"google-compute-engine-oslogin",
					"google-guest-agent",
					"google-osconfig-agent",
				},
				units: []string{
					"google-disk-expand.service",
					"google-guest-agent-manager.service",
					"google-guest-compat-manager.service",
					"google-osconfig-agent.service",
					"google-oslogin-cache.service",
					"google-oslogin-cache.timer",
					"google-shutdown-scripts.service",
					"google-startup-scripts.service",
				},
			},
			sudoUser: "test_user",
			expected: `#!/bin/bash
# Removes the Google guest environment from this host. Rendered for this host
# by the GCE testing library: the units and packages below are the ones that
# were found on it.
set -euxo pipefail
export DEBIAN_FRONTEND=noninteractive

# Give the user that the tests log in as a sudo rule of its own. The one it
# has comes from the guest agent, through the google-sudoers group, and must
# not be counted on once the agent is gone.
sudoers="$(mktemp)"
printf '%s\n' 'test_user ALL=(ALL:ALL) NOPASSWD:ALL' > "${sudoers}"
visudo -c -f "${sudoers}"
install --owner=root --group=root --mode=0440 "${sudoers}" /etc/sudoers.d/90-test-user
rm --force "${sudoers}"

# Stop the guest environment's units and mask them, so that nothing starts
# them again, neither while their packages are removed nor afterwards.
systemctl stop -- 'google-disk-expand.service' 'google-guest-agent-manager.service' 'google-guest-compat-manager.service' 'google-osconfig-agent.service' 'google-oslogin-cache.service' 'google-oslogin-cache.timer' 'google-shutdown-scripts.service' 'google-startup-scripts.service'
systemctl mask -- 'google-disk-expand.service' 'google-guest-agent-manager.service' 'google-guest-compat-manager.service' 'google-osconfig-agent.service' 'google-oslogin-cache.service' 'google-oslogin-cache.timer' 'google-shutdown-scripts.service' 'google-startup-scripts.service'

# Purge the packages, and only these: what they depend on stays installed.
apt-get --quiet --yes --option DPkg::Lock::Timeout=300 --option APT::Get::AutomaticRemove=false purge -- 'gce-disk-expand' 'google-cloud-cli' 'google-cloud-packages-archive-keyring' 'google-compute-engine' 'google-compute-engine-oslogin' 'google-guest-agent' 'google-osconfig-agent'

# Remove the apt sources of the guest environment: the files that are named
# after it, and any other file that points at Google's package repositories.
for source in /etc/apt/sources.list.d/*; do
  if [[ ! -f "${source}" ]]; then
    continue
  fi
  if [[ "${source##*/}" == google* ]] || grep --quiet --fixed-strings packages.cloud.google.com "${source}"; then
    rm --force --verbose "${source}"
  fi
done
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := removalScript(tc.list, tc.sudoUser)
			if got != tc.expected {
				t.Errorf("removalScript(%+v, %q) =\n%s\nwant\n%s", tc.list, tc.sudoUser, got, tc.expected)
			}
			checkBashSyntax(t, got)
		})
	}
}

// With nothing to stop or purge, the script still sets up sudo and removes the
// apt sources, and has no command left that needs a unit or a package.
func TestRemovalScriptForEmptyList(t *testing.T) {
	got := removalScript(removalList{}, "test_user")

	for _, want := range []string{
		`'test_user ALL=(ALL:ALL) NOPASSWD:ALL'`,
		" /etc/sudoers.d/90-test-user\n",
		"for source in /etc/apt/sources.list.d/*; do\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("removalScript() for an empty list does not contain %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"systemctl", "apt-get"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("removalScript() for an empty list contains %q:\n%s", unwanted, got)
		}
	}
	checkBashSyntax(t, got)
}

// Names reach the script as single shell words, whatever characters they
// contain.
func TestRemovalScriptQuotesNames(t *testing.T) {
	list := removalList{
		packages: []string{"google-example:amd64"},
		units:    []string{`google-startup\x2dscripts.service`},
	}
	got := removalScript(list, "o'brien")

	for _, want := range []string{
		`printf '%s\n' 'o'\''brien ALL=(ALL:ALL) NOPASSWD:ALL' > "${sudoers}"`,
		`systemctl stop -- 'google-startup\x2dscripts.service'` + "\n",
		`systemctl mask -- 'google-startup\x2dscripts.service'` + "\n",
		` purge -- 'google-example:amd64'` + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("removalScript(%+v, %q) does not contain %q:\n%s", list, "o'brien", want, got)
		}
	}
	checkBashSyntax(t, got)
}

// checkBashSyntax fails the test if bash cannot parse script. The script is
// not run.
func checkBashSyntax(t *testing.T, script string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Logf("Not checking the syntax of the script: %v", err)
		return
	}
	cmd := exec.Command(bash, "-n")
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("bash -n rejected the script: %v\n%s\nscript:\n%s", err, output, script)
	}
}
