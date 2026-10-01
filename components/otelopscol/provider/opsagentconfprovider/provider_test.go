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
	"os"
	"path/filepath"
	"testing"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/googleclientauthextension"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/storage/filestorage"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/intervalprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstarttimeprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"
	"go.uber.org/zap"
)

func testFactories(t *testing.T) otelcol.Factories {
	t.Helper()

	var err error
	factories := otelcol.Factories{
		Telemetry: otelconftelemetry.NewFactory(),
	}

	factories.Receivers, err = otelcol.MakeFactoryMap[receiver.Factory](
		prometheusreceiver.NewFactory(),
	)
	require.NoError(t, err)

	factories.Processors, err = otelcol.MakeFactoryMap[processor.Factory](
		batchprocessor.NewFactory(),
		filterprocessor.NewFactory(),
		intervalprocessor.NewFactory(),
		metricstarttimeprocessor.NewFactory(),
		metricstransformprocessor.NewFactory(),
		resourcedetectionprocessor.NewFactory(),
		transformprocessor.NewFactory(),
	)
	require.NoError(t, err)

	factories.Exporters, err = otelcol.MakeFactoryMap[exporter.Factory](
		otlpexporter.NewFactory(),
	)
	require.NoError(t, err)

	factories.Extensions, err = otelcol.MakeFactoryMap[extension.Factory](
		filestorage.NewFactory(),
		googleclientauthextension.NewFactory(),
	)
	require.NoError(t, err)

	return factories
}

func writeTestConfig(t *testing.T, content string) (configFile string, stateDir string) {
	t.Helper()
	tmpDir := t.TempDir()
	configFile = filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0600))

	stateDir = filepath.Join(tmpDir, "state")
	t.Setenv("STATE_DIRECTORY", stateDir)
	return configFile, stateDir
}

func TestRetrieveEmptyConfig(t *testing.T) {
	configFile, stateDir := writeTestConfig(t, "")

	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	assert.Equal(t, "opsagentconf", p.Scheme())

	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)

	rawMap := conf.ToStringMap()
	assert.Contains(t, rawMap, "receivers")
	assert.Contains(t, rawMap, "processors")
	assert.Contains(t, rawMap, "exporters")
	assert.Contains(t, rawMap, "extensions")
	assert.Contains(t, rawMap, "service")

	assert.True(t, conf.IsSet("receivers::prometheus/agent_prometheus"))
	assert.True(t, conf.IsSet("processors::resourcedetection/_global_0"))
	assert.True(t, conf.IsSet("exporters::otlp_grpc/otlp_metrics"))
	assert.True(t, conf.IsSet("exporters::otlp_grpc/otlp_logs"))
	assert.Equal(t, filepath.Join(stateDir, "file_storage"), conf.Get("extensions::file_storage::directory"))
	assert.True(t, conf.IsSet("service::telemetry::metrics"))

	assert.Equal(t, []string{
		"transform/agent_prometheus_0",
		"transform/agent_prometheus_1",
		"transform/agent_prometheus_2",
		"transform/otel_0",
		"filter/otel_1",
		"filter/otel_2",
		"metricstransform/otel_3",
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}, conf.Get("service::pipelines::metrics/otel::processors"))

	assert.Equal(t, []string{
		"transform/agent_prometheus_0",
		"transform/agent_prometheus_1",
		"transform/agent_prometheus_2",
		"transform/loggingmetrics_0",
		"filter/loggingmetrics_1",
		"filter/loggingmetrics_2",
		"metricstransform/loggingmetrics_3",
		"transform/loggingmetrics_4",
		"interval/loggingmetrics_5",
		"metricstransform/loggingmetrics_6",
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}, conf.Get("service::pipelines::metrics/loggingmetrics::processors"))

	require.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieveNonExistentConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	missingConfig := filepath.Join(tmpDir, "does_not_exist.yaml")
	t.Setenv("STATE_DIRECTORY", filepath.Join(tmpDir, "state"))

	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+missingConfig, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)
	assert.True(t, conf.IsSet("service::pipelines::metrics/otel"))
	assert.True(t, conf.IsSet("service::pipelines::metrics/loggingmetrics"))
}

func TestRetrieveInvalidYAML(t *testing.T) {
	configFile, _ := writeTestConfig(t, "logging:\n  - invalid: [unclosed\n")

	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	_, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.ErrorContains(t, err, "failed to read config file")
}

func TestRetrieveDefaultStateDir(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(""), 0600))
	t.Setenv("STATE_DIRECTORY", "")

	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(defaultStateDir(), "file_storage"), conf.Get("extensions::file_storage::directory"))
}

func TestValidateCollectorConfig(t *testing.T) {
	testCases := []struct {
		name   string
		config string
	}{
		{
			name:   "empty_config",
			config: "",
		},
		{
			name: "empty_sections",
			config: `combined: {}
logging: {}
metrics: {}
traces: {}
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configFile, _ := writeTestConfig(t, tc.config)

			factories := testFactories(t)
			providerFactory := NewFactory()

			configProvider, err := otelcol.NewConfigProvider(otelcol.ConfigProviderSettings{
				ResolverSettings: confmap.ResolverSettings{
					URIs: []string{"opsagentconf:" + configFile},
					ProviderFactories: []confmap.ProviderFactory{
						providerFactory,
					},
				},
			})
			require.NoError(t, err)

			cfg, err := configProvider.Get(context.Background(), factories)
			require.NoError(t, err)
			require.NotNil(t, cfg)
			require.NoError(t, cfg.Validate())
			require.NoError(t, configProvider.Shutdown(context.Background()))
		})
	}
}

func TestRetrieveUnsupportedScheme(t *testing.T) {
	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	_, err := p.Retrieve(context.Background(), "file:/etc/config.yaml", nil)
	require.ErrorContains(t, err, "is not supported")
}
