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

package main

import (
	"fmt"
	"log"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/service"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/env"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/levelchanger"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/version"
)

func main() {
	if err := env.Create(); err != nil {
		log.Fatalf("failed to build environment variables for config: %v", err)
	}

	factories, err := components()
	if err != nil {
		log.Fatalf("failed to build default components: %v", err)
	}

	info := component.BuildInfo{
		Command:     "google-cloud-metrics-agent",
		Description: "Google Cloud Metrics Agent",
		Version:     version.Version,
	}

	params := service.CollectorSettings{
		Factories: factories,
		BuildInfo: info,
		LoggingOptions: []zap.Option{
			levelchanger.NewLevelChangerOption(
				zapcore.ErrorLevel,
				zapcore.DebugLevel,
				// We would like the Error logs from this file to be logged at Debug instead.
				// https://github.com/open-telemetry/opentelemetry-collector/blob/831373ae6c6959f6c9258ac585a2ec0ab19a074f/receiver/scraperhelper/scrapercontroller.go#L198
				levelchanger.FilePathLevelChangeCondition("scrapercontroller.go")),
		},
	}

	if err := run(params); err != nil {
		log.Fatal(err)
	}
}

func runInteractive(params service.CollectorSettings) error {
	cmd := service.NewCommand(params)
	err := cmd.Execute()
	if err != nil {
		return fmt.Errorf("application run finished with error: %w", err)
	}

	return nil
}
