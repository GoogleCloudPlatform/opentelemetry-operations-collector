package processhandleprocessor

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
)

var processorCapabilities = consumer.Capabilities{MutatesData: true}

const typeStr = "processhandleprocessor"

func NewFactory() processor.Factory {
	return processor.NewFactory(
		typeStr,
		createDefaultConfig,
		processor.WithMetrics(createMetricsProcessor, component.StabilityLevelBeta))
}
