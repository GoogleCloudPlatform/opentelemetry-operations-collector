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

	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/apps"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/self_metrics"
	"go.opentelemetry.io/collector/confmap"

	"go.uber.org/zap"
)

type provider struct {
	logger *zap.Logger
}

func NewFactory() confmap.ProviderFactory {
	return confmap.NewProviderFactory(createProvider)
}

func createProvider(set confmap.ProviderSettings) confmap.Provider {
	return &provider{
		logger: set.Logger,
	}
}

func (p *provider) Retrieve(ctx context.Context, uri string, watcher confmap.WatcherFunc) (*confmap.Retrieved, error) {
	if p.logger != nil {
		p.logger.Info("Retrieving config via opsagentconfprovider", zap.String("uri", uri))
	}

	configPath := strings.TrimPrefix(uri, "opsagentconf:")
	if configPath == "" {
		if runtime.GOOS == "windows" {
			configPath = filepath.Join("C:", "Program Files/Google/Cloud Operations/Ops Agent/config/config.yaml")
		} else {
			configPath = "/etc/google-cloud-ops-agent/config.yaml"
		}
	}

	outDir := os.Getenv("RUNTIME_DIRECTORY")
	if outDir == "" {
		if runtime.GOOS == "windows" {
			outDir = filepath.Join(os.Getenv("PROGRAMDATA"), "Google/Cloud Operations/Ops Agent/generated_configs/otel")
		} else {
			outDir = "/run/google-cloud-ops-agent"
		}
	}
	stateDir := os.Getenv("STATE_DIRECTORY")
	if stateDir == "" {
		if runtime.GOOS == "windows" {
			stateDir = filepath.Join(os.Getenv("PROGRAMDATA"), "Google/Cloud Operations/Ops Agent/run")
		} else {
			stateDir = "/var/lib/google-cloud-ops-agent"
		}
	}
	logsDir := os.Getenv("LOG_DIRECTORY")
	if logsDir == "" {
		logsDir = os.Getenv("LOGS_DIRECTORY")
	}
	if logsDir == "" {
		if runtime.GOOS == "windows" {
			logsDir = filepath.Join(os.Getenv("PROGRAMDATA"), "Google/Cloud Operations/Ops Agent/log")
		} else {
			logsDir = "/var/log/google-cloud-ops-agent"
		}
	}

	uc, err := confgenerator.MergeConfFiles(ctx, configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to merge config files: %w", err)
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create runtime directory %q: %w", outDir, err)
	}

	err = self_metrics.GenerateOpsAgentSelfMetricsOTLPJSON(ctx, configPath, outDir)
	if err != nil {
		return nil, fmt.Errorf("failed to generate self metrics: %w", err)
	}

	otelConfig, err := uc.GenerateOtelConfig(ctx, logsDir, outDir, stateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to generate otel config: %w", err)
	}

	if p.logger != nil {
		p.logger.Info("Generated OTEL config", zap.String("config", otelConfig))
	}

	return confmap.NewRetrievedFromYAML([]byte(otelConfig))
}

func (p *provider) Scheme() string {
	return "opsagentconf"
}

func (p *provider) Shutdown(ctx context.Context) error {
	return nil
}
