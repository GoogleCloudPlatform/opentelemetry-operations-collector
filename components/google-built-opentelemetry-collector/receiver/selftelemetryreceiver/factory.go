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

// Package selftelemetryreceiver implements a receiver that emits the
// Collector's own internal metrics into a pipeline entirely in-process.
//
// It requires the Collector binary to be built with the
// inprocesstelemetry.NewFactory() telemetry factory
// (components/google-built-opentelemetry-collector/telemetry/inprocesstelemetry),
// which attaches an in-memory reader to the internal telemetry MeterProvider. No host port,
// socket, named pipe or file is used.
package selftelemetryreceiver // import "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/receiver/selftelemetryreceiver"

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/scraper"
	"go.opentelemetry.io/collector/scraper/scraperhelper"
	"go.uber.org/zap"
)

const (
	typeStr = "selftelemetry"

	// defaultTimeout bounds every collection, including delivery to the next
	// consumer. It matches the OTel SDK's default periodic reader export
	// timeout (OTEL_METRIC_EXPORT_TIMEOUT, 30s). Without it, a collection that
	// blocks would block receiver shutdown forever, because scraperhelper
	// never cancels the scrape context.
	defaultTimeout = 30 * time.Second
)

var componentType = component.MustNewType(typeStr)

// Config is the configuration of the selftelemetry receiver.
type Config struct {
	scraperhelper.ControllerConfig `mapstructure:",squash"`
}

func createDefaultConfig() component.Config {
	cfg := scraperhelper.NewDefaultControllerConfig()
	cfg.Timeout = defaultTimeout
	return &Config{ControllerConfig: cfg}
}

// NewFactory creates a factory for the selftelemetry receiver.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		componentType,
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, component.StabilityLevelDevelopment),
	)
}

func createMetricsReceiver(
	_ context.Context,
	set receiver.Settings,
	cfg component.Config,
	next consumer.Metrics,
) (receiver.Metrics, error) {
	rCfg := cfg.(*Config)
	s := newSelfScraper(set.Logger)
	// The scraper's shutdown is intentionally not registered with the
	// controller: selfTelemetryReceiver.Shutdown needs the scraper to still
	// be usable after the controller has stopped, for the final collection.
	sc, err := scraper.NewMetrics(s.scrape, scraper.WithStart(s.start))
	if err != nil {
		return nil, err
	}
	ctrl, err := scraperhelper.NewMetricsController(
		&rCfg.ControllerConfig,
		set,
		next,
		scraperhelper.AddMetricsScraper(componentType, sc),
	)
	if err != nil {
		return nil, err
	}
	return &selfTelemetryReceiver{
		Metrics: ctrl,
		scraper: s,
		next:    next,
		timeout: rCfg.Timeout,
		logger:  set.Logger,
	}, nil
}

// selfTelemetryReceiver wraps the scraperhelper controller to add a final
// collection on shutdown.
//
// The Collector shuts pipelines down in topological order, so when this
// receiver's Shutdown runs, every downstream processor and exporter is still
// running and able to accept (and later drain) the final batch. Without this,
// everything recorded since the last periodic collection would be lost.
type selfTelemetryReceiver struct {
	receiver.Metrics // the scraperhelper controller

	scraper *selfScraper
	next    consumer.Metrics
	timeout time.Duration
	logger  *zap.Logger
}

func (r *selfTelemetryReceiver) Shutdown(ctx context.Context) error {
	// Stop periodic collection and wait for any in-flight collection.
	errs := r.Metrics.Shutdown(ctx)

	// The final collection gets its own deadline so that a slow downstream
	// cannot consume the caller's shutdown budget for the steps below.
	finalCtx := ctx
	if r.timeout > 0 {
		var cancel context.CancelFunc
		finalCtx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}
	if err := r.finalCollect(finalCtx); err != nil {
		r.logger.Warn("Final collection of internal metrics on shutdown failed", zap.Error(err))
	}

	return errors.Join(errs, r.scraper.shutdown(ctx))
}

func (r *selfTelemetryReceiver) finalCollect(ctx context.Context) error {
	if !r.scraper.started() {
		return nil
	}
	md, err := r.scraper.scrape(ctx)
	if err != nil {
		return err
	}
	if md.DataPointCount() == 0 {
		return nil
	}
	return r.next.ConsumeMetrics(ctx, md)
}
