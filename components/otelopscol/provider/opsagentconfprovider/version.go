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

package opsagentconfprovider

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/host"
)

const (
	metricsVersionLabelPrefix = "google-cloud-ops-agent-metrics"
	metricsUserAgentPrefix    = "Google-Cloud-Ops-Agent-Metrics"
	nvidiaVendorID            = "0x10de"
)

// Version is the Ops Agent version reported in self-metrics and OTLP User-Agent headers.
// It can be overridden at link time via -ldflags.
var Version = "latest"

type hostInfo struct {
	OS              string
	Platform        string
	PlatformFamily  string
	PlatformVersion string
	DistroCodename  string
	Hostname        string
	HasNvidiaGPU    bool
}

func detectHostInfo() hostInfo {
	h := hostInfo{
		OS: runtime.GOOS,
	}
	if info, err := host.Info(); err == nil && info != nil {
		h.OS = info.OS
		h.Platform = info.Platform
		h.PlatformFamily = info.PlatformFamily
		h.PlatformVersion = info.PlatformVersion
		h.Hostname = info.Hostname
	}
	if h.OS != "windows" {
		h.DistroCodename = readDistroCodename("/etc/os-release", "/usr/lib/os-release")
		h.HasNvidiaGPU = hasNvidiaGPU("/sys/bus/pci/devices")
	}
	return h
}

func hasNvidiaGPU(sysDevicesPath string) bool {
	devices, err := os.ReadDir(sysDevicesPath)
	if err != nil {
		return false
	}
	for _, device := range devices {
		vendor, err := os.ReadFile(filepath.Join(sysDevicesPath, device.Name(), "vendor"))
		if err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(string(vendor)), nvidiaVendorID) {
			return true
		}
	}
	return false
}

func (h hostInfo) versionLabel() string {
	return fmt.Sprintf("%s/%s-%s", metricsVersionLabelPrefix, Version, h.buildDistro())
}

func (h hostInfo) userAgent() string {
	return fmt.Sprintf("%s/%s (BuildDistro=%s;Platform=%s;ShortName=%s;ShortVersion=%s)",
		metricsUserAgentPrefix, Version, h.buildDistro(), h.OS, h.Platform, h.PlatformVersion)
}

func (h hostInfo) buildDistro() string {
	switch h.OS {
	case "windows":
		switch windowsBuildNumber(h.PlatformVersion) {
		case "14393":
			return "windows-ltsc2016"
		case "17763":
			return "windows-ltsc2019"
		case "20348":
			return "windows-ltsc2022"
		case "26100":
			return "windows-ltsc2025"
		default:
			if strings.HasPrefix(strings.ToLower(h.Platform), "microsoft windows") {
				return "windows-ltsc2022"
			}
			return "build_distro"
		}
	case "linux":
		major, _, _ := strings.Cut(h.PlatformVersion, ".")
		switch h.PlatformFamily {
		case "debian":
			if h.DistroCodename != "" {
				return h.DistroCodename
			}
		case "rhel":
			if major != "" {
				return "el" + major
			}
		case "suse":
			if major == "42" {
				major = "12"
			}
			if major != "" {
				return "sles" + major
			}
		}
	}
	return "build_distro"
}

func windowsBuildNumber(platformVersion string) string {
	if _, after, ok := strings.Cut(platformVersion, "Build "); ok {
		if fields := strings.Fields(after); len(fields) > 0 {
			return fields[0]
		}
	}
	if fields := strings.Fields(platformVersion); len(fields) > 0 {
		if parts := strings.Split(fields[0], "."); len(parts) >= 3 {
			return parts[2]
		}
		return fields[0]
	}
	return ""
}

func readDistroCodename(paths ...string) string {
	for _, path := range paths {
		if codename := parseDistroCodenameFile(path); codename != "" {
			return codename
		}
	}
	return ""
}

func parseDistroCodenameFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var ubuntuCodename string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if val, ok := strings.CutPrefix(line, "VERSION_CODENAME="); ok {
			if trimmed := trimOSReleaseValue(val); trimmed != "" {
				return trimmed
			}
		}
		if val, ok := strings.CutPrefix(line, "UBUNTU_CODENAME="); ok {
			ubuntuCodename = trimOSReleaseValue(val)
		}
	}
	return ubuntuCodename
}

func trimOSReleaseValue(val string) string {
	return strings.Trim(strings.TrimSpace(val), `"'`)
}
