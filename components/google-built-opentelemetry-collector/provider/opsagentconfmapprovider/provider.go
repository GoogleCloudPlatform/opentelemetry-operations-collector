package opsagentconfmapprovider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/collector/confmap"
	"gopkg.in/yaml.v3"

	"github.com/GoogleCloudPlatform/ops-agent/confgenerator"
	_ "github.com/GoogleCloudPlatform/ops-agent/apps"
)

type provider struct {
	settings confmap.ProviderSettings
}

func newProvider(settings confmap.ProviderSettings) *provider {
	return &provider{
		settings: settings,
	}
}

func (p *provider) Retrieve(ctx context.Context, uri string, watcher confmap.WatcherFunc) (*confmap.Retrieved, error) {
	// Parse the URI, which is expected to be a file path.
	filePath := strings.TrimPrefix(uri, "opsagentconfmap:")
	if strings.HasPrefix(filePath, "file:") {
		filePath = strings.TrimPrefix(filePath, "file:")
	}

	userConf, err := os.ReadFile(filePath)

	fmt.Printf("Read %d bytes from %s\n", len(userConf), filePath)

	fmt.Printf("Content: %q\n", string(userConf))
	if err != nil {
		return nil, fmt.Errorf("failed to read ops agent config file %q: %w", filePath, err)
	}

	// Use the AgentDefaults requested.
	defaults := confgenerator.AgentDefaults{
		DisableOtlpjsonFileCollection: true,
		DisableRubyRegex:              true,
		EnableOtlpExporterByDefault:   true,
	}

	uc, err := confgenerator.BuildUnifiedConfig(ctx, userConf, defaults)
	if err != nil {
		return nil, fmt.Errorf("failed to build unified config: %w", err)
	}

	// Default ops-agent paths
	outDir := "/var/run/google-cloud-ops-agent/opentelemetry-collector"
	stateDir := "/var/lib/google-cloud-ops-agent/opentelemetry-collector"
	logsDir := "/var/log/google-cloud-ops-agent/subagents"

	otelConfigStr, err := uc.GenerateOtelConfig(ctx, outDir, stateDir, logsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to generate otel config: %w", err)
	}

	// Dump the config for debugging
	_ = os.WriteFile("/tmp/generated-otel-config.yaml", []byte(otelConfigStr), 0644)
	fmt.Println("Generated OTel Config dumped to: /tmp/generated-otel-config.yaml")

	var otelConfigMap map[string]interface{}
	if err := yaml.Unmarshal([]byte(otelConfigStr), &otelConfigMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal generated otel config: %w", err)
	}

	return confmap.NewRetrieved(otelConfigMap)
}

func (p *provider) Scheme() string {
	return "opsagentconfmap"
}

func (p *provider) Shutdown(ctx context.Context) error {
	return nil
}
