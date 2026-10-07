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
		DisableRubyRegex:              true,
		EnableOtlpExporterByDefault:   true,
		EnableOpsAgentHealthExtension: true,
	}

	uc, err := confgenerator.BuildUnifiedConfig(ctx, userConf, defaults)
	if err != nil {
		return nil, fmt.Errorf("failed to build unified config: %w", err)
	}

	// Default ops-agent paths (temporary for testing without sudo)
	outDir := "/tmp/test-ops-agent/run/google-cloud-ops-agent/opentelemetry-collector"
	stateDir := "/tmp/test-ops-agent/lib/google-cloud-ops-agent/opentelemetry-collector"
	logsDir := "/tmp/test-ops-agent/log/google-cloud-ops-agent/subagents"

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

	// Inject opsagenthealth extension
	exts, ok := otelConfigMap["extensions"].(map[string]interface{})
	if !ok {
		exts = map[string]interface{}{}
		otelConfigMap["extensions"] = exts
	}
	exts["opsagenthealth"] = map[string]interface{}{
		"config_path": filePath,
		"out_dir": outDir,
	}

	svc, ok := otelConfigMap["service"].(map[string]interface{})
	if !ok {
		svc = map[string]interface{}{}
		otelConfigMap["service"] = svc
	}
	svcExts, _ := svc["extensions"].([]interface{})
	svc["extensions"] = append(svcExts, "opsagenthealth")

	return confmap.NewRetrieved(otelConfigMap)
}

func (p *provider) Scheme() string {
	return "opsagentconfmap"
}

func (p *provider) Shutdown(ctx context.Context) error {
	return nil
}
