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
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
)

const (
	defaultOTLPGRPCEndpoint = "0.0.0.0:4317"
	metricsModeGCM          = "googlecloudmonitoring"
	metricsModeGMP          = "googlemanagedprometheus"
)

// Combined represents combined telemetry receivers in the Ops Agent configuration.
type Combined struct {
	Receivers map[string]CombinedReceiver `yaml:"receivers,omitempty"`
}

// CombinedReceiver represents a combined receiver in the Ops Agent configuration.
type CombinedReceiver struct {
	Type         string `yaml:"type"`
	GRPCEndpoint string `yaml:"grpc_endpoint,omitempty"`
	MetricsMode  string `yaml:"metrics_mode,omitempty"`
}

func (r CombinedReceiver) allowCustomProcessors() bool {
	return r.MetricsMode == metricsModeGCM
}

func (c *Combined) validate(hasTraces bool) error {
	if c == nil {
		return nil
	}
	for _, id := range slices.Sorted(maps.Keys(c.Receivers)) {
		if err := validateComponentID("combined", "receiver", id); err != nil {
			return err
		}
		r := c.Receivers[id]
		if r.Type != "otlp" {
			return fmt.Errorf("combined receiver %q with type %q is not supported", id, r.Type)
		}
		if r.GRPCEndpoint != "" {
			if err := validateHostPort(r.GRPCEndpoint); err != nil {
				return fmt.Errorf("combined receiver %q has invalid grpc_endpoint %q: %w", id, r.GRPCEndpoint, err)
			}
		}
		if r.MetricsMode != "" && r.MetricsMode != metricsModeGCM && r.MetricsMode != metricsModeGMP {
			return fmt.Errorf("combined receiver %q has invalid metrics_mode %q: must be one of [%s %s]", id, r.MetricsMode, metricsModeGCM, metricsModeGMP)
		}
		if !hasTraces {
			return fmt.Errorf("combined receiver %q found with no traces section; separate metrics and traces pipelines are required for this receiver, or an empty traces configuration if the data is being intentionally dropped", id)
		}
	}
	return nil
}

func validateHostPort(endpoint string) error {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return err
	}
	if host == "" {
		return fmt.Errorf("missing host")
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("invalid port %q", port)
	}
	return nil
}

func (r CombinedReceiver) otlpReceiver() map[string]any {
	endpoint := r.GRPCEndpoint
	if endpoint == "" {
		endpoint = defaultOTLPGRPCEndpoint
	}
	return map[string]any{
		"protocols": map[string]any{
			"grpc": map[string]any{
				"endpoint": endpoint,
			},
		},
	}
}

func (r CombinedReceiver) metricsExporterProcessors(processors map[string]any, baseExporterProcs []string) []string {
	if r.MetricsMode == metricsModeGCM {
		processors["resourcedetection/_global_1"] = gcpResourceDetectorProcessor(false)
		return append([]string{"resourcedetection/_global_1"}, baseExporterProcs...)
	}
	return baseExporterProcs
}

func (r CombinedReceiver) otlpMetricsProcessors(escapedID string) []namedProcessor {
	if r.MetricsMode == metricsModeGCM {
		return workloadMetricsProcessors(escapedID)
	}
	return gmpMetricsProcessors(escapedID)
}
