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
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	scpb "cloud.google.com/go/servicecontrol/apiv1/servicecontrolpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/exportertest"
	"go.opentelemetry.io/collector/otelcol/otelcoltest"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	grpcmd "google.golang.org/grpc/metadata"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/exporter/googleservicecontrolexporter/internal/metadata"
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
				QueueConfig: configoptional.Some(exporterhelper.QueueBatchConfig{
					NumConsumers: 5,
					QueueSize:    1000,
					Sizer:        exporterhelper.RequestSizerTypeRequests,
				}),
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
			clientProvider = func(_ string, _ bool, _ bool, _ bool, _ *zap.Logger, _ ...grpc.DialOption) (ServiceControlClient, error) {
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
			}
		})
	}
}

func TestCreateLogsExporter(t *testing.T) {
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
			clientProvider = func(_ string, _ bool, _ bool, _ bool, _ *zap.Logger, _ ...grpc.DialOption) (ServiceControlClient, error) {
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
				logsExporter, err := factory.CreateLogs(context.Background(), exportertest.NewNopSettings(metadata.Type), config)
				assert.NoError(t, err)
				assert.NotNil(t, logsExporter)
				assert.NoError(t, logsExporter.Shutdown(context.Background()))
			}
		})
	}
}

func TestCreateClient(t *testing.T) {
	ctx := context.Background()
	settings := exportertest.NewNopSettings(metadata.Type)

	testCases := []struct {
		name                       string
		config                     Config
		expectedUseRawClient       bool
		expectedInsecure           bool
		expectedEnableDebugHeaders bool
		expectNumberOfOpts         int //No easy way to test dialOptions; will only check for number of options here
	}{
		{
			name: "raw client",
			config: Config{
				UseRawServiceControlClient: "true",
			},
			expectedUseRawClient: true,
			expectNumberOfOpts:   2, //userAgent and credentialBundle
		},
		{
			name: "library client",
			config: Config{
				UseRawServiceControlClient: "false",
			},
			expectedUseRawClient: false,
			expectNumberOfOpts:   1, //userAgent
		},
		{
			name: "insecure",
			config: Config{
				UseInsecure: true,
			},
			expectedInsecure:   true,
			expectNumberOfOpts: 2, //userAgent and insecureCredential
		},
		{
			name: "debug headers",
			config: Config{
				EnableDebugHeaders: true,
			},
			expectedEnableDebugHeaders: true,
			expectNumberOfOpts:         1, //userAgent
		},
		{
			name: "impersonation with library client",
			config: Config{
				ImpersonateServiceAccount:  "test@account.com",
				UseRawServiceControlClient: "false",
			},
			expectedUseRawClient: false,
			expectNumberOfOpts:   1, //userAgent
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedUseRawClient, capturedInsecure, capturedEnableDebugHeaders bool
			var capturedOpts []grpc.DialOption

			defaultClientProvider := clientProvider
			clientProvider = func(endpoint string, useRawServiceControlClient bool, insecure bool, enableDebugHeaders bool, logger *zap.Logger, opts ...grpc.DialOption) (ServiceControlClient, error) {
				capturedUseRawClient = useRawServiceControlClient
				capturedInsecure = insecure
				capturedEnableDebugHeaders = enableDebugHeaders
				capturedOpts = opts
				return nil, nil
			}
			defer func() {
				clientProvider = defaultClientProvider
			}()

			_, err := createClient(ctx, &tc.config, settings)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedUseRawClient, capturedUseRawClient)
			assert.Equal(t, tc.expectedInsecure, capturedInsecure)
			assert.Equal(t, tc.expectedEnableDebugHeaders, capturedEnableDebugHeaders)
			assert.Len(t, capturedOpts, tc.expectNumberOfOpts)
			for _, opt := range capturedOpts {
				assert.NotNil(t, opt)
			}
		})
	}
}

func TestCreateExporterClientFails(t *testing.T) {
	defaultClientProvider := clientProvider
	clientProvider = func(_ string, _ bool, _ bool, _ bool, _ *zap.Logger, _ ...grpc.DialOption) (ServiceControlClient, error) {
		return nil, fmt.Errorf("failed to create client")
	}
	defer func() {
		clientProvider = defaultClientProvider
	}()

	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)
	cfg.ServiceName = "test.service.name"
	cfg.ConsumerProject = "my-project-id"

	// Test metrics exporter creation failure
	mexp, err := factory.CreateMetrics(context.Background(), exportertest.NewNopSettings(metadata.Type), cfg)
	assert.Error(t, err)
	assert.Nil(t, mexp)
	assert.Contains(t, err.Error(), "failed to create client")

	// Test logs exporter creation failure
	lexp, err := factory.CreateLogs(context.Background(), exportertest.NewNopSettings(metadata.Type), cfg)
	assert.Error(t, err)
	assert.Nil(t, lexp)
	assert.Contains(t, err.Error(), "failed to create client")
}

func TestCreateClientUserAgent(t *testing.T) {
	scenarios := []struct {
		testName     string
		useRawClient string
	}{
		{
			testName:     "library_client",
			useRawClient: "false",
		},
		{
			testName:     "raw_client",
			useRawClient: "true",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.testName, func(t *testing.T) {
			defaultClientProvider := clientProvider
			clientProvider = func(endpoint string, useRawServiceControlClient bool, insecure bool, enableDebugHeaders bool, logger *zap.Logger, opts ...grpc.DialOption) (ServiceControlClient, error) {
				mockServerOpts := []grpc.DialOption{
					grpc.WithContextDialer(BufDialer),
				}
				opts = append(opts, mockServerOpts...)
				return defaultClientProvider(endpoint, useRawServiceControlClient, insecure, enableDebugHeaders, logger, opts...)
			}
			defer func() {
				clientProvider = defaultClientProvider
			}()

			ctx := context.Background()
			server, mockServer, listener, err := StartMockServer()
			require.NoError(t, err)
			defer StopMockServer(server, listener)
			defer server.Stop()

			var gotMetadata grpcmd.MD
			mockServer.SetReturnFunc(func(ctx context.Context, req *scpb.ReportRequest) (*scpb.ReportResponse, error) {
				gotMetadata, _ = grpcmd.FromIncomingContext(ctx)
				return &scpb.ReportResponse{}, nil
			})
			cfg := createDefaultConfig().(*Config)
			cfg.ServiceControlEndpoint = "bufconn"
			cfg.UseInsecure = true
			cfg.UseRawServiceControlClient = scenario.useRawClient

			settings := exportertest.NewNopSettings(metadata.Type)
			settings.BuildInfo.Description = "the-description"
			settings.BuildInfo.Version = "the-version"

			client, err := createClient(ctx, cfg, settings)
			require.NoError(t, err)
			defer (*client).Close()

			_, err = (*client).Report(ctx, &scpb.ReportRequest{})
			require.NoError(t, err)

			collectorUserAgent := fmt.Sprintf("%s/%s (%s/%s)",
				"the-description", "the-version", runtime.GOOS, runtime.GOARCH)

			if got, ok := gotMetadata["user-agent"]; !ok {
				t.Errorf("missing user-agent header")
			} else {
				if len(got) != 1 {
					t.Errorf("len(user-agent) = %d; want 1", len(got))
				}
				expected := fmt.Sprintf("%s %s%s", collectorUserAgent, grpcUserAgentPrefix, grpc.Version)
				assert.Equal(t, expected, got[0], "user-agent header mismatch")
			}
		})
	}
}
