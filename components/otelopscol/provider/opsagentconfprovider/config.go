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
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

const (
	fileStorageExtensionType      = "file_storage"
	googleClientAuthExtensionType = "googleclientauth"
)

// Config represents the Ops Agent configuration schema.
type Config struct {
	Combined *Combined `yaml:"combined,omitempty"`
	Logging  *Logging  `yaml:"logging,omitempty"`
	Metrics  *Metrics  `yaml:"metrics,omitempty"`
	Traces   *Traces   `yaml:"traces,omitempty"`
}

// Logging represents logging pipelines, receivers, and processors in the Ops Agent configuration.
type Logging struct{}

type collectorConfig struct {
	receivers  map[string]any
	processors map[string]any
	exporters  map[string]any
	extensions map[string]any
	pipelines  map[string]any
}

func readConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return &Config{}, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) generateOtelConfig(_ context.Context, stateDir string, info hostInfo) (map[string]any, error) {
	isWindows := info.OS == "windows"
	metricsCfg := mergeMetricsConfig(c.Metrics, isWindows)
	if err := metricsCfg.validate(c.Combined, isWindows); err != nil {
		return nil, err
	}
	if err := c.Traces.validate(c.Combined); err != nil {
		return nil, err
	}
	if err := c.Combined.validate(c.Traces != nil); err != nil {
		return nil, err
	}

	userAgent := info.userAgent()
	versionLabel := info.versionLabel()

	b := &collectorConfig{
		receivers: map[string]any{},
		processors: map[string]any{
			"resourcedetection/_global_0":                        gcpResourceDetectorProcessor(true),
			"metric_start_time/otlp_grpc/otlp_metrics_metrics_1": metricStartTimeProcessor(),
			"batch/otlp_grpc/otlp_metrics_metrics_2":             batchProcessor(),
		},
		exporters: map[string]any{
			"otlp_grpc/otlp_metrics": otlpExporter(userAgent),
			"otlp_grpc/otlp_logs":    otlpLogsExporter(userAgent),
		},
		extensions: map[string]any{
			fileStorageExtensionType:      fileStorageExtension(stateDir),
			googleClientAuthExtensionType: map[string]any{},
		},
		pipelines: map[string]any{},
	}

	defaultMetricsProcessors := []string{
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}

	b.addSelfMetrics(defaultMetricsPort, versionLabel, defaultMetricsProcessors)
	b.addMetricsPipelines(metricsCfg, c.Combined, isWindows, defaultMetricsProcessors)
	b.addTracesPipelines(c.Traces, c.Combined, userAgent)

	return map[string]any{
		"receivers":  b.receivers,
		"processors": b.processors,
		"exporters":  b.exporters,
		"extensions": b.extensions,
		"service": map[string]any{
			"extensions": []string{fileStorageExtensionType, googleClientAuthExtensionType},
			"pipelines":  b.pipelines,
			"telemetry":  telemetryConfig(defaultMetricsPort, metricsCfg.Service.LogLevel),
		},
	}, nil
}

func otlpExporter(userAgent string) map[string]any {
	return map[string]any{
		"endpoint":      "telemetry.googleapis.com:443",
		"balancer_name": "pick_first",
		"user_agent":    userAgent,
		"auth": map[string]any{
			"authenticator": googleClientAuthExtensionType,
		},
	}
}

func otlpLogsExporter(userAgent string) map[string]any {
	cfg := otlpExporter(userAgent)
	cfg["sending_queue"] = map[string]any{
		"enabled":           true,
		"queue_size":        20000000,
		"num_consumers":     10,
		"sizer":             "bytes",
		"block_on_overflow": true,
		"batch": map[string]any{
			"flush_timeout": "200ms",
			"min_size":      1000000,
			"max_size":      5000000,
			"sizer":         "bytes",
		},
		"storage": fileStorageExtensionType,
	}
	return cfg
}

func fileStorageExtension(stateDir string) map[string]any {
	return map[string]any{
		"directory":        filepath.Join(stateDir, "file_storage"),
		"create_directory": true,
	}
}
