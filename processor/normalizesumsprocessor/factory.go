// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package normalizesumsprocessor

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor/processorhelper"
)

const (
	// The value of "type" key in configuration.
	typeStr = "normalizesums"
)

func NewFactory() component.ProcessorFactory {
	return processorhelper.NewFactory(
		typeStr,
		createDefaultConfig,
		processorhelper.WithMetrics(createMetricsProcessor))
}

func createDefaultConfig() config.Processor {
	settings := config.NewProcessorSettings(config.NewComponentID(typeStr))
	return &Config{
		ProcessorSettings: &settings,
	}
}

var processorCapabilities = consumer.Capabilities{MutatesData: true}

func createMetricsProcessor(
	_ context.Context,
	params component.ProcessorCreateSettings,
	cfg config.Processor,
	nextConsumer consumer.Metrics,
) (component.MetricsProcessor, error) {
	oCfg := cfg.(*Config)
	if err := validateConfiguration(oCfg); err != nil {
		return nil, err
	}
	metricsProcessor := newNormalizeSumsProcessor(params.Logger)
	return processorhelper.NewMetricsProcessor(
		cfg,
		nextConsumer,
		metricsProcessor.ProcessMetrics,
		processorhelper.WithCapabilities(processorCapabilities))
}

// validateConfiguration validates the input configuration has all of the required fields for the processor
// An error is returned if there are any invalid inputs.
func validateConfiguration(config *Config) error {
	return nil
}
