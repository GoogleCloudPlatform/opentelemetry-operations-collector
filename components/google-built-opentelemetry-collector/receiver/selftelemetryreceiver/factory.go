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
// inprocesstelemetry.NewFactory() telemetry factory, which attaches an
// in-memory reader to the internal telemetry MeterProvider. No host port,
// socket, named pipe or file is used.
package selftelemetryreceiver // import "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/receiver/selftelemetryreceiver"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/scraper"
	"go.opentelemetry.io/collector/scraper/scraperhelper"
)

const typeStr = "selftelemetry"

var componentType = component.MustNewType(typeStr)

// Config is the configuration of the selftelemetry receiver.
type Config struct {
	scraperhelper.ControllerConfig `mapstructure:",squash"`
}

func createDefaultConfig() component.Config {
	return &Config{
		ControllerConfig: scraperhelper.NewDefaultControllerConfig(),
	}
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
	sc, err := scraper.NewMetrics(s.scrape, scraper.WithStart(s.start), scraper.WithShutdown(s.shutdown))
	if err != nil {
		return nil, err
	}
	return scraperhelper.NewMetricsController(
		&rCfg.ControllerConfig,
		set,
		next,
		scraperhelper.AddMetricsScraper(componentType, sc),
	)
}
