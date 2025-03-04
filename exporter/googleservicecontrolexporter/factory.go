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
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"google.golang.org/api/impersonate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/oauth"
)

const (
	// The value of "type" key in configuration.
	typeStr = "googleservicecontrol"
)

var (
	// 16s is the Service Control API default:
	// https://github.com/googleapis/googleapis/blob/d68746128bbb1c5729ff97132f8532e36f796929/google/api/servicecontrol/v1/servicecontrol_grpc_service_config.json#L26
	// It is important to keep it as is: go/slm-monitoring-opentelemetry-batching:
	// Metrics points should come in chronological order: https://cloud.google.com/monitoring/api/ref_v3/rest/v3/projects.timeSeries/create
	// Together with the RetrySettings on the exporter, this ensures metric at T
	// is received, or dropped, before the metric at T + ScrapeInterval is sent.
	defaultTimeout  = 16 * time.Second
	defaultEndpoint = "servicecontrol.googleapis.com:443"
	clientProvider  = NewServiceControllerClient
)

// Config defines configuration for Service Control Exporter
type Config struct {
	ServiceName               string `mapstructure:"service_name"`
	ConsumerProject           string `mapstructure:"consumer_project"`
	ServiceControlEndpoint    string `mapstructure:"service_control_endpoint"`
	ServiceConfigID           string `mapstructure:"service_config_id"`
	ImpersonateServiceAccount string `mapstructure:"impersonate_service_account"`
	// Whether to use servicecontrol library or raw sc client.
	// The Client Library's SC client supports authentications using ADC and WIF
	// https://cloud.google.com/kubernetes-engine/fleet-management/docs/use-workload-identity#authenticate_from_your_code
	// Defaults to `true`, so that existing customers are unaffected by changes.
	// See go/agent-gdce
	// TODO(b/400987158): remove the option and migrate all to Client Library.
	UseRawServiceControlClient string `mapstructure:"use_raw_sc_client"`
	EnableDebugHeaders         bool   `mapstructure:"enable_debug_headers"`
	DisableAuth                bool   `mapstructure:"disable_auth"`

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
		UseRawServiceControlClient: "true",
		EnableDebugHeaders:         false,
		DisableAuth:                false,
		// The meaning of RetrySettings is described in
		// https://github.com/open-telemetry/opentelemetry-collector/blob/v0.54.0/exporter/exporterhelper/queued_retry.go#L38.
		// The defaults are ported from our collectd agent
		BackOffConfig: configretry.BackOffConfig{
			Enabled:         true,
			InitialInterval: 1 * time.Second,
			MaxInterval:     1 * time.Second,
			// Allow 1 regular metric submission + 1 retry + a couple of seconds in between.
			// It's important to keep it as is: go/slm-monitoring-opentelemetry-batching:
			// Metrics points should come in chronological order: https://cloud.google.com/monitoring/api/ref_v3/rest/v3/projects.timeSeries/create
			// With MaxElapsedTime < ScrapeInterval, this ensures metric at T
			// is received, or dropped, before the metric at T + ScrapeInterval
			// is sent.
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

	if oCfg.DisableAuth {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		opts = append(opts, grpc.WithCredentialsBundle(credentials))
	}

	useRawServiceControlClient := strings.TrimSpace(strings.ToLower(oCfg.UseRawServiceControlClient)) == "true"
	c, err := clientProvider(oCfg.ServiceControlEndpoint, useRawServiceControlClient, oCfg.EnableDebugHeaders, settings.Logger, opts...)
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
