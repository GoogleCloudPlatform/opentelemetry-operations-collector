// Copyright 2020, Google Inc.
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

package agentmetricsprocessor

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configerror"
	"go.opentelemetry.io/collector/config/configmodels"
	"go.opentelemetry.io/collector/consumer"
)

const (
	// The value of "type" key in configuration.
	typeStr = "agentmetrics"
)

// Factory is the factory for metrics transform processor.
type Factory struct{}

// Type gets the type of the Option config created by this factory.
func (f *Factory) Type() configmodels.Type {
	return typeStr
}

func NewFactory() *Factory {
	return &Factory{}
}

// CreateDefaultConfig creates the default configuration for processor.
func (f *Factory) CreateDefaultConfig() configmodels.Processor {
	return &Config{
		ProcessorSettings: configmodels.ProcessorSettings{
			TypeVal: typeStr,
			NameVal: typeStr,
		},
	}
}

// CreateTraceProcessor creates a trace processor based on this config.
func (f *Factory) CreateTraceProcessor(
	_ context.Context,
	_ component.ProcessorCreateParams,
	_ consumer.TraceConsumer,
	_ configmodels.Processor,
) (component.TraceProcessor, error) {
	return nil, configerror.ErrDataTypeIsNotSupported
}

// CreateMetricsProcessor creates a metrics processor based on this config.
func (f *Factory) CreateMetricsProcessor(
	_ context.Context,
	params component.ProcessorCreateParams,
	nextConsumer consumer.MetricsConsumer,
	_ configmodels.Processor,
) (component.MetricsProcessor, error) {
	return newAgentMetricsProcessor(params.Logger, nextConsumer), nil
}

// CreateLogsProcessor creates a logs processor based on this config.
func (f *Factory) CreateLogsProcessor(
	_ context.Context,
	_ component.ProcessorCreateParams,
	_ configmodels.Processor,
	_ consumer.LogsConsumer,
) (component.LogsProcessor, error) {
	return nil, configerror.ErrDataTypeIsNotSupported
}
