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

// Package inprocesstelemetry provides a Collector telemetry.Factory that
// behaves exactly like the upstream otelconftelemetry factory, but
// additionally attaches an in-memory metric reader to the internal telemetry
// MeterProvider. The selftelemetry receiver reads from that reader, which lets
// the Collector feed its own metrics into a regular pipeline without exposing
// any port, socket or file to the host.
package inprocesstelemetry // import "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/receiver/selftelemetryreceiver/inprocesstelemetry"

import (
	"context"
	"errors"

	otelconf "go.opentelemetry.io/contrib/otelconf/v0.3.0"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/service/telemetry"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/receiver/selftelemetryreceiver/internal/inprocess"
)

var errMissingCollectorResource = errors.New("collector resource is required to create the meter provider")

// Config is the service::telemetry configuration.
//
// It is a defined type over upstream otelconftelemetry.Config, so it has the
// exact same fields, YAML schema, nested (un)marshalers and nested validators.
// The only difference is the top-level Validate method: upstream requires at
// least one metric reader whenever the metrics level is not "none", but with
// this factory the in-process reader always exists, so
// `service::telemetry::metrics::readers: []` (i.e. no host endpoint at all)
// is a valid configuration.
type Config otelconftelemetry.Config

// Validate mirrors otelconftelemetry.Config.Validate minus the "a reader must
// exist" rule. Nested fields (e.g. the resource config) are still validated
// by confmap's recursive validation.
func (c *Config) Validate() error {
	if c.Metrics.Views != nil && c.Metrics.Level != configtelemetry.LevelDetailed {
		return errors.New("service::telemetry::metrics::views can only be set when service::telemetry::metrics::level is detailed")
	}
	return nil
}

// Upstream returns c as the upstream otelconftelemetry configuration type.
func (c *Config) Upstream() *otelconftelemetry.Config {
	return (*otelconftelemetry.Config)(c)
}

// NewFactory returns a telemetry.Factory that delegates resource, logger and
// tracer creation to otelconftelemetry, and creates the MeterProvider the same
// way otelconftelemetry does plus one extra in-memory reader.
func NewFactory() telemetry.Factory {
	base := otelconftelemetry.NewFactory()
	unwrap := func(cfg component.Config) component.Config { return cfg.(*Config).Upstream() }
	return telemetry.NewFactory(
		func() component.Config {
			return (*Config)(base.CreateDefaultConfig().(*otelconftelemetry.Config))
		},
		telemetry.WithCreateResource(func(ctx context.Context, set telemetry.Settings, cfg component.Config) (pcommon.Resource, string, error) {
			return base.CreateResource(ctx, set, unwrap(cfg))
		}),
		telemetry.WithCreateLogger(func(ctx context.Context, set telemetry.LoggerSettings, cfg component.Config) (*zap.Logger, component.ShutdownFunc, error) {
			return base.CreateLogger(ctx, set, unwrap(cfg))
		}),
		telemetry.WithCreateMeterProvider(func(ctx context.Context, set telemetry.MeterSettings, cfg component.Config) (telemetry.MeterProvider, error) {
			return createMeterProvider(ctx, set, unwrap(cfg))
		}),
		telemetry.WithCreateTracerProvider(func(ctx context.Context, set telemetry.TracerSettings, cfg component.Config) (telemetry.TracerProvider, error) {
			return base.CreateTracerProvider(ctx, set, unwrap(cfg))
		}),
	)
}

// createMeterProvider mirrors otelconftelemetry's createMeterProvider
// (go.opentelemetry.io/collector/service/telemetry/otelconftelemetry/metrics.go)
// and adds the in-process reader via otelconf.WithMeterProviderOptions.
//
// Keep this in sync with upstream when bumping the Collector version.
func createMeterProvider(
	ctx context.Context,
	set telemetry.MeterSettings,
	componentConfig component.Config,
) (telemetry.MeterProvider, error) {
	cfg := componentConfig.(*otelconftelemetry.Config)
	if cfg.Metrics.MigratedFromV02 {
		set.Logger.Warn("Telemetry metrics configuration is using the deprecated v0.2.0 Declarative Configuration format, please migrate to the v0.3.0 format",
			zap.String("url", "https://opentelemetry.io/docs/specs/otel/configuration/#declarative-configuration"))
	}
	if cfg.Metrics.Level == configtelemetry.LevelNone {
		set.Logger.Info("Internal metrics telemetry disabled")
		return noopMeterProvider{MeterProvider: noopmetric.NewMeterProvider()}, nil
	} else if cfg.Metrics.Views == nil && set.DefaultViews != nil {
		cfg.Metrics.Views = set.DefaultViews(cfg.Metrics.Level)
	}

	resourceConfig, err := createFixedResourceConfig(set.Resource, set.SchemaURL)
	if err != nil {
		return nil, err
	}

	reader := inprocess.NewReader()
	mpConfig := cfg.Metrics.MeterProvider
	sdk, err := otelconf.NewSDK(
		otelconf.WithContext(ctx),
		otelconf.WithOpenTelemetryConfiguration(otelconf.OpenTelemetryConfiguration{
			Resource:      resourceConfig,
			MeterProvider: &mpConfig,
		}),
		otelconf.WithMeterProviderOptions(sdkmetric.WithReader(reader)),
	)
	if err != nil {
		inprocess.Release(reader)
		return nil, err
	}
	return &meterProvider{
		MeterProvider: sdk.MeterProvider().(telemetry.MeterProvider),
		reader:        reader,
	}, nil
}

// meterProvider unpublishes the in-process reader when shut down.
type meterProvider struct {
	telemetry.MeterProvider
	reader *sdkmetric.ManualReader
}

func (mp *meterProvider) Shutdown(ctx context.Context) error {
	inprocess.Release(mp.reader)
	return mp.MeterProvider.Shutdown(ctx)
}

// createFixedResourceConfig mirrors the unexported helper of the same name in
// otelconftelemetry/resource.go.
func createFixedResourceConfig(res *pcommon.Resource, schemaURL string) (*otelconf.Resource, error) {
	if res == nil {
		return nil, errMissingCollectorResource
	}

	providerConfig := &otelconf.Resource{
		Attributes: make([]otelconf.AttributeNameValue, 0, res.Attributes().Len()),
	}

	res.Attributes().Range(func(key string, value pcommon.Value) bool {
		providerConfig.Attributes = append(providerConfig.Attributes, otelconf.AttributeNameValue{
			Name:  key,
			Value: value.AsRaw(),
		})
		return true
	})
	if schemaURL != "" {
		providerConfig.SchemaUrl = &schemaURL
	}

	return providerConfig, nil
}

type noopMeterProvider struct {
	noopmetric.MeterProvider
}

func (noopMeterProvider) Shutdown(context.Context) error {
	return nil
}
