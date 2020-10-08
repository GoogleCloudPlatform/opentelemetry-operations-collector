// Copyright 2020, Google Inc.
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

package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/service"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/version"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"
)

func main() {
	err := setConfigEnvVars()
	if err != nil {
		log.Fatalf("failed to build environment variables for config: %v", err)
	}

	factories, err := components()
	if err != nil {
		log.Fatalf("failed to build default components: %v", err)
	}

	info := component.ApplicationStartInfo{
		ExeName:  "google-cloud-metrics-agent",
		LongName: "Google Cloud Metrics Agent",
		Version:  version.Version,
		GitHash:  version.GitHash,
	}

	params := service.Parameters{Factories: factories, ApplicationStartInfo: info}

	if err := run(params); err != nil {
		log.Fatal(err)
	}
}

func runInteractive(params service.Parameters) error {
	app, err := service.New(params)
	if err != nil {
		return fmt.Errorf("failed to construct the application: %w", err)
	}

	err = app.Run()
	if err != nil {
		return fmt.Errorf("application run finished with error: %w", err)
	}

	return nil
}

func setConfigEnvVars() error {
	hostInfo, err := host.Info()
	if err != nil {
		return err
	}

	cores := runtime.NumCPU()

	memory, err := mem.VirtualMemory()
	if err != nil {
		return err
	}

	partitions, err := disk.Partitions(false)
	if err != nil {
		return err
	}

	var totalDiskCapacity uint64
	for _, partition := range partitions {
		disk, err := disk.Usage(partition.Mountpoint)
		if err != nil {
			return err
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

	os.Setenv("USERAGENT", userAgent)
	return nil
}
