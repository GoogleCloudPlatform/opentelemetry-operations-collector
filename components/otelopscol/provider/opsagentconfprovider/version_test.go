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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDistro(t *testing.T) {
	tests := []struct {
		name string
		info hostInfo
		want string
	}{
		{
			name: "debian_bookworm",
			info: hostInfo{
				OS:              "linux",
				Platform:        "debian",
				PlatformFamily:  "debian",
				PlatformVersion: "12.9",
				DistroCodename:  "bookworm",
			},
			want: "bookworm",
		},
		{
			name: "ubuntu_noble",
			info: hostInfo{
				OS:              "linux",
				Platform:        "ubuntu",
				PlatformFamily:  "debian",
				PlatformVersion: "24.04",
				DistroCodename:  "noble",
			},
			want: "noble",
		},
		{
			name: "debian_empty_codename_fallback",
			info: hostInfo{
				OS:              "linux",
				Platform:        "debian",
				PlatformFamily:  "debian",
				PlatformVersion: "12.9",
			},
			want: "build_distro",
		},
		{
			name: "rocky_9_rhel_family",
			info: hostInfo{
				OS:              "linux",
				Platform:        "rocky",
				PlatformFamily:  "rhel",
				PlatformVersion: "9.5",
			},
			want: "el9",
		},
		{
			name: "rhel_10",
			info: hostInfo{
				OS:              "linux",
				Platform:        "rhel",
				PlatformFamily:  "rhel",
				PlatformVersion: "10.0",
			},
			want: "el10",
		},
		{
			name: "sles_15",
			info: hostInfo{
				OS:              "linux",
				Platform:        "sles",
				PlatformFamily:  "suse",
				PlatformVersion: "15.6",
			},
			want: "sles15",
		},
		{
			name: "opensuse_leap_15",
			info: hostInfo{
				OS:              "linux",
				Platform:        "opensuse-leap",
				PlatformFamily:  "suse",
				PlatformVersion: "15.6",
			},
			want: "sles15",
		},
		{
			name: "opensuse_leap_42_3_sles12",
			info: hostInfo{
				OS:              "linux",
				Platform:        "opensuse-leap",
				PlatformFamily:  "suse",
				PlatformVersion: "42.3",
			},
			want: "sles12",
		},
		{
			name: "windows_server_2016",
			info: hostInfo{
				OS:              "windows",
				Platform:        "Microsoft Windows Server 2016 Datacenter",
				PlatformVersion: "10.0.14393 Build 14393",
			},
			want: "windows-ltsc2016",
		},
		{
			name: "windows_server_2019",
			info: hostInfo{
				OS:              "windows",
				Platform:        "Microsoft Windows Server 2019 Datacenter",
				PlatformVersion: "10.0.17763",
			},
			want: "windows-ltsc2019",
		},
		{
			name: "windows_server_2022",
			info: hostInfo{
				OS:              "windows",
				Platform:        "Microsoft Windows Server 2022 Datacenter",
				PlatformVersion: "10.0.20348 Build 20348",
			},
			want: "windows-ltsc2022",
		},
		{
			name: "windows_server_2025",
			info: hostInfo{
				OS:              "windows",
				Platform:        "Microsoft Windows Server 2025 Datacenter",
				PlatformVersion: "26100",
			},
			want: "windows-ltsc2025",
		},
		{
			name: "windows_unlisted_real_build_defaults_to_ltsc2022",
			info: hostInfo{
				OS:              "windows",
				Platform:        "Microsoft Windows Server Future",
				PlatformVersion: "10.0.99999 Build 99999",
			},
			want: "windows-ltsc2022",
		},
		{
			name: "synthetic_windows_fallback",
			info: hostInfo{
				OS:              "windows",
				Platform:        "win_platform",
				PlatformVersion: "win_platform_version",
			},
			want: "build_distro",
		},
		{
			name: "synthetic_linux_fallback",
			info: hostInfo{
				OS:              "linux",
				Platform:        "linux_platform",
				PlatformVersion: "linux_platform_version",
			},
			want: "build_distro",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.info.buildDistro())
		})
	}
}

func TestVersionLabelAndUserAgent(t *testing.T) {
	info := hostInfo{
		OS:              "linux",
		Platform:        "debian",
		PlatformFamily:  "debian",
		PlatformVersion: "12.9",
		DistroCodename:  "bookworm",
	}

	assert.Equal(t,
		"google-cloud-ops-agent-metrics/latest-bookworm",
		info.versionLabel(),
	)
	assert.Equal(t,
		"Google-Cloud-Ops-Agent-Metrics/latest (BuildDistro=bookworm;Platform=linux;ShortName=debian;ShortVersion=12.9)",
		info.userAgent(),
	)
}

func TestReadDistroCodename(t *testing.T) {
	dir := t.TempDir()

	unquoted := filepath.Join(dir, "os-release-unquoted")
	require.NoError(t, os.WriteFile(unquoted, []byte("ID=debian\nVERSION_CODENAME=bookworm\n"), 0600))
	assert.Equal(t, "bookworm", readDistroCodename(unquoted))

	quoted := filepath.Join(dir, "os-release-quoted")
	require.NoError(t, os.WriteFile(quoted, []byte("  VERSION_CODENAME=\"noble\" \n"), 0600))
	assert.Equal(t, "noble", readDistroCodename(filepath.Join(dir, "missing"), quoted))

	ubuntuFallback := filepath.Join(dir, "os-release-ubuntu")
	require.NoError(t, os.WriteFile(ubuntuFallback, []byte("ID=ubuntu\nUBUNTU_CODENAME='jammy'\n"), 0600))
	assert.Equal(t, "jammy", readDistroCodename(ubuntuFallback))

	assert.Empty(t, readDistroCodename(filepath.Join(dir, "nonexistent")))
}
