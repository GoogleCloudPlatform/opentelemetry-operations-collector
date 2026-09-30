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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.opentelemetry.io/collector/confmap"
	"go.uber.org/zap"
)

const schemeName = "opsagentconf"

type provider struct {
	logger *zap.Logger
}

// NewFactory returns a factory for the opsagentconf provider.
func NewFactory() confmap.ProviderFactory {
	return confmap.NewProviderFactory(createProvider)
}

func createProvider(set confmap.ProviderSettings) confmap.Provider {
	return &provider{
		logger: set.Logger,
	}
}

func (p *provider) Retrieve(ctx context.Context, uri string, _ confmap.WatcherFunc) (*confmap.Retrieved, error) {
	if !strings.HasPrefix(uri, schemeName+":") {
		return nil, fmt.Errorf("%q uri is not supported by %q provider", uri, schemeName)
	}

	if p.logger != nil {
		p.logger.Info("Retrieving config via opsagentconfprovider", zap.String("uri", uri))
	}

	configPath := strings.TrimPrefix(uri, schemeName+":")
	if configPath == "" {
		configPath = defaultConfigPath()
	}

	stateDir := os.Getenv("STATE_DIRECTORY")
	if stateDir == "" {
		stateDir = defaultStateDir()
	}

	cfg, err := readAndMergeConfigs(ctx, configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to merge config files: %w", err)
	}

	otelConfig, err := cfg.generateOtelConfig(ctx, stateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to generate otel config: %w", err)
	}

	if p.logger != nil {
		p.logger.Info("Generated OTEL config", zap.String("config", otelConfig))
	}

	return confmap.NewRetrievedFromYAML([]byte(otelConfig))
}

func (p *provider) Scheme() string {
	return schemeName
}

func (p *provider) Shutdown(context.Context) error {
	return nil
}

func defaultConfigPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join("C:", "Program Files", "Google", "Cloud Operations", "Ops Agent", "config", "config.yaml")
	}
	return "/etc/google-cloud-ops-agent/config.yaml"
}

func defaultStateDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("PROGRAMDATA"), "Google", "Cloud Operations", "Ops Agent", "run")
	}
	return "/var/lib/google-cloud-ops-agent"
}
