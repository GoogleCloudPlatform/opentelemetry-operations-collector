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
	"strings"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/service"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"moul.io/zapfilter"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/env"
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
			logSpamFilterCore(),
		},
	}

	if err := run(params); err != nil {
		log.Fatal(err)
	}
}

// Returns a zapfilter core that will filter log spam from the otel collector.
// Upstream issue: https://github.com/open-telemetry/opentelemetry-collector/issues/3004
func logSpamFilterCore() zap.Option {
	logFilterFunc := func(entry zapcore.Entry, fields []zapcore.Field) bool {
		if strings.Contains(entry.Caller.File, "scrapercontroller.go") {
			return strings.Contains(entry.Message, "error reading process name for pid")
		}
		return true
	}

	return zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return zapfilter.NewFilteringCore(core, logFilterFunc)
	})
}

func runInteractive(params service.CollectorSettings) error {
	cmd := service.NewCommand(params)
	err := cmd.Execute()
	if err != nil {
		return fmt.Errorf("application run finished with error: %w", err)
	}

	return nil
}
