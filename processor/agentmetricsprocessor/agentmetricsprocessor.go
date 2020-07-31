// Copyright 2020 OpenTelemetry Authors
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
	"regexp"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenterror"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/pdata"
	"go.opentelemetry.io/collector/consumer/pdatautil"
	"go.uber.org/zap"
)

// matches the string after the last "." (or the whole string if no ".")
var metricPostfixRegex = regexp.MustCompile(`([^.]*$)`)

type agentMetricsProcessor struct {
	logger            *zap.Logger
	next              consumer.MetricsConsumer
	prevCPUTimeValues map[string]float64
}

func newAgentMetricsProcessor(logger *zap.Logger, next consumer.MetricsConsumer) *agentMetricsProcessor {
	return &agentMetricsProcessor{logger: logger, next: next}
}

// GetCapabilities returns the Capabilities associated with the metrics transform processor.
func (mtp *agentMetricsProcessor) GetCapabilities() component.ProcessorCapabilities {
	return component.ProcessorCapabilities{MutatesConsumedData: true}
}

// Start is invoked during service startup.
func (*agentMetricsProcessor) Start(ctx context.Context, host component.Host) error {
	return nil
}

// Shutdown is invoked during service shutdown.
func (*agentMetricsProcessor) Shutdown(ctx context.Context) error {
	return nil
}

// ConsumeMetrics implements the MetricsProcessor interface.
func (mtp *agentMetricsProcessor) ConsumeMetrics(ctx context.Context, metrics pdata.Metrics) error {
	md := pdatautil.MetricsToInternalMetrics(metrics)

	var errors []error

	if err := combineProcessMetrics(md.ResourceMetrics()); err != nil {
		errors = append(errors, err)
	}

	if err := splitReadWriteBytesMetrics(md.ResourceMetrics()); err != nil {
		errors = append(errors, err)
	}

	if err := mtp.appendUtilizationMetrics(md.ResourceMetrics()); err != nil {
		errors = append(errors, err)
	}

	metrics = pdatautil.MetricsFromInternalMetrics(md)

	if err := mtp.next.ConsumeMetrics(ctx, metrics); err != nil {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return componenterror.CombineErrors(errors)
	}

	return nil
}
