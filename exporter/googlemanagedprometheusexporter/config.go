// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package googlemanagedprometheusexporter // import "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/exporter/googlemanagedprometheusexporter"

import (
	"fmt"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/collector"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/collector/googlemanagedprometheus"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/featuregate"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// Config defines configuration for Google Cloud Managed Service for Prometheus exporter.
type Config struct {
	GMPConfig `mapstructure:",squash"`

	// Timeout for all API calls. If not set, defaults to 12 seconds.
	exporterhelper.TimeoutSettings `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct.
	exporterhelper.QueueSettings   `mapstructure:"sending_queue"`
	exporterhelper.RetrySettings   `mapstructure:"retry_on_failure"`
}

// GMPConfig is a subset of the collector config applicable to the GMP exporter.
type GMPConfig struct {
	ProjectID    string       `mapstructure:"project"`
	UserAgent    string       `mapstructure:"user_agent"`
	MetricConfig MetricConfig `mapstructure:"metric"`

	// Setting UntypedDoubleExport to true makes the collector double write prometheus
	// untyped metrics to GMP similar to the GMP collector. That is, it writes it once as
	// a gauge with the metric name suffix `unknown` and once as a counter with the
	// metric name suffix `unknown:counter`.
	// For the counter, if the point value is smaller than the previous point in the series
	// it is considered a reset point.
	UntypedDoubleExport bool `mapstructure:"untyped_double_export"`
}

type MetricConfig struct {
	// Prefix configures the prefix of metrics sent to GoogleManagedPrometheus.  Defaults to prometheus.googleapis.com.
	// Changing this prefix is not recommended, as it may cause metrics to not be queryable with promql in the Cloud Monitoring UI.
	Prefix       string                 `mapstructure:"prefix"`
	ClientConfig collector.ClientConfig `mapstructure:",squash"`
}

func (c *GMPConfig) toCollectorConfig() collector.Config {
	// start with whatever the default collector config is.
	cfg := collector.DefaultConfig()
	cfg.MetricConfig.Prefix = c.MetricConfig.Prefix
	if c.MetricConfig.Prefix == "" {
		cfg.MetricConfig.Prefix = "prometheus.googleapis.com"
	}
	cfg.MetricConfig.SkipCreateMetricDescriptor = true
	cfg.MetricConfig.InstrumentationLibraryLabels = false
	cfg.MetricConfig.ServiceResourceLabels = false
	// Update metric naming to match GMP conventions
	cfg.MetricConfig.GetMetricName = googlemanagedprometheus.GetMetricName
	// Map to the prometheus_target monitored resource
	cfg.MetricConfig.MapMonitoredResource = googlemanagedprometheus.MapToPrometheusTarget
	cfg.MetricConfig.EnableSumOfSquaredDeviation = true
	// map the GMP config's fields to the collector config
	cfg.ProjectID = c.ProjectID
	cfg.UserAgent = c.UserAgent
	cfg.MetricConfig.ClientConfig = c.MetricConfig.ClientConfig
	if c.UntypedDoubleExport {
		cfg.MetricConfig.ExtraMetrics = func(m pmetric.Metrics) pmetric.ResourceMetricsSlice {
			//nolint:errcheck
			featuregate.GlobalRegistry().Set("gcp.untyped_double_export", true)
			googlemanagedprometheus.AddUntypedMetrics(m)
			return m.ResourceMetrics()
		}
	}

	return cfg
}

func (cfg *Config) Validate() error {
	if err := collector.ValidateConfig(cfg.toCollectorConfig()); err != nil {
		return fmt.Errorf("exporter settings are invalid :%w", err)
	}
	return nil
}
