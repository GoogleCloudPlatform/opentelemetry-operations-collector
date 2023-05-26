//go:build !windows
// +build !windows

package processhandleprocessor

import (
	"context"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/collectorerror"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
)

func createMetricsProcessor(
	_ context.Context,
	_ processor.CreateSettings,
	_ component.Config,
	_ consumer.Metrics,
) (receiver.Metrics, error) {
	return nil, collectorerror.ErrGPUSupportDisabled
}
