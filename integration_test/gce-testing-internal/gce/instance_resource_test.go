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
	"regexp"
	"strings"
	"testing"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/protobuf/proto"
)

func TestParseDiskSizeGb(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"10GB", 10, false},
		{"50Gb", 50, false},
		{"100gb", 100, false},
		{"200G", 200, false},
		{"500g", 500, false},
		{"1024", 1024, false},
		{"invalid", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDiskSizeGb(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseDiskSizeGb(%q) error = %v, wantErr = %v", tc.input, err, tc.wantErr)
			}
			if got != tc.expected {
				t.Errorf("parseDiskSizeGb(%q) = %d, want %d", tc.input, got, tc.expected)
			}
		})
	}
}

func TestParseDurationSeconds(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"1h", 3600, false},
		{"30m", 1800, false},
		{"1800s", 1800, false},
		{"7200", 7200, false},
		{"1d", 86400, false},
		{"2d", 172800, false},
		{"invalid", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseDurationSeconds(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseDurationSeconds(%q) error = %v, wantErr = %v", tc.input, err, tc.wantErr)
			}
			if got != tc.expected {
				t.Errorf("parseDurationSeconds(%q) = %d, want %d", tc.input, got, tc.expected)
			}
		})
	}
}

func TestParseExtraCreateArguments(t *testing.T) {
	args := []string{
		"--boot-disk-size=100GB",
		"--boot-disk-type=pd-ssd",
		"--scopes=https://www.googleapis.com/auth/cloud-platform",
		"--tags=tag1,tag2",
		"--metadata=key1=val1,key2=val2",
	}
	extra, err := parseExtraCreateArguments(args)
	if err != nil {
		t.Fatalf("parseExtraCreateArguments() error = %v", err)
	}
	if extra.diskSizeGb == nil || *extra.diskSizeGb != 100 {
		t.Errorf("parseExtraCreateArguments() diskSize = %v, want 100", extra.diskSizeGb)
	}
	if extra.diskType == nil || *extra.diskType != "pd-ssd" {
		t.Errorf("parseExtraCreateArguments() diskType = %v, want pd-ssd", extra.diskType)
	}
	if len(extra.scopes) != 1 || extra.scopes[0] != "https://www.googleapis.com/auth/cloud-platform" {
		t.Errorf("parseExtraCreateArguments() scopes = %v, want [cloud-platform]", extra.scopes)
	}
	if len(extra.tags) != 2 || extra.tags[0] != "tag1" || extra.tags[1] != "tag2" {
		t.Errorf("parseExtraCreateArguments() tags = %v, want [tag1 tag2]", extra.tags)
	}
	if extra.extraMetadata["key1"] != "val1" || extra.extraMetadata["key2"] != "val2" {
		t.Errorf("parseExtraCreateArguments() metadata = %v, want key1=val1, key2=val2", extra.extraMetadata)
	}

	// Test space-separated flags
	spaceArgs := []string{
		"--boot-disk-size", "200G",
		"--boot-disk-type", "hyperdisk-balanced",
		"--scopes", "cloud-platform",
		"--tags", "tag3",
		"--metadata", "foo=bar",
	}
	extra, err = parseExtraCreateArguments(spaceArgs)
	if err != nil {
		t.Fatalf("parseExtraCreateArguments(spaceArgs) error = %v", err)
	}
	if extra.diskSizeGb == nil || *extra.diskSizeGb != 200 {
		t.Errorf("parseExtraCreateArguments() spaceArgs diskSize = %v, want 200", extra.diskSizeGb)
	}
	if extra.diskType == nil || *extra.diskType != "hyperdisk-balanced" {
		t.Errorf("parseExtraCreateArguments() spaceArgs diskType = %v, want hyperdisk-balanced", extra.diskType)
	}
	if len(extra.scopes) != 1 || extra.scopes[0] != "https://www.googleapis.com/auth/cloud-platform" {
		t.Errorf("parseExtraCreateArguments() spaceArgs scopes = %v, want [cloud-platform]", extra.scopes)
	}
	if len(extra.tags) != 1 || extra.tags[0] != "tag3" {
		t.Errorf("parseExtraCreateArguments() spaceArgs tags = %v, want [tag3]", extra.tags)
	}
	if extra.extraMetadata["foo"] != "bar" {
		t.Errorf("parseExtraCreateArguments() spaceArgs metadata = %v, want foo=bar", extra.extraMetadata)
	}

	// Test invalid disk size returns error
	invalidArgs := []string{"--boot-disk-size=invalid"}
	if _, err := parseExtraCreateArguments(invalidArgs); err == nil {
		t.Errorf("parseExtraCreateArguments(invalidArgs) expected error, got nil")
	}
}

func TestBuildInstanceResource(t *testing.T) {
	vm := &VM{
		Name:        "test-vm",
		Project:     "test-project",
		Zone:        "us-central1-a",
		MachineType: "e2-standard-4",
		Network:     "default",
		ImageSpec:   "debian-cloud:debian-11",
	}
	options := VMOptions{
		ImageSpec:  "debian-cloud:debian-11",
		TimeToLive: "2h",
		Metadata: map[string]string{
			"custom-key": "custom-val",
		},
		Labels: map[string]string{
			"env": "test",
		},
		ExtraCreateArguments: []string{
			"--boot-disk-size=100GB",
			"--boot-disk-type=pd-ssd",
			"--tags=tag1,tag2",
		},
	}

	inst, err := buildInstanceResource(options, vm)
	if err != nil {
		t.Fatalf("buildInstanceResource() error = %v", err)
	}

	if inst.GetName() != "test-vm" {
		t.Errorf("inst.GetName() = %q, want test-vm", inst.GetName())
	}
	if inst.GetMachineType() != "zones/us-central1-a/machineTypes/e2-standard-4" {
		t.Errorf("inst.GetMachineType() = %q, want zones/us-central1-a/machineTypes/e2-standard-4", inst.GetMachineType())
	}
	if len(inst.GetDisks()) != 1 {
		t.Fatalf("len(inst.GetDisks()) = %d, want 1", len(inst.GetDisks()))
	}
	bootDisk := inst.GetDisks()[0]
	if !bootDisk.GetBoot() || !bootDisk.GetAutoDelete() {
		t.Errorf("bootDisk boot/autoDelete not set properly: %#v", bootDisk)
	}
	if bootDisk.InitializeParams.GetSourceImage() != "projects/debian-cloud/global/images/family/debian-11" {
		t.Errorf("bootDisk sourceImage = %q, want projects/debian-cloud/global/images/family/debian-11", bootDisk.InitializeParams.GetSourceImage())
	}
	if bootDisk.InitializeParams.GetDiskSizeGb() != 100 {
		t.Errorf("bootDisk diskSizeGb = %d, want 100", bootDisk.InitializeParams.GetDiskSizeGb())
	}
	if bootDisk.InitializeParams.GetDiskType() != "zones/us-central1-a/diskTypes/pd-ssd" {
		t.Errorf("bootDisk diskType = %q, want zones/us-central1-a/diskTypes/pd-ssd", bootDisk.InitializeParams.GetDiskType())
	}
	if inst.Scheduling == nil || inst.Scheduling.MaxRunDuration == nil || inst.Scheduling.MaxRunDuration.GetSeconds() != 7200 {
		t.Errorf("inst.Scheduling.MaxRunDuration = %v, want 7200s", inst.Scheduling)
	}
	if inst.Scheduling.GetInstanceTerminationAction() != "DELETE" {
		t.Errorf("inst.Scheduling.InstanceTerminationAction = %q, want DELETE", inst.Scheduling.GetInstanceTerminationAction())
	}
	if inst.Scheduling.GetProvisioningModel() != "STANDARD" {
		t.Errorf("inst.Scheduling.ProvisioningModel = %q, want STANDARD", inst.Scheduling.GetProvisioningModel())
	}
	if len(inst.NetworkInterfaces) != 1 {
		t.Fatalf("len(inst.NetworkInterfaces) = %d, want 1", len(inst.NetworkInterfaces))
	}
	nic := inst.NetworkInterfaces[0]
	if nic.GetNetwork() != "projects/test-project/global/networks/default" {
		t.Errorf("nic.Network = %q, want projects/test-project/global/networks/default", nic.GetNetwork())
	}
	if len(nic.AccessConfigs) != 1 || nic.AccessConfigs[0].GetName() != "External NAT" {
		t.Errorf("nic.AccessConfigs = %v, want [External NAT]", nic.AccessConfigs)
	}
	if inst.Labels["env"] != "test" {
		t.Errorf("inst.Labels[env] = %q, want test", inst.Labels["env"])
	}
	if len(inst.Tags.GetItems()) != 2 {
		t.Errorf("inst.Tags = %v, want [tag1 tag2]", inst.Tags.GetItems())
	}
}

func TestExtractIPFromInstance(t *testing.T) {
	instWithExternalIP := &computepb.Instance{
		Name: proto.String("test-vm"),
		NetworkInterfaces: []*computepb.NetworkInterface{
			{
				NetworkIP: proto.String("10.128.0.2"),
				AccessConfigs: []*computepb.AccessConfig{
					{
						NatIP: proto.String("35.200.10.20"),
					},
				},
			},
		},
	}

	ip, err := extractIPFromInstance(instWithExternalIP)
	if err != nil {
		t.Fatalf("extractIPFromInstance() error = %v", err)
	}
	if ip != "35.200.10.20" {
		t.Errorf("extractIPFromInstance() = %q, want 35.200.10.20", ip)
	}

	// Test USE_INTERNAL_IP=true
	t.Setenv("USE_INTERNAL_IP", "true")
	ip, err = extractIPFromInstance(instWithExternalIP)
	if err != nil {
		t.Fatalf("extractIPFromInstance(USE_INTERNAL_IP) error = %v", err)
	}
	if ip != "10.128.0.2" {
		t.Errorf("extractIPFromInstance(USE_INTERNAL_IP) = %q, want 10.128.0.2", ip)
	}

	// Test missing network interfaces
	emptyInst := &computepb.Instance{Name: proto.String("empty-vm")}
	if _, err := extractIPFromInstance(emptyInst); err == nil {
		t.Errorf("extractIPFromInstance(emptyInst) expected error, got nil")
	}
}

func TestBuildInstanceProperties(t *testing.T) {
	vm := &VM{
		Name:        "test-mig-vm",
		Project:     "test-project",
		Zone:        "us-central1-a",
		MachineType: "e2-standard-4",
		Network:     "default",
		ImageSpec:   "debian-cloud:debian-11",
	}
	options := VMOptions{
		ImageSpec:  "debian-cloud:debian-11",
		TimeToLive: "2h",
		Metadata: map[string]string{
			"custom-key": "custom-val",
		},
		Labels: map[string]string{
			"env": "test",
		},
		ExtraCreateArguments: []string{
			"--boot-disk-size=100GB",
			"--boot-disk-type=zones/us-central1-a/diskTypes/pd-ssd",
			"--tags=tag1,tag2",
		},
	}

	props, err := buildInstanceProperties(options, vm)
	if err != nil {
		t.Fatalf("buildInstanceProperties() error = %v", err)
	}

	// InstanceProperties.MachineType must be the short name (e.g. "e2-standard-4"),
	// NOT a full or partial zone URL, or GCE API will return INTERNAL_ERROR.
	if props.GetMachineType() != "e2-standard-4" {
		t.Errorf("props.GetMachineType() = %q, want e2-standard-4", props.GetMachineType())
	}
	if len(props.GetDisks()) != 1 {
		t.Fatalf("len(props.GetDisks()) = %d, want 1", len(props.GetDisks()))
	}
	bootDisk := props.GetDisks()[0]
	if !bootDisk.GetBoot() || !bootDisk.GetAutoDelete() {
		t.Errorf("bootDisk boot/autoDelete not set properly: %#v", bootDisk)
	}
	if bootDisk.InitializeParams.GetDiskSizeGb() != 100 {
		t.Errorf("bootDisk diskSizeGb = %d, want 100", bootDisk.InitializeParams.GetDiskSizeGb())
	}
	// InstanceProperties AttachedDiskInitializeParams.DiskType must be the short name (e.g. "pd-ssd"),
	// not a URL.
	if bootDisk.InitializeParams.GetDiskType() != "pd-ssd" {
		t.Errorf("bootDisk diskType = %q, want pd-ssd", bootDisk.InitializeParams.GetDiskType())
	}
	if props.Scheduling == nil || props.Scheduling.MaxRunDuration == nil || props.Scheduling.MaxRunDuration.GetSeconds() != 7200 {
		t.Errorf("props.Scheduling.MaxRunDuration = %v, want 7200s", props.Scheduling)
	}
	if props.Scheduling.GetInstanceTerminationAction() != "DELETE" {
		t.Errorf("props.Scheduling.InstanceTerminationAction = %q, want DELETE", props.Scheduling.GetInstanceTerminationAction())
	}
	if props.Scheduling.GetProvisioningModel() != "STANDARD" {
		t.Errorf("props.Scheduling.ProvisioningModel = %q, want STANDARD", props.Scheduling.GetProvisioningModel())
	}
	if len(props.NetworkInterfaces) != 1 {
		t.Fatalf("len(props.NetworkInterfaces) = %d, want 1", len(props.NetworkInterfaces))
	}
	if props.Labels["env"] != "test" {
		t.Errorf("props.Labels[env] = %q, want test", props.Labels["env"])
	}
	if len(props.Tags.GetItems()) != 2 {
		t.Errorf("props.Tags = %v, want [tag1 tag2]", props.Tags.GetItems())
	}
}

func TestBuildInstanceTemplate(t *testing.T) {
	vm := &VM{
		Name:        "test-mig-vm",
		Project:     "test-project",
		Zone:        "us-central1-a",
		MachineType: "e2-standard-4",
		Network:     "default",
		ImageSpec:   "debian-cloud:debian-11",
	}
	options := VMOptions{
		ImageSpec: "debian-cloud:debian-11",
	}

	tmpl, err := buildInstanceTemplate("test-tmpl-name", options, vm)
	if err != nil {
		t.Fatalf("buildInstanceTemplate() error = %v", err)
	}
	if tmpl.GetName() != "test-tmpl-name" {
		t.Errorf("tmpl.GetName() = %q, want test-tmpl-name", tmpl.GetName())
	}
	if tmpl.GetProperties() == nil {
		t.Fatalf("tmpl.GetProperties() is nil")
	}
	if tmpl.GetProperties().GetMachineType() != "e2-standard-4" {
		t.Errorf("tmpl.GetProperties().GetMachineType() = %q, want e2-standard-4", tmpl.GetProperties().GetMachineType())
	}
}

func TestCleanDiskType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"pd-standard", "pd-standard"},
		{"pd-ssd", "pd-ssd"},
		{"zones/us-central1-a/diskTypes/pd-ssd", "pd-ssd"},
		{"projects/p/zones/z/diskTypes/hyperdisk-balanced", "hyperdisk-balanced"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := cleanDiskType(tc.input)
			if got != tc.expected {
				t.Errorf("cleanDiskType(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestDeducedBaseInstanceName(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short name",
			input:    "my-mig",
			expected: "my-mig",
		},
		{
			name:     "exactly 54 chars",
			input:    strings.Repeat("a", 54),
			expected: strings.Repeat("a", 54),
		},
		{
			name:     "over 54 chars truncated",
			input:    strings.Repeat("a", 54) + "-extra",
			expected: strings.Repeat("a", 54),
		},
		{
			name:     "trailing hyphen stripped when truncated",
			input:    strings.Repeat("a", 53) + "--extra",
			expected: strings.Repeat("a", 53),
		},
		{
			name:     "real world mig name truncated to 54 chars",
			input:    "github-test-20261008-97629-dccf3c83-f95c-437d-9f75-625e28-mig",
			expected: "github-test-20261008-97629-dccf3c83-f95c-437d-9f75-625",
		},
	}

	apiRegex := regexp.MustCompile(`^[a-z](([-a-z0-9]{0,57})|([-a-z0-9]{0,54}-#{1,10}(\[[0-9]{1,10}\])?)|([-a-z0-9]{0,25}-UUID))$`)
	rfc1035Regex := regexp.MustCompile(`^[a-z]([-a-z0-9]*[a-z0-9])?$`)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := deducedBaseInstanceName(tc.input)
			if actual != tc.expected {
				t.Errorf("deducedBaseInstanceName(%q) = %q, expected %q", tc.input, actual, tc.expected)
			}
			if len(actual) > 54 {
				t.Errorf("deducedBaseInstanceName(%q) length %d > 54", tc.input, len(actual))
			}
			if !apiRegex.MatchString(actual) {
				t.Errorf("deducedBaseInstanceName(%q) = %q does not match GCE API regex", tc.input, actual)
			}
			if !rfc1035Regex.MatchString(actual) {
				t.Errorf("deducedBaseInstanceName(%q) = %q does not match RFC1035 regex", tc.input, actual)
			}
		})
	}
}
