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

package googleservicecontrolexporter

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/otelcol/otelcoltest"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/exporter/googleservicecontrolexporter/internal/metadata"
)

func TestCreateDefaultConfig(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	if cfg == nil {
		t.Errorf("failed to create default config")
	}
}

func TestCreateExporterFromConfig(t *testing.T) {
	requiredParamsConfig := func() *Config {
		def := createDefaultConfig().(*Config)

		def.ServiceName = "test.service.name"
		def.ConsumerProject = "my-project-id"
		def.UseRawServiceControlClient = "true"
		return def
	}()
	tests := []struct {
		name string
		want *Config
	}{
		{
			name: "all_params",
			want: &Config{
				TimeoutConfig: exporterhelper.TimeoutConfig{Timeout: 10 * time.Second},
				BackOffConfig: configretry.BackOffConfig{
					Enabled:         true,
					InitialInterval: 5 * time.Second,
					MaxInterval:     10 * time.Second,
					MaxElapsedTime:  200 * time.Second,
				},
				QueueConfig: exporterhelper.QueueConfig{
					Enabled:      true,
					NumConsumers: 5,
					QueueSize:    1000,
				},
				ServiceControlEndpoint:     "test.googleapis.com:443",
				ConsumerProject:            "my-project-id",
				ServiceName:                "test.service.name",
				ServiceConfigID:            "111-222-333",
				ImpersonateServiceAccount:  "serviceAccount@myproject.iam.gserviceaccount.com",
				UseRawServiceControlClient: "false",
				EnableDebugHeaders:         false,
				UseInsecure:                false,
				LogConfig: LogConfig{
					DefaultLogName: "log-name",
					OperationName:  "test-operation-name",
				},
			},
		},
		{
			name: "required_params",
			want: requiredParamsConfig,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factories, err := otelcoltest.NopFactories()
			assert.NoError(t, err)

			factory := NewFactory()
			factories.Exporters[metadata.Type] = factory
			cfg, err := otelcoltest.LoadConfigAndValidate(filepath.Join("testdata", "config.yaml"), factories)

			require.Nil(t, err)
			require.NotNil(t, cfg)

			expConf := cfg.Exporters[component.NewIDWithName(metadata.Type, tc.name)]
			assert.Equal(t, tc.want, expConf)
		})
	}
}

func TestCreateMetricsExporter(t *testing.T) {
	scenarios := []struct {
		testName        string
		serviceName     string
		consumerProject string
		timeout         time.Duration
		wantError       bool
	}{
		{
			testName:        "Required fields provided",
			serviceName:     "ssa.googleapis.com",
			consumerProject: "161811806171",
			wantError:       false,
		},
		{
			testName:        "Timeout set",
			serviceName:     "ssa.googleapis.com",
			consumerProject: "161811806171",
			timeout:         time.Second,
			wantError:       false,
		},
		{
			testName:        "Missing serviceName",
			consumerProject: "161811806171",
			wantError:       true,
		},
		{
			testName:    "Missing consumerProject",
			serviceName: "ssa.googleapis.com",
			wantError:   true,
		},
	}

	for _, test := range scenarios {
		t.Run(test.testName, func(t *testing.T) {
			defaultClientProvider := clientProvider
			clientProvider = func(_ string, _ bool, _ bool, _ *zap.Logger, _ ...grpc.DialOption) (ServiceControlClient, error) {
				return nil, nil
			}
			defer func() {
				clientProvider = defaultClientProvider
			}()
			factory := NewFactory()
			cfg := factory.CreateDefaultConfig()
			config, ok := cfg.(*Config)
			if !ok {
				t.Errorf("Didn't get config of expected type")
			}
			config.ServiceName = test.serviceName
			config.ConsumerProject = test.consumerProject
			config.TimeoutConfig.Timeout = test.timeout

			err := config.Validate()
			if test.wantError {
				assert.Error(t, err)
			}

			if !test.wantError {
				assert.NoError(t, err)
				metricsExporter, err := factory.CreateMetrics(context.Background(), exportertest.NewNopSettings(metadata.Type), config)
				assert.NoError(t, err)
				assert.NotNil(t, metricsExporter)
				assert.NoError(t, metricsExporter.Shutdown(context.Background()))
				LogsExporter, err := factory.CreateLogs(context.Background(), exportertest.NewNopSettings(metadata.Type), config)
				assert.NoError(t, err)
				assert.NotNil(t, LogsExporter)
				assert.NoError(t, LogsExporter.Shutdown(context.Background()))
			}
		})
	}
}
