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
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/otelcol/otelcoltest"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"google.golang.org/grpc"
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
		def.UseRawServicecontrolClient = "true"
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
				UseRawServicecontrolClient: "false",
				EnableDebugHeaders:         false,
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
			factories.Exporters[component.MustNewType(typeStr)] = factory
			cfg, err := otelcoltest.LoadConfigAndValidate(filepath.Join("testdata", "config.yaml"), factories)

			require.Nil(t, err)
			require.NotNil(t, cfg)

			expConf := cfg.Exporters[component.NewIDWithName(component.MustNewType(typeStr), tc.name)]
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
			clientProvider = func(_ string, _ bool, _ bool, _ *zap.Logger, _ ...grpc.DialOption) (ServiceControlClient, error) {
				return nil, nil
			}
			factory := NewFactory()
			cfg := factory.CreateDefaultConfig()
			config, ok := cfg.(*Config)
			if !ok {
				t.Errorf("Didn't get config of expected type")
			}
			config.ServiceName = test.serviceName
			config.ConsumerProject = test.consumerProject
			config.TimeoutConfig.Timeout = test.timeout

			settings := exporter.Settings{
				TelemetrySettings: component.TelemetrySettings{
					Logger:         zap.NewNop(),
					TracerProvider: trace.NewNoopTracerProvider(),
					MeterProvider:  noopmetric.NewMeterProvider(),
				},
			}
			exporter, err := factory.CreateMetrics(context.Background(), settings, cfg)
			if test.wantError && ((err == nil) || (exporter != nil)) {
				t.Errorf("factory.CreateMetrics(zap.NewNop(), cfg) = (%v, %v), want (nil, error)", exporter, err)
			}

			if !test.wantError && ((err != nil) || (exporter == nil)) {
				t.Errorf("factory.CreateMetrics(zap.NewNop(), cfg) = (%v, %v), want (receiver.MetricExporter{}, nil)", exporter, err)
			}
		})
	}
}
