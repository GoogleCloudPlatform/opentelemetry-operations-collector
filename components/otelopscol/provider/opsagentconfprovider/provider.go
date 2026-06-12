package opsagentconfprovider

import (
	"context"
	"fmt"
	"net/url"
	"os"

	_ "github.com/GoogleCloudPlatform/ops-agent/apps"
	"github.com/GoogleCloudPlatform/ops-agent/confgenerator"
	"github.com/GoogleCloudPlatform/ops-agent/self_metrics"
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
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to parse uri %q: %w", uri, err)
	}

	configPath := u.Path
	if configPath == "" {
		configPath = u.Opaque
	}
	if configPath == "" {
		configPath = "/etc/google-cloud-ops-agent/config.yaml"
	}

	outDir := os.Getenv("RUNTIME_DIRECTORY")
	if outDir == "" {
		outDir = "/run/google-cloud-ops-agent"
	}
	stateDir := "/var/lib/google-cloud-ops-agent"

	uc, err := confgenerator.MergeConfFiles(ctx, configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to merge config files: %w", err)
	}

	err = self_metrics.GenerateOpsAgentSelfMetricsOTLPJSON(ctx, configPath, outDir)
	if err != nil {
		return nil, fmt.Errorf("failed to generate self metrics: %w", err)
	}

	otelConfig, err := uc.GenerateOtelConfig(ctx, outDir, stateDir)
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
