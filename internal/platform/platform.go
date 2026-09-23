// Copyright 2023 Google LLC
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

package platform

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator/resourcedetector"
	"github.com/shirou/gopsutil/host"
)

type Platform struct {
	Type               Type
	WindowsBuildNumber string
	WinlogV1Channels   []string
	HostInfo           *host.InfoStat
	DistroCodename     string
	HasNvidiaGpu       bool
	ResourceOverride   resourcedetector.Resource
	// Resource override only for GCE metadata unit testing
	TestGCEResourceOverride resourcedetector.Resource
}

type Type int

const (
	Linux Type = 1 << iota
	Windows
	All = Linux | Windows
)

func (p Platform) Is2012() bool {
	// https://en.wikipedia.org/wiki/List_of_Microsoft_Windows_versions#Server_versions
	return p.WindowsBuildNumber == "9200" || p.WindowsBuildNumber == "9600"
}

func (p Platform) Is2016() bool {
	return p.WindowsBuildNumber == "14393"
}

type platformKeyType struct{}

// platformKey is a singleton that is used as a Context key for retrieving the current platform from the context.Context.
var platformKey = platformKeyType{}

func (p Platform) TestContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, platformKey, p)
}

var detectedPlatform Platform = detect()

func FromContext(ctx context.Context) Platform {
	if opt := ctx.Value(platformKey); opt != nil {
		return opt.(Platform)
	}
	return detectedPlatform
}

func detect() Platform {
	info, err := host.Info()
	if err != nil {
		log.Fatalf("Failed to detect platform: %v", err)
	}
	p := Platform{
		HostInfo: info,
	}
	p.detectPlatform()
	return p
}

func (p Platform) Hostname() string {
	return p.HostInfo.Hostname
}

func (p Platform) Name() string {
	if p.Type == Windows {
		return "windows"
	} else if p.Type == Linux {
		return "linux"
	}
	panic(fmt.Sprintf("unknown type %v", p.Type))
}

func (p Platform) GetResource() (resourcedetector.Resource, error) {
	if p.TestGCEResourceOverride != nil {
		return p.TestGCEResourceOverride, nil
	} else if p.ResourceOverride != nil {
		return p.ResourceOverride, nil
	}
	r, err := resourcedetector.GetResource()
	return r, err
}

// BuildDistro returns the distribution identifier used in the agent version
// label and User-Agent header (e.g. "bookworm", "noble", "el9", "sles15",
// "windows-ltsc2022"). It falls back to "build_distro" when the platform is
// synthetic or unrecognized (such as in golden configuration tests).
func (p Platform) BuildDistro() string {
	if p.Type == Windows {
		switch p.WindowsBuildNumber {
		case "14393":
			return "windows-ltsc2016"
		case "17763":
			return "windows-ltsc2019"
		case "20348":
			return "windows-ltsc2022"
		case "26100":
			return "windows-ltsc2025"
		default:
			// Unlisted real Windows Server builds default to windows-ltsc2022
			// (matching the single Windows package builder image), while synthetic
			// test platforms (e.g. Platform="win_platform") fall back to "build_distro".
			if p.HostInfo != nil && strings.HasPrefix(strings.ToLower(p.HostInfo.Platform), "microsoft windows") {
				return "windows-ltsc2022"
			}
			return "build_distro"
		}
	} else if p.Type == Linux && p.HostInfo != nil {
		major, _, _ := strings.Cut(p.HostInfo.PlatformVersion, ".")
		switch p.HostInfo.PlatformFamily {
		case "debian":
			if p.DistroCodename != "" {
				return p.DistroCodename
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
