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
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

const (
	defaultMetricsPort            = 20201
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

// Combined represents combined telemetry receivers in the Ops Agent configuration.
type Combined struct{}

// Logging represents logging pipelines, receivers, and processors in the Ops Agent configuration.
type Logging struct{}

// Metrics represents metrics pipelines, receivers, and processors in the Ops Agent configuration.
type Metrics struct{}

// Traces represents traces pipelines in the Ops Agent configuration.
type Traces struct{}

func readConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) generateOtelConfig(_ context.Context, stateDir string) (map[string]any, error) {
	receivers := map[string]any{
		"prometheus/agent_prometheus": agentPrometheusReceiver(defaultMetricsPort),
	}

	processors := map[string]any{
		"resourcedetection/_global_0": map[string]any{
			"detectors": []string{"gcp"},
		},
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1": map[string]any{
			"strategy": "subtract_initial_point",
		},
		"batch/otlp_grpc/otlp_metrics_metrics_2": map[string]any{
			"send_batch_max_size": 200,
			"send_batch_size":     200,
			"timeout":             "200ms",
		},
	}

	exporters := map[string]any{
		"otlp_grpc/otlp_metrics": otlpExporter(),
		"otlp_grpc/otlp_logs":    otlpLogsExporter(),
	}

	extensions := map[string]any{
		fileStorageExtensionType:      fileStorageExtension(stateDir),
		googleClientAuthExtensionType: map[string]any{},
	}

	defaultMetricsProcessors := []string{
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}

	pipelines := map[string]any{
		"metrics/otel": map[string]any{
			"receivers":  []string{"prometheus/agent_prometheus"},
			"processors": defaultMetricsProcessors,
			"exporters":  []string{"otlp_grpc/otlp_metrics"},
		},
		"metrics/loggingmetrics": map[string]any{
			"receivers":  []string{"prometheus/agent_prometheus"},
			"processors": defaultMetricsProcessors,
			"exporters":  []string{"otlp_grpc/otlp_metrics"},
		},
	}

	service := map[string]any{
		"extensions": []string{fileStorageExtensionType, googleClientAuthExtensionType},
		"pipelines":  pipelines,
		"telemetry":  telemetryConfig(defaultMetricsPort),
	}

	return map[string]any{
		"receivers":  receivers,
		"processors": processors,
		"exporters":  exporters,
		"extensions": extensions,
		"service":    service,
	}, nil
}

func agentPrometheusReceiver(port int) map[string]any {
	return map[string]any{
		"config": map[string]any{
			"scrape_configs": []map[string]any{
				{
					"job_name":        "otel-collector",
					"scrape_interval": "1m",
					"static_configs": []map[string]any{
						{
							"targets": []string{fmt.Sprintf("0.0.0.0:%d", port)},
						},
					},
				},
			},
		},
	}
}

func otlpExporter() map[string]any {
	return map[string]any{
		"endpoint":      "telemetry.googleapis.com:443",
		"balancer_name": "pick_first",
		"auth": map[string]any{
			"authenticator": googleClientAuthExtensionType,
		},
	}
}

func otlpLogsExporter() map[string]any {
	cfg := otlpExporter()
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

func telemetryConfig(port int) map[string]any {
	return map[string]any{
		"metrics": map[string]any{
			"level": "detailed",
			"readers": []map[string]any{
				{
					"pull": map[string]any{
						"exporter": map[string]any{
							"prometheus": map[string]any{
								"host":                "0.0.0.0",
								"port":                port,
								"without_scope_info":  true,
								"without_units":       true,
								"without_type_suffix": true,
							},
						},
					},
				},
			},
		},
	}
}
