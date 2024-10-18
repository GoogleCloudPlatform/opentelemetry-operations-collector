// Copyright 2022 Google LLC
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

package service

import (
	"context"
	"fmt"
	"log"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/otelcol"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/env"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/levelchanger"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/version"

	envprovider "go.opentelemetry.io/collector/confmap/provider/envprovider"
	fileprovider "go.opentelemetry.io/collector/confmap/provider/fileprovider"
	httpprovider "go.opentelemetry.io/collector/confmap/provider/httpprovider"
	httpsprovider "go.opentelemetry.io/collector/confmap/provider/httpsprovider"
	yamlprovider "go.opentelemetry.io/collector/confmap/provider/yamlprovider"
)

func MainContext(ctx context.Context) {
	if err := env.Create(); err != nil {
		log.Printf("failed to build environment variables for config: %v", err)
	}

	info := component.BuildInfo{
		Command:     "google-cloud-metrics-agent",
		Description: "Google Cloud Metrics Agent",
		Version:     version.Version,
	}

	params := otelcol.CollectorSettings{
		Factories: components,
		BuildInfo: info,
		ConfigProviderSettings: otelcol.ConfigProviderSettings{
			ResolverSettings: confmap.ResolverSettings{
				ProviderFactories: []confmap.ProviderFactory{
					fileprovider.NewFactory(),
					envprovider.NewFactory(),
					yamlprovider.NewFactory(),
					httpprovider.NewFactory(),
					httpsprovider.NewFactory(),
				},
			},
		},
		LoggingOptions: []zap.Option{
			levelchanger.NewLevelChangerOption(
				zapcore.ErrorLevel,
				zapcore.DebugLevel,
				// We would like the Error logs from this file to be logged at Debug instead.
				// https://github.com/open-telemetry/opentelemetry-collector/blob/831373ae6c6959f6c9258ac585a2ec0ab19a074f/receiver/scraperhelper/scrapercontroller.go#L198
				levelchanger.FilePathLevelChangeCondition("scrapercontroller.go"),
			),
			levelchanger.NewLevelChangerOption(
				zapcore.WarnLevel,
				zapcore.DebugLevel,
				// This is a warning log that is written unless a certain featuregate is
				// enabled, but we don't want to turn the featuregate on.
				// https://github.com/open-telemetry/opentelemetry-collector/blob/8a2a1a58d13b5f492ebab2f40c51fdcb6fc452ce/internal/localhostgate/featuregate.go#L61-L68
				levelchanger.FilePathLevelChangeCondition("localhostgate/featuregate.go"),
			),
		},
	}

	if err := run(ctx, params); err != nil {
		log.Fatal(err)
	}
}

func runInteractive(ctx context.Context, params otelcol.CollectorSettings) error {
	cmd := otelcol.NewCommand(params)
	err := cmd.ExecuteContext(ctx)
	if err != nil {
		return fmt.Errorf("application run finished with error: %w", err)
	}

	return nil
}
