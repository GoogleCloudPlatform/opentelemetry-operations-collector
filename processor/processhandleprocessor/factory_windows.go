//go:build windows
// +build windows

package processhandleprocessor

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
)

func createMetricsProcessor(
	ctx context.Context,
	params processor.CreateSettings,
	cfg component.Config,
	nextConsumer consumer.Metrics,
) (processor.Metrics, error) {
	// NewMetricsProcess takes an MProcessor, which is what agentMetricsProcessor implements, and returns a MetricsProcessor.
	mProcessor := newProcessHandleProcessor(params.Logger, cfg.(*Config))
	return processorhelper.NewMetricsProcessor(
		ctx,
		params,
		cfg,
		nextConsumer,
		mProcessor.ProcessMetrics,
		processorhelper.WithCapabilities(processorCapabilities))
}
