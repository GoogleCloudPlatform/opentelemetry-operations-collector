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

//go:build !windows

package platform

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/accelerators"
)

func (p *Platform) detectPlatform() {
	p.Type = Linux
	p.DistroCodename = readDistroCodename("/etc/os-release", "/usr/lib/os-release")
	if hasGpu, err := accelerators.HasNvidiaGpu(); err != nil {
		log.Printf("Failed to look up GPU devices: %s", err)
		p.HasNvidiaGpu = false
	} else {
		p.HasNvidiaGpu = hasGpu
	}
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
			if trimmed := strings.Trim(strings.TrimSpace(val), `"'`); trimmed != "" {
				return trimmed
			}
		}
		if val, ok := strings.CutPrefix(line, "UBUNTU_CODENAME="); ok {
			ubuntuCodename = strings.Trim(strings.TrimSpace(val), `"'`)
		}
	}
	return ubuntuCodename
}

func GetOldWinlogChannels() ([]string, error) {
	return nil, fmt.Errorf("not a Windows platform")
}
