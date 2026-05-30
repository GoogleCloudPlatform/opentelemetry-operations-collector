// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package googlecloudopsagentprovider

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/collector/confmap"
	"go.uber.org/zap"
	yaml "gopkg.in/yaml.v3"

	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/provider/googlecloudopsagentprovider/internal/apps"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/provider/googlecloudopsagentprovider/internal/confgenerator"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/provider/googlecloudopsagentprovider/internal/platform"
)

const schemeName = "googlecloudopsagent"

// NewFactory creates a factory for the googlecloudopsagent provider.
func NewFactory() confmap.ProviderFactory {
	return confmap.NewProviderFactory(newProvider)
}

func newProvider(settings confmap.ProviderSettings) confmap.Provider {
	return &provider{
		logger: settings.Logger,
	}
}

type provider struct {
	logger *zap.Logger
}

func (p *provider) Scheme() string {
	return schemeName
}

func (p *provider) Shutdown(ctx context.Context) error {
	return nil
}

func (p *provider) Retrieve(ctx context.Context, uri string, watcher confmap.WatcherFunc) (*confmap.Retrieved, error) {
	if !strings.HasPrefix(uri, schemeName+":") {
		return nil, fmt.Errorf("unsupported scheme: %q", uri)
	}

	// e.g. googlecloudopsagent:/path/to/config.yaml?platform=linux
	opaque := strings.TrimPrefix(uri, schemeName+":")

	// Split path and query
	parts := strings.SplitN(opaque, "?", 2)
	path := parts[0]

	// Parse query parameters if present
	platformStr := ""
	stateDir := ""
	outDir := ""
	if len(parts) > 1 {
		q, err := url.ParseQuery(parts[1])
		if err != nil {
			return nil, fmt.Errorf("failed to parse query parameters: %w", err)
		}
		platformStr = q.Get("platform")
		stateDir = q.Get("state_dir")
		outDir = q.Get("out_dir")
	}

	// Read the file
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	otelYAML, err := p.translate(ctx, content, platformStr, stateDir, outDir)
	if err != nil {
		return nil, fmt.Errorf("failed to translate config: %w", err)
	}

	// Unmarshal the generated OTEL YAML into a map
	var rawConf map[string]any
	if err := yaml.Unmarshal([]byte(otelYAML), &rawConf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal generated OTEL config: %w", err)
	}

	return confmap.NewRetrieved(rawConf)
}

func (p *provider) translate(ctx context.Context, content []byte, platformStr, stateDir, outDir string) (string, error) {
	// 1. Parse platform
	pl := platform.FromContext(ctx) // start with detected
	if platformStr != "" {
		var pType platform.Type
		switch strings.ToLower(platformStr) {
		case "linux":
			pType = platform.Linux
		case "windows":
			pType = platform.Windows
		default:
			return "", fmt.Errorf("unsupported platform: %q", platformStr)
		}
		pl = platform.Platform{
			Type:     pType,
			HostInfo: pl.HostInfo, // keep host info if detected
		}
	}
	ctx = pl.TestContext(ctx)

	// 2. Parse config
	uc, err := confgenerator.UnmarshalYamlToUnifiedConfig(ctx, content)
	if err != nil {
		return "", fmt.Errorf("failed to parse config: %w", err)
	}

	// 3. Apply defaults for directories if not provided
	if stateDir == "" {
		if pl.Type == platform.Windows {
			stateDir = `C:\ProgramData\Google\Cloud Operations\Ops Agent`
		} else {
			stateDir = "/var/lib/google-cloud-ops-agent"
		}
	}
	if outDir == "" {
		if pl.Type == platform.Windows {
			outDir = `C:\ProgramData\Google\Cloud Operations\Ops Agent\run`
		} else {
			outDir = "/var/run/google-cloud-ops-agent/subagents/otel"
		}
	}

	// 4. Generate OTEL config
	otelYAML, err := uc.GenerateOtelConfig(ctx, outDir, stateDir)
	if err != nil {
		return "", fmt.Errorf("failed to generate OTEL config: %w", err)
	}

	return otelYAML, nil
}
