// Copyright 2020 Google LLC
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

package env

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/version"
)

func Create() error {
	userAgent, err := getUserAgent()
	if err != nil {
		return err
	}

	os.Setenv("USERAGENT", userAgent)
	return nil
}

func getUserAgent() (string, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return "", err
	}

	cores := runtime.NumCPU()

	memory, err := mem.VirtualMemory()
	if err != nil {
		return "", err
	}

	partitions, err := disk.Partitions(false)
	if err != nil {
		return "", err
	}

	var totalDiskCapacity uint64
	for _, partition := range partitions {
		disk, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			return "", err
		}
		totalDiskCapacity += disk.Total
	}

	platform := hostInfo.Platform
	if platform == "" {
		platform = "Unknown"
	}

	platformVersion := hostInfo.PlatformVersion
	if platformVersion != "" {
		platformVersion = fmt.Sprintf("v%v ", platformVersion)
	}

	userAgent := fmt.Sprintf(
		"Google Cloud Metrics Agent/%v (TargetPlatform=%v; Framework=OpenTelemetry Collector) %s %s(Cores=%v; Memory=%0.1fGB; Disk=%0.1fGB)",
		version.Version,
		strings.Title(runtime.GOOS),
		platform,
		platformVersion,
		cores,
		float64(memory.Total)/math.Pow(1024, 3),
		float64(totalDiskCapacity)/math.Pow(1024, 3),
	)

	return userAgent, nil
}
