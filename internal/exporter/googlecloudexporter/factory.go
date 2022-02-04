// Copyright 2022 Google LLC
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

package googlecloudexporter

import (
	"context"
	"time"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/collector"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const (
	// The value of "type" key in configuration.
	typeStr        = "googlecloud"
	defaultTimeout = 12 * time.Second // Consistent with Cloud Monitoring's timeout
)

// NewFactory creates a factory for the googlecloud exporter
func NewFactory() component.ExporterFactory {
	return exporterhelper.NewFactory(
		typeStr,
		createDefaultConfig,
		exporterhelper.WithTraces(createTracesExporter),
		exporterhelper.WithMetrics(createMetricsExporter),
	)
}

// createDefaultConfig creates the default configuration for exporter.
func createDefaultConfig() config.Exporter {
	cfg := &Config{
		ExporterSettings: config.NewExporterSettings(config.NewComponentID(typeStr)),
		TimeoutSettings:  exporterhelper.TimeoutSettings{Timeout: defaultTimeout},
		RetrySettings:    exporterhelper.DefaultRetrySettings(),
		QueueSettings:    exporterhelper.DefaultQueueSettings(),
		Config:           collector.DefaultConfig(),
	}
	return cfg
}

// createTracesExporter creates a trace exporter based on this config.
func createTracesExporter(
	_ context.Context,
	params component.ExporterCreateSettings,
	cfg config.Exporter) (component.TracesExporter, error) {
	eCfg := cfg.(*Config)
	tExp, err := collector.NewGoogleCloudTracesExporter(eCfg.Config, params.BuildInfo.Version, eCfg.Timeout)
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewTracesExporter(
		cfg,
		params,
		tExp.PushTraces,
		exporterhelper.WithShutdown(tExp.Shutdown),
		// Disable exporterhelper Timeout, since we are using a custom mechanism
		// within exporter itself
		exporterhelper.WithTimeout(exporterhelper.TimeoutSettings{Timeout: 0}),
		exporterhelper.WithQueue(eCfg.QueueSettings),
		exporterhelper.WithRetry(eCfg.RetrySettings))
}

// createMetricsExporter creates a metrics exporter based on this config.
func createMetricsExporter(
	ctx context.Context,
	params component.ExporterCreateSettings,
	cfg config.Exporter) (component.MetricsExporter, error) {
	eCfg := cfg.(*Config)
	mExp, err := collector.NewGoogleCloudMetricsExporter(ctx, eCfg.Config, params.TelemetrySettings.Logger, params.BuildInfo.Version, eCfg.Timeout)
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewMetricsExporter(
		cfg,
		params,
		mExp.PushMetrics,
		exporterhelper.WithShutdown(mExp.Shutdown),
		// Disable exporterhelper Timeout, since we are using a custom mechanism
		// within exporter itself
		exporterhelper.WithTimeout(exporterhelper.TimeoutSettings{Timeout: 0}),
		exporterhelper.WithQueue(eCfg.QueueSettings),
		exporterhelper.WithRetry(eCfg.RetrySettings))
}
