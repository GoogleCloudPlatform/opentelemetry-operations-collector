package googleservicecontrolexporter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"google.golang.org/api/impersonate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/credentials/oauth"
)

const (
	// The value of "type" key in configuration.
	typeStr = "googleservicecontrol"
)

var (
	// 16s is the Service Control API default:
	// https://github.com/googleapis/googleapis/blob/d68746128bbb1c5729ff97132f8532e36f796929/google/api/servicecontrol/v1/servicecontrol_grpc_service_config.json#L26
	// It is important to keep it as is: go/slm-monitoring-opentelemetry-batching.
	defaultTimeout  = 16 * time.Second
	defaultEndpoint = "servicecontrol.googleapis.com:443"
	clientProvider  = New
)

// Config defines configuration for Service Control Exporter
type Config struct {
	ServiceName               string `mapstructure:"service_name"`
	ConsumerProject           string `mapstructure:"consumer_project"`
	ServiceControlEndpoint    string `mapstructure:"service_control_endpoint"`
	ServiceConfigID           string `mapstructure:"service_config_id"`
	ImpersonateServiceAccount string `mapstructure:"impersonate_service_account"`
	// Whether to use servicecontrol library or raw sc client.
	// Defaults to `true`, so that existing customers are unaffected by changes.
	// See go/agent-gdce
	UseRawServicecontrolClient string `mapstructure:"use_raw_sc_client"`
	EnableDebugHeaders         bool   `mapstructure:"enable_debug_headers"`

	exporterhelper.TimeoutConfig `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct.
	configretry.BackOffConfig    `mapstructure:"retry_on_failure"`
	exporterhelper.QueueConfig   `mapstructure:"sending_queue"`
}

func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		exporter.WithMetrics(createMetricsExporter, component.StabilityLevelBeta))
}

func createDefaultConfig() component.Config {
	return &Config{
		TimeoutConfig:              exporterhelper.TimeoutConfig{Timeout: defaultTimeout},
		ServiceControlEndpoint:     defaultEndpoint,
		ImpersonateServiceAccount:  "",
		UseRawServicecontrolClient: "true",
		EnableDebugHeaders:         false,
		// The meaning of RetrySettings is described in
		// https://github.com/open-telemetry/opentelemetry-collector/blob/v0.54.0/exporter/exporterhelper/queued_retry.go#L38.
		// The defaults are ported from our collectd agent
		BackOffConfig: configretry.BackOffConfig{
			Enabled:         true,
			InitialInterval: 1 * time.Second,
			MaxInterval:     1 * time.Second,
			// Allow 1 regular metric submission + 1 retry + a couple of seconds in between.
			// It's important to keep it as is: go/slm-monitoring-opentelemetry-batching.
			MaxElapsedTime: defaultTimeout + defaultTimeout + 2*time.Second,
		},
		// QueueSettings are described in
		// https://github.com/open-telemetry/opentelemetry-collector/blob/v0.54.0/exporter/exporterhelper/queued_retry_inmemory.go.
		QueueConfig: exporterhelper.QueueConfig{
			Enabled:      true,
			NumConsumers: 10,
			// Limit queue size to prevent memory growing in case of API outage.
			// This queue grows only in case of retries.
			QueueSize: 3000,
		},
	}
}

func createMetricsExporter(ctx context.Context, settings exporter.Settings, cfg component.Config) (exporter.Metrics, error) {
	oCfg := cfg.(*Config)
	opts := []grpc.DialOption{}
	if oCfg.ServiceName == "" {
		return nil, fmt.Errorf("empty service_name")
	}
	if oCfg.ConsumerProject == "" {
		return nil, fmt.Errorf("empty consumer_project")
	}
	if oCfg.ServiceControlEndpoint == "" {
		return nil, fmt.Errorf("empty service_control_endpoint")
	}
	var credentials = google.NewDefaultCredentials()
	if oCfg.ImpersonateServiceAccount != "" {
		src, err := impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
			TargetPrincipal: oCfg.ImpersonateServiceAccount,
			Scopes:          []string{"https://www.googleapis.com/auth/cloud-platform"},
		})
		if err != nil {
			return nil, fmt.Errorf("Failed to impersonate serviceAccount: %w", err)
		}
		credentials = google.NewDefaultCredentialsWithOptions(google.DefaultCredentialsOptions{oauth.TokenSource{src}, nil})
	}

	opts = append(opts, grpc.WithCredentialsBundle(credentials))

	useRawServicecontrolClient := strings.TrimSpace(strings.ToLower(oCfg.UseRawServicecontrolClient)) == "true"
	c, err := clientProvider(oCfg.ServiceControlEndpoint, useRawServicecontrolClient, oCfg.EnableDebugHeaders, settings.Logger, opts...)
	if err != nil {
		return nil, err
	}

	exp := NewExporter(settings.Logger, c, oCfg.ServiceName, oCfg.ConsumerProject, oCfg.ServiceConfigID, oCfg.EnableDebugHeaders, settings.TelemetrySettings)
	return exporterhelper.NewMetrics(ctx, settings, cfg, exp.ConsumeMetrics,
		exporterhelper.WithCapabilities(exp.Capabilities()),
		exporterhelper.WithTimeout(oCfg.TimeoutConfig),
		exporterhelper.WithRetry(oCfg.BackOffConfig),
		exporterhelper.WithQueue(oCfg.QueueConfig),
		exporterhelper.WithStart(exp.Start),
	)
}
