// Copyright 2022 Google LLC
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

package casttosumprocessor

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor/processorhelper"
)

const (
	// The value of "type" key in configuration.
	typeStr = "casttosum"
)

func NewFactory() component.ProcessorFactory {
	return component.NewProcessorFactory(
		typeStr,
		createDefaultConfig,
		component.WithMetricsProcessor(createMetricsProcessor))
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
	processorConfig, ok := cfg.(*Config)
	if !ok {
		return nil, fmt.Errorf("configuration parsing error")
	}

	if err := processorConfig.Validate(); err != nil {
		return nil, err
	}

	metricsProcessor := newCastToSumProcessor(processorConfig, params.Logger)
	return processorhelper.NewMetricsProcessor(
		cfg,
		nextConsumer,
		metricsProcessor.ProcessMetrics,
		processorhelper.WithCapabilities(processorCapabilities))
}
