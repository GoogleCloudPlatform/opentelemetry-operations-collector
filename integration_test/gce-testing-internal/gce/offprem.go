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
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/google/uuid"
	"go.uber.org/multierr"
)

// OffPremOptions selects what SimulateOffPrem changes on a VM. The zero value
// changes nothing.
type OffPremOptions struct {
	// RemoveGuestEnvironment removes the Google guest environment from the
	// VM: the guest agent, OS Login, the OS Config agent, the GCE
	// configuration and disk expansion packages and the Google Cloud CLI,
	// along with the package repositories that they are installed from.
	RemoveGuestEnvironment bool
}

// ErrOffPremUnsupported is wrapped by the error that SimulateOffPrem returns
// for a VM whose operating system it cannot change yet.
var ErrOffPremUnsupported = errors.New("not supported yet")

// SimulateOffPrem changes a VM so that it resembles a machine that does not
// run on GCE, as far as options ask for.
//
// Call it on a freshly created VM, before installing what is under test, so
// that the test models a machine that never had the guest environment. With
// RemoveGuestEnvironment, every installed google-* and gce-* package is
// removed, whatever it is, and every loaded google* systemd unit is stopped
// and left masked.
//
// Afterwards the VM has no gcloud, so files have to reach it with
// UploadContentSSH instead of UploadContent. It is still a GCE VM in every
// other respect: in particular, the metadata server stays reachable and the
// VM keeps its service account.
//
// Everything that is done to the VM is written to logger, including the
// journal of the removal. Pass a logger that writes into the log directory of
// the test.
func SimulateOffPrem(ctx context.Context, logger *log.Logger, vm *VM, options OffPremOptions) error {
	if options.RemoveGuestEnvironment {
		if err := removeGuestEnvironment(ctx, logger, vm); err != nil {
			return fmt.Errorf("SimulateOffPrem() failed on %s: %w", vm.Name, err)
		}
	}
	return nil
}

// removeGuestEnvironment removes the guest environment from vm with the
// implementation for the package manager of its OS family.
func removeGuestEnvironment(ctx context.Context, logger *log.Logger, vm *VM) error {
	switch {
	case IsWindows(vm.ImageSpec):
		// The package manager is googet. An implementation has to keep ssh
		// working: on Windows test VMs, sshd itself is a guest environment
		// package (see addFrameworkMetadata), which has to stay or be
		// replaced.
		return fmt.Errorf("removing the guest environment from Windows VMs (image spec %q) is %w", vm.ImageSpec, ErrOffPremUnsupported)
	case IsDebianBased(vm.ImageSpec):
		return removeGuestEnvironmentApt(ctx, logger, vm)
	case IsRpm(vm.ImageSpec):
		// The package managers are dnf, yum and zypper.
		return fmt.Errorf("removing the guest environment from RPM-based Linux VMs (image spec %q) is %w", vm.ImageSpec, ErrOffPremUnsupported)
	default:
		return fmt.Errorf("removing the guest environment from VMs with image spec %q is %w", vm.ImageSpec, ErrOffPremUnsupported)
	}
}

// removeGuestEnvironmentApt removes the guest environment from a VM whose
// packages are managed with dpkg and apt. It has been verified on Debian 12
// only. It does not remove snaps, which is how some Ubuntu images ship gcloud
// (see verifyGcloudInstallation).
func removeGuestEnvironmentApt(ctx context.Context, logger *log.Logger, vm *VM) error {
	list, err := readRemovalList(ctx, logger, vm)
	if err != nil {
		return fmt.Errorf("could not list the guest environment: %w", err)
	}
	logger.Printf("Guest environment found on %s: packages %v, systemd units %v", vm.Name, list.packages, list.units)

	script := removalScript(list, sshUserName)
	logger.Printf("Removing it with this script:\n%s", script)
	if err := runScriptDetached(ctx, logger, vm, "remove-guest-environment", script); err != nil {
		return err
	}
	return checkGuestEnvironmentRemoved(ctx, logger, vm)
}

// removalList is what removing the guest environment takes off one VM. It is
// read from the VM rather than hard-coded, because images differ in what they
// ship and can come with other Google agents installed.
type removalList struct {
	// packages are the installed google-* and gce-* packages, to be purged.
	packages []string
	// units are the loaded google* systemd units, to be stopped and masked.
	units []string
}

// readRemovalList asks vm what there is to remove.
func readRemovalList(ctx context.Context, logger *log.Logger, vm *VM) (removalList, error) {
	packages, err := installedGooglePackages(ctx, logger, vm)
	if err != nil {
		return removalList{}, err
	}
	unitList, err := RunRemotely(ctx, logger, vm, unitListCommand)
	if err != nil {
		return removalList{}, err
	}
	units, err := parseUnitList(unitList.Stdout)
	if err != nil {
		return removalList{}, err
	}
	return removalList{packages: packages, units: units}, nil
}

// installedGooglePackages asks vm for its installed google-* and gce-*
// packages.
func installedGooglePackages(ctx context.Context, logger *log.Logger, vm *VM) ([]string, error) {
	dpkgStatus, err := RunRemotely(ctx, logger, vm, dpkgStatusCommand)
	if err != nil {
		return nil, err
	}
	return parseDpkgStatus(dpkgStatus.Stdout)
}

// dpkgStatusCommand prints one "<status> <package>" line for every Google
// package that dpkg knows about, installed or not; see parseDpkgStatus.
// dpkg-query exits with status 1 when a pattern matches no package, which is
// not a failure here.
const dpkgStatusCommand = `dpkg-query --show --showformat='${db:Status-Status} ${binary:Package}\n' 'google-*' 'gce-*' || [ $? -eq 1 ]`

// parseDpkgStatus returns the installed packages in the output of
// dpkgStatusCommand, sorted by name.
//
// dpkg also lists packages that are not installed, which are left out: names
// that it merely knows about, for example because another package refers to
// them or because they were purged ("not-installed"), and packages that were
// removed with their configuration files left behind ("config-files"). A
// package in any other state has files on the system, even if it is not
// completely installed or configured, and counts as installed.
func parseDpkgStatus(output string) ([]string, error) {
	var packages []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("unexpected line %q in the output of dpkg-query, want \"<status> <package>\"", line)
		}
		switch status, name := fields[0], fields[1]; status {
		case "not-installed", "config-files":
			// Not installed.
		default:
			packages = append(packages, name)
		}
	}
	slices.Sort(packages)
	return packages, nil
}

// unitListCommand prints one "<unit> <load state> ..." line for every Google
// systemd unit that is loaded or referenced by a loaded unit; see
// parseUnitList.
const unitListCommand = `systemctl list-units --all --plain --no-legend --no-pager 'google*'`

// parseUnitList returns the units in the output of unitListCommand that
// systemd has loaded from a unit file, running or not, sorted by name.
//
// Units in any other load state are left out. Most of them are "not-found":
// names that a loaded unit refers to, with no unit file behind them. There is
// nothing to stop in such a unit, and systemctl refuses to try.
func parseUnitList(output string) ([]string, error) {
	var units []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Every unit name ends in a "." and the type of the unit.
		if len(fields) < 2 || !strings.Contains(fields[0], ".") {
			return nil, fmt.Errorf("unexpected line %q in the output of systemctl list-units, want \"<unit> <load state> ...\"", line)
		}
		if unit, loadState := fields[0], fields[1]; loadState == "loaded" {
			units = append(units, unit)
		}
	}
	slices.Sort(units)
	return units, nil
}

const (
	// sudoersDropInPath is the sudoers drop-in that removalScript writes.
	sudoersDropInPath = "/etc/sudoers.d/90-test-user"

	// dpkgLockTimeoutSeconds is how long apt-get waits for another package
	// manager run to finish. Shortly after boot there often is one.
	dpkgLockTimeoutSeconds = 300
)

// removalScript renders the script that removes list from a Debian-family
// host. It must run as root.
//
// The script first gives sudoUser a sudo rule of its own, so that its sudo
// access no longer depends on the group and the sudoers file that the guest
// agent set up for it. Only the packages in list are purged: there is
// deliberately no autoremove, which would also take away the packages that
// the guest environment pulled in, rsyslog among them.
func removalScript(list removalList, sudoUser string) string {
	var script strings.Builder
	script.WriteString(`#!/bin/bash
# Removes the Google guest environment from this host. Rendered for this host
# by the GCE testing library: the units and packages below are the ones that
# were found on it.
set -euxo pipefail
export DEBIAN_FRONTEND=noninteractive

# Give the user that the tests log in as a sudo rule of its own. The one it
# has comes from the guest agent, through the google-sudoers group, and must
# not be counted on once the agent is gone.
sudoers="$(mktemp)"
printf '%s\n' ` + shellQuote(sudoUser+" ALL=(ALL:ALL) NOPASSWD:ALL") + ` > "${sudoers}"
visudo -c -f "${sudoers}"
install --owner=root --group=root --mode=0440 "${sudoers}" ` + sudoersDropInPath + `
rm --force "${sudoers}"
`)
	if len(list.units) > 0 {
		units := shellQuoteAll(list.units)
		script.WriteString(`
# Stop the guest environment's units and mask them, so that nothing starts
# them again, neither while their packages are removed nor afterwards.
systemctl stop -- ` + units + `
systemctl mask -- ` + units + `
`)
	}
	if len(list.packages) > 0 {
		script.WriteString(`
# Purge the packages, and only these: what they depend on stays installed.
apt-get --quiet --yes --option DPkg::Lock::Timeout=` + strconv.Itoa(dpkgLockTimeoutSeconds) + ` --option APT::Get::AutomaticRemove=false purge -- ` + shellQuoteAll(list.packages) + `
`)
	}
	script.WriteString(`
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
`)
	return script.String()
}

const (
	// detachedScriptTimeout is how long runScriptDetached waits for the
	// script to finish.
	detachedScriptTimeout = 15 * time.Minute
	// detachedScriptPollInterval is the pause between two looks at the
	// script's unit.
	detachedScriptPollInterval = 5 * time.Second
)

// runScriptDetached runs script as root on vm, waits for it to finish and
// writes its journal to logger. name describes the script and starts the name
// of the systemd unit that it runs under.
//
// Unlike with RunScriptRemotely, the script does not run inside an ssh
// session but under a transient systemd unit, which is polled. A dropped
// connection therefore cannot stop the script halfway, and the script is free
// to change things that ssh sessions depend on.
func runScriptDetached(ctx context.Context, logger *log.Logger, vm *VM, name, script string) error {
	// A new unit for every run: a transient unit cannot be started twice.
	unit := name + "-" + uuid.NewString()
	service := unit + ".service"
	// On a tmpfs that only root can write to.
	scriptPath := "/run/" + unit + ".sh"
	// A shell test that succeeds while systemd has the unit.
	unitIsLoaded := `[ "$(systemctl show --property=LoadState --value ` + shellQuote(service) + `)" = loaded ]`

	if err := UploadContentSSH(ctx, logger, vm, strings.NewReader(script), scriptPath); err != nil {
		return err
	}
	// --remain-after-exit keeps the unit around once the script has exited,
	// so that the outcome can still be asked for.
	//
	// RunRemotely repeats a command whose connection dropped, also when the
	// command did run. The second systemd-run then fails because the unit
	// already exists, which is fine: the unit is all that is asked for.
	start := "sudo systemd-run --unit=" + shellQuote(unit) + " --remain-after-exit /bin/bash " + shellQuote(scriptPath) + " || " + unitIsLoaded
	if _, err := RunRemotely(ctx, logger, vm, start); err != nil {
		return fmt.Errorf("could not start %s: %w", service, err)
	}

	waitErr := waitForDetachedScript(ctx, logger, vm, service)

	// The journal is the only record of what the script did, so save it
	// whatever the outcome, and in full: --all keeps journalctl from
	// replacing lines that hold control characters. Reading the journal
	// takes sudo, unless the user is in a group that may read it; try both in
	// case sudo is what broke.
	logger.Printf("Journal of %s follows.", service)
	journal := "journalctl --no-pager --all --unit=" + shellQuote(service)
	if _, err := RunRemotely(ctx, logger, vm, "sudo -n "+journal+" || "+journal); err != nil {
		logger.Printf("Could not read the journal of %s: %v", service, err)
	}
	if waitErr != nil {
		// The unit and the script are left in place for debugging.
		return fmt.Errorf("%s did not finish successfully, see its journal: %w", service, waitErr)
	}

	// Stopping the unit makes systemd forget it. As above, this can run a
	// second time, when the unit is already gone.
	//
	// A failure is logged and no more: the script has done its work, and what
	// stays behind is harmless. If the script left sudo broken, it is for the
	// caller's checks of the VM to say so, not for this cleanup.
	cleanUp := "{ sudo systemctl stop " + shellQuote(service) + " || ! " + unitIsLoaded + "; } && sudo rm --force " + shellQuote(scriptPath)
	if _, err := RunRemotely(ctx, logger, vm, cleanUp); err != nil {
		logger.Printf("Could not clean up after %s: %v", service, err)
	}
	return nil
}

// waitForDetachedScript polls the unit of a script started by
// runScriptDetached until the script has finished. It returns an error if the
// script did not succeed or did not finish in time.
func waitForDetachedScript(ctx context.Context, logger *log.Logger, vm *VM, service string) error {
	ctx, cancel := context.WithTimeout(ctx, detachedScriptTimeout)
	defer cancel()

	poll := func() error {
		// This needs no sudo, which the script may be reconfiguring.
		output, err := RunRemotely(ctx, logger, vm, "systemctl show --property=SubState --value "+shellQuote(service))
		if err != nil {
			if ctx.Err() != nil {
				return backoff.Permanent(ctx.Err())
			}
			if isSSHTransportError(err) {
				// The script runs on regardless of the connection. Ask again.
				return err
			}
			return backoff.Permanent(err)
		}
		subState := strings.TrimSpace(output.Stdout)
		finished, err := detachedScriptFinished(subState)
		if err != nil {
			return backoff.Permanent(err)
		}
		if !finished {
			return fmt.Errorf("%s is still in state %q", service, subState)
		}
		return nil
	}
	backoffPolicy := backoff.WithContext(backoff.NewConstantBackOff(detachedScriptPollInterval), ctx)
	return backoff.Retry(poll, backoffPolicy)
}

// detachedScriptFinished interprets the SubState property of the unit of a
// script started by runScriptDetached. It returns whether the script has
// finished, and an error if it finished without succeeding.
func detachedScriptFinished(subState string) (bool, error) {
	switch subState {
	case "exited":
		// The script exited with status 0, and --remain-after-exit kept the
		// unit.
		return true, nil
	case "failed":
		return true, errors.New("the script failed")
	case "dead":
		// A unit that systemd has not loaded is reported as dead, too.
		return true, errors.New("the unit of the script is gone, as after a reboot, so the outcome of the script is unknown")
	default:
		return false, nil
	}
}

// checkGuestEnvironmentRemoved verifies that vm has no guest environment
// left and is still usable by tests. It returns an error naming every
// post-condition that does not hold.
//
// Each check runs in an ssh session of its own, opened after the removal: a
// session from before would still hold the groups and environment that the
// test user had at the time.
func checkGuestEnvironmentRemoved(ctx context.Context, logger *log.Logger, vm *VM) error {
	postConditions := []struct {
		name  string
		check func(context.Context, *log.Logger, *VM) error
	}{
		{"sudo works for " + sshUserName, checkSudoWorks},
		{"no google-* or gce-* package is installed", checkNoGooglePackages},
		{"gcloud is gone", checkNoGcloud},
		{"the system is running or degraded", checkSystemRunning},
	}
	var violations error
	for _, postCondition := range postConditions {
		if err := postCondition.check(ctx, logger, vm); err != nil {
			logger.Printf("Post-condition violated: %s: %v", postCondition.name, err)
			violations = multierr.Append(violations, fmt.Errorf("post-condition violated: %s: %w", postCondition.name, err))
			continue
		}
		logger.Printf("Post-condition holds: %s", postCondition.name)
	}
	return violations
}

func checkSudoWorks(ctx context.Context, logger *log.Logger, vm *VM) error {
	_, err := RunRemotely(ctx, logger, vm, "sudo -n true")
	return err
}

func checkNoGooglePackages(ctx context.Context, logger *log.Logger, vm *VM) error {
	packages, err := installedGooglePackages(ctx, logger, vm)
	if err != nil {
		return err
	}
	if len(packages) > 0 {
		return fmt.Errorf("still installed: %v", packages)
	}
	return nil
}

func checkNoGcloud(ctx context.Context, logger *log.Logger, vm *VM) error {
	// Prints where the test user and root find gcloud, if they do. Tests run
	// it as either.
	output, err := RunRemotely(ctx, logger, vm, "command -v gcloud; sudo -n sh -c 'command -v gcloud'; true")
	if err != nil {
		return err
	}
	if found := slices.Compact(strings.Fields(output.Stdout)); len(found) > 0 {
		return fmt.Errorf("found %v", found)
	}
	return nil
}

func checkSystemRunning(ctx context.Context, logger *log.Logger, vm *VM) error {
	// systemctl exits with a nonzero status for every state but "running",
	// so go by what it prints.
	output, err := RunRemotely(ctx, logger, vm, "systemctl is-system-running")
	switch state := strings.TrimSpace(output.Stdout); state {
	case "running":
		return nil
	case "degraded":
		// Some unit failed. As when a VM starts (see waitForStartLinux),
		// that is good enough to go on; log which units they are.
		if _, err := RunRemotely(ctx, logger, vm, "systemctl --failed"); err != nil {
			logger.Printf("Could not list the failed units: %v", err)
		}
		return nil
	default:
		return fmt.Errorf("systemctl is-system-running reported %q: %v", state, err)
	}
}
