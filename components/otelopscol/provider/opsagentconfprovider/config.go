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

import "context"

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

func readAndMergeConfigs(_ context.Context, _ string) (*Config, error) {
	return &Config{}, nil
}

func (c *Config) generateOtelConfig(_ context.Context, _ string) (string, error) {
	return "", nil
}
