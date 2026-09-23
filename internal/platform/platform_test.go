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

package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/host"
)

func TestBuildDistro(t *testing.T) {
	tests := []struct {
		name     string
		platform Platform
		want     string
	}{
		{
			name: "debian bookworm",
			platform: Platform{
				Type:           Linux,
				DistroCodename: "bookworm",
				HostInfo: &host.InfoStat{
					OS:              "linux",
					Platform:        "debian",
					PlatformFamily:  "debian",
					PlatformVersion: "12.9",
				},
			},
			want: "bookworm",
		},
		{
			name: "ubuntu noble",
			platform: Platform{
				Type:           Linux,
				DistroCodename: "noble",
				HostInfo: &host.InfoStat{
					OS:              "linux",
					Platform:        "ubuntu",
					PlatformFamily:  "debian",
					PlatformVersion: "24.04",
				},
			},
			want: "noble",
		},
		{
			name: "debian with empty codename falls back to build_distro",
			platform: Platform{
				Type: Linux,
				HostInfo: &host.InfoStat{
					OS:              "linux",
					Platform:        "debian",
					PlatformFamily:  "debian",
					PlatformVersion: "12.9",
				},
			},
			want: "build_distro",
		},
		{
			name: "rocky 9 (rhel family)",
			platform: Platform{
				Type: Linux,
				HostInfo: &host.InfoStat{
					OS:              "linux",
					Platform:        "rocky",
					PlatformFamily:  "rhel",
					PlatformVersion: "9.5",
				},
			},
			want: "el9",
		},
		{
			name: "rhel 10",
			platform: Platform{
				Type: Linux,
				HostInfo: &host.InfoStat{
					OS:              "linux",
					Platform:        "rhel",
					PlatformFamily:  "rhel",
					PlatformVersion: "10.0",
				},
			},
			want: "el10",
		},
		{
			name: "sles 15",
			platform: Platform{
				Type: Linux,
				HostInfo: &host.InfoStat{
					OS:              "linux",
					Platform:        "sles",
					PlatformFamily:  "suse",
					PlatformVersion: "15.6",
				},
			},
			want: "sles15",
		},
		{
			name: "opensuse leap 15.6",
			platform: Platform{
				Type: Linux,
				HostInfo: &host.InfoStat{
					OS:              "linux",
					Platform:        "opensuse-leap",
					PlatformFamily:  "suse",
					PlatformVersion: "15.6",
				},
			},
			want: "sles15",
		},
		{
			name: "opensuse leap 42.3 (sles 12 emulation)",
			platform: Platform{
				Type: Linux,
				HostInfo: &host.InfoStat{
					OS:              "linux",
					Platform:        "opensuse-leap",
					PlatformFamily:  "suse",
					PlatformVersion: "42.3",
				},
			},
			want: "sles12",
		},
		{
			name: "windows server 2016",
			platform: Platform{
				Type:               Windows,
				WindowsBuildNumber: "14393",
			},
			want: "windows-ltsc2016",
		},
		{
			name: "windows server 2019",
			platform: Platform{
				Type:               Windows,
				WindowsBuildNumber: "17763",
			},
			want: "windows-ltsc2019",
		},
		{
			name: "windows server 2022",
			platform: Platform{
				Type:               Windows,
				WindowsBuildNumber: "20348",
				HostInfo: &host.InfoStat{
					OS:              "windows",
					Platform:        "Microsoft Windows Server 2022 Datacenter",
					PlatformVersion: "10.0.20348 Build 20348",
				},
			},
			want: "windows-ltsc2022",
		},
		{
			name: "windows server 2025",
			platform: Platform{
				Type:               Windows,
				WindowsBuildNumber: "26100",
			},
			want: "windows-ltsc2025",
		},
		{
			name: "synthetic linux golden test platform",
			platform: Platform{
				Type: Linux,
				HostInfo: &host.InfoStat{
					OS:              "linux",
					Platform:        "linux_platform",
					PlatformVersion: "linux_platform_version",
				},
			},
			want: "build_distro",
		},
		{
			name: "linux with nil HostInfo",
			platform: Platform{
				Type: Linux,
			},
			want: "build_distro",
		},
		{
			name: "synthetic windows golden test platform",
			platform: Platform{
				Type:               Windows,
				WindowsBuildNumber: "1",
				HostInfo: &host.InfoStat{
					OS:              "windows",
					Platform:        "win_platform",
					PlatformVersion: "win_platform_version",
				},
			},
			want: "build_distro",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.platform.BuildDistro(); got != tc.want {
				t.Errorf("BuildDistro() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadDistroCodename(t *testing.T) {
	dir := t.TempDir()

	unquoted := filepath.Join(dir, "os-release-unquoted")
	if err := os.WriteFile(unquoted, []byte("ID=debian\nVERSION_CODENAME=bookworm\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := readDistroCodename(unquoted); got != "bookworm" {
		t.Errorf("readDistroCodename(unquoted) = %q, want %q", got, "bookworm")
	}

	quoted := filepath.Join(dir, "os-release-quoted")
	if err := os.WriteFile(quoted, []byte("  VERSION_CODENAME=\"noble\" \n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := readDistroCodename(filepath.Join(dir, "missing"), quoted); got != "noble" {
		t.Errorf("readDistroCodename(missing, quoted) = %q, want %q", got, "noble")
	}

	ubuntuFallback := filepath.Join(dir, "os-release-ubuntu")
	if err := os.WriteFile(ubuntuFallback, []byte("ID=ubuntu\nUBUNTU_CODENAME='jammy'\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got := readDistroCodename(ubuntuFallback); got != "jammy" {
		t.Errorf("readDistroCodename(ubuntuFallback) = %q, want %q", got, "jammy")
	}

	if got := readDistroCodename(filepath.Join(dir, "nonexistent")); got != "" {
		t.Errorf("readDistroCodename(nonexistent) = %q, want empty", got)
	}
}
