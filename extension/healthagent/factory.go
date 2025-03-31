// Copyright 2025 Google LLC
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

package healthagent

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
)

const (
	// This is a default value for `error_check_interval`.
	// See go/ucp-metrics-agent-health-check-b309384363 for why the default is 65 seconds.
	defaultInterval = 65 * time.Second
	defaultPort     = "37123"
	defaultScope    = "otel"
	defaultName     = "google_built_opentelemetry_collector"
	typeStr         = "healthagent"
)

type Config struct {
	// Parameters for Instance Agent: go/slm-instance-agent#health-checking-containers.
	Scope string `mapstructure:"scope"`
	Name  string `mapstructure:"name"`
	Port  string `mapstructure:"port"`
	// Health check will report UNHEALTHY if there was an error during (time.Now() - error_check_interval, time.Now()]
	ErrorCheckInterval time.Duration `mapstructure:"error_check_interval"`
}

func createDefaultConfig() component.Config {
	return &Config{
		Scope:              defaultScope,
		Name:               defaultName,
		Port:               defaultPort,
		ErrorCheckInterval: defaultInterval,
	}
}

func NewFactory() extension.Factory {
	return extension.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		createExtension,
		component.StabilityLevelBeta)
}
func createExtension(_ context.Context, set extension.Settings, cfg component.Config) (extension.Extension, error) {
	config := cfg.(*Config)
	return newHealthAgent(*config, set), nil
}
