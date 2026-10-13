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
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/protobuf/proto"
)

var defaultScopes = []string{
	"https://www.googleapis.com/auth/devstorage.read_only",
	"https://www.googleapis.com/auth/logging.write",
	"https://www.googleapis.com/auth/monitoring.write",
	"https://www.googleapis.com/auth/servicecontrol",
	"https://www.googleapis.com/auth/service.management.readonly",
	"https://www.googleapis.com/auth/trace.append",
}

func normalizeScope(s string) string {
	switch s {
	case "cloud-platform":
		return "https://www.googleapis.com/auth/cloud-platform"
	default:
		if !strings.HasPrefix(s, "https://") {
			return "https://www.googleapis.com/auth/" + s
		}
		return s
	}
}

func parseDiskSizeGb(val string) (int64, error) {
	val = strings.TrimSpace(val)
	val = strings.TrimSuffix(val, "GB")
	val = strings.TrimSuffix(val, "Gb")
	val = strings.TrimSuffix(val, "gb")
	val = strings.TrimSuffix(val, "G")
	val = strings.TrimSuffix(val, "g")
	return strconv.ParseInt(val, 10, 64)
}

func parseDurationSeconds(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		days, err := strconv.ParseInt(daysStr, 10, 64)
		if err == nil {
			return days * 24 * 3600, nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return int64(d.Seconds()), nil
	}
	trimmed := strings.TrimSuffix(s, "s")
	if sec, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return sec, nil
	}
	return 0, fmt.Errorf("unable to parse duration: %q", s)
}

func sourceImageFromImageSpec(imageSpec string, imageFamilyScope string) (string, error) {
	if strings.HasPrefix(imageSpec, "projects/") {
		return imageSpec, nil
	}
	delim := ""
	if strings.Contains(imageSpec, ":") {
		delim = ":"
	} else if strings.Contains(imageSpec, "=") {
		delim = "="
	} else {
		return "", fmt.Errorf("invalid imageSpec: %s", imageSpec)
	}

	s := strings.Split(imageSpec, delim)
	project := s[0]
	name := s[1]
	scope := imageFamilyScope
	if scope == "" {
		scope = "global"
	}
	switch delim {
	case ":":
		return fmt.Sprintf("projects/%s/%s/images/family/%s", project, scope, name), nil
	case "=":
		return fmt.Sprintf("projects/%s/%s/images/%s", project, scope, name), nil
	default:
		return "", fmt.Errorf("invalid imageSpec: %s", imageSpec)
	}
}

type extraCreateArgs struct {
	diskSizeGb    *int64
	diskType      *string
	scopes        []string
	tags          []string
	extraMetadata map[string]string
}

func parseExtraCreateArguments(args []string) (extraCreateArgs, error) {
	result := extraCreateArgs{
		extraMetadata: make(map[string]string),
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		getVal := func(prefix string) (string, bool) {
			if strings.HasPrefix(arg, prefix+"=") {
				return strings.TrimPrefix(arg, prefix+"="), true
			}
			if arg == prefix && i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}

		if val, ok := getVal("--boot-disk-size"); ok {
			sz, err := parseDiskSizeGb(val)
			if err != nil {
				return result, fmt.Errorf("invalid --boot-disk-size %q: %w", val, err)
			}
			result.diskSizeGb = &sz
		} else if val, ok := getVal("--boot-disk-type"); ok {
			result.diskType = &val
		} else if val, ok := getVal("--scopes"); ok {
			for _, sc := range strings.Split(val, ",") {
				if sc != "" {
					result.scopes = append(result.scopes, normalizeScope(sc))
				}
			}
		} else if val, ok := getVal("--tags"); ok {
			for _, t := range strings.Split(val, ",") {
				if t != "" {
					result.tags = append(result.tags, t)
				}
			}
		} else if val, ok := getVal("--metadata"); ok {
			for _, item := range strings.Split(val, ",") {
				parts := strings.SplitN(item, "=", 2)
				if len(parts) == 2 {
					result.extraMetadata[parts[0]] = parts[1]
				}
			}
		}
	}
	return result, nil
}

func buildInstanceResource(options VMOptions, vm *VM) (*computepb.Instance, error) {
	sourceImage, err := sourceImageFromImageSpec(vm.ImageSpec, options.ImageFamilyScope)
	if err != nil {
		return nil, err
	}

	extraArgs, err := parseExtraCreateArguments(options.ExtraCreateArguments)
	if err != nil {
		return nil, err
	}

	initializeParams := &computepb.AttachedDiskInitializeParams{
		SourceImage: proto.String(sourceImage),
	}
	if extraArgs.diskSizeGb != nil {
		initializeParams.DiskSizeGb = extraArgs.diskSizeGb
	}
	if extraArgs.diskType != nil {
		dt := *extraArgs.diskType
		if !strings.Contains(dt, "/") {
			dt = fmt.Sprintf("zones/%s/diskTypes/%s", vm.Zone, dt)
		}
		initializeParams.DiskType = proto.String(dt)
	}

	bootDisk := &computepb.AttachedDisk{
		Boot:             proto.Bool(true),
		AutoDelete:       proto.Bool(true),
		InitializeParams: initializeParams,
	}

	network := vm.Network
	if !strings.Contains(network, "/") {
		network = fmt.Sprintf("projects/%s/global/networks/%s", vm.Project, network)
	}
	nic := &computepb.NetworkInterface{
		Network: proto.String(network),
	}
	if os.Getenv("USE_INTERNAL_IP") != "true" {
		nic.AccessConfigs = []*computepb.AccessConfig{
			{
				Name:        proto.String("External NAT"),
				Type:        proto.String("ONE_TO_ONE_NAT"),
				NetworkTier: proto.String("PREMIUM"),
			},
		}
	}

	combinedMetadata := make(map[string]string)
	for k, v := range options.Metadata {
		combinedMetadata[k] = v
	}
	for k, v := range extraArgs.extraMetadata {
		combinedMetadata[k] = v
	}
	newMetadata, err := addFrameworkMetadata(vm.ImageSpec, combinedMetadata)
	if err != nil {
		return nil, fmt.Errorf("buildInstanceResource() could not construct valid metadata: %w", err)
	}
	var metadataItems []*computepb.Items
	for k, v := range newMetadata {
		metadataItems = append(metadataItems, &computepb.Items{
			Key:   proto.String(k),
			Value: proto.String(v),
		})
	}
	sort.Slice(metadataItems, func(i, j int) bool {
		return metadataItems[i].GetKey() < metadataItems[j].GetKey()
	})

	newLabels, err := addFrameworkLabels(options.Labels)
	if err != nil {
		return nil, fmt.Errorf("buildInstanceResource() could not construct valid labels: %w", err)
	}

	email := os.Getenv("SERVICE_EMAIL")
	if email == "" {
		email = "default"
	}
	var resolvedScopes []string
	if len(extraArgs.scopes) == 0 {
		resolvedScopes = defaultScopes
	} else {
		for _, sc := range extraArgs.scopes {
			if sc == "default" || sc == "https://www.googleapis.com/auth/default" {
				resolvedScopes = append(resolvedScopes, defaultScopes...)
			} else {
				resolvedScopes = append(resolvedScopes, sc)
			}
		}
	}

	inst := &computepb.Instance{
		Name:              proto.String(vm.Name),
		MachineType:       proto.String(fmt.Sprintf("zones/%s/machineTypes/%s", vm.Zone, vm.MachineType)),
		Disks:             []*computepb.AttachedDisk{bootDisk},
		NetworkInterfaces: []*computepb.NetworkInterface{nic},
		Metadata: &computepb.Metadata{
			Items: metadataItems,
		},
		Labels: newLabels,
		ServiceAccounts: []*computepb.ServiceAccount{
			{
				Email:  proto.String(email),
				Scopes: resolvedScopes,
			},
		},
	}

	if len(extraArgs.tags) > 0 {
		inst.Tags = &computepb.Tags{
			Items: extraArgs.tags,
		}
	}

	if options.TimeToLive != "" {
		secs, err := parseDurationSeconds(options.TimeToLive)
		if err != nil {
			return nil, err
		}
		inst.Scheduling = &computepb.Scheduling{
			MaxRunDuration: &computepb.Duration{
				Seconds: proto.Int64(secs),
			},
			InstanceTerminationAction: proto.String("DELETE"),
			ProvisioningModel:         proto.String("STANDARD"),
		}
	}

	return inst, nil
}

func extractIPFromInstance(inst *computepb.Instance) (string, error) {
	if len(inst.GetNetworkInterfaces()) == 0 {
		return "", fmt.Errorf("empty NetworkInterfaces list in instance %q", inst.GetName())
	}
	nic := inst.GetNetworkInterfaces()[0]
	if os.Getenv("USE_INTERNAL_IP") == "true" {
		internalIP := nic.GetNetworkIP()
		if internalIP == "" {
			return "", fmt.Errorf("empty internal IP in instance %q", inst.GetName())
		}
		return internalIP, nil
	}
	if len(nic.GetAccessConfigs()) == 0 {
		return "", fmt.Errorf("empty AccessConfigs in instance %q", inst.GetName())
	}
	externalIP := nic.GetAccessConfigs()[0].GetNatIP()
	if externalIP == "" {
		return "", fmt.Errorf("empty external IP in instance %q", inst.GetName())
	}
	return externalIP, nil
}
