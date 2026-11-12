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

package inprocesstelemetry

import (
	"context"
	"errors"

	otelconf "go.opentelemetry.io/contrib/otelconf/v0.3.0"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/service/telemetry"
)

// attachLogBuffer tees the logger built by upstream otelconftelemetry into a
// new in-process LogBuffer and publishes that buffer for Logs().
//
// The logger itself is left exactly as upstream built it (stderr output,
// sampling, and any service::telemetry::logs::processors such as an OTLP
// exporter keep working), so the in-process copy is purely additive.
// Upstream's logger construction is not forked: the copy is made by an extra
// zap core wrapped around upstream's, backed by a dedicated LoggerProvider
// with the same resource as upstream's.
func attachLogBuffer(
	ctx context.Context,
	set telemetry.LoggerSettings,
	logger *zap.Logger,
	shutdown component.ShutdownFunc,
) (*zap.Logger, component.ShutdownFunc, error) {
	resourceConfig, err := createFixedResourceConfig(set.Resource, set.SchemaURL)
	if err != nil {
		return nil, nil, errors.Join(err, shutdown.Shutdown(ctx))
	}

	buf := NewLogBuffer(DefaultLogBufferCapacity)
	sdk, err := otelconf.NewSDK(
		otelconf.WithContext(ctx),
		otelconf.WithOpenTelemetryConfiguration(otelconf.OpenTelemetryConfiguration{
			Resource:       resourceConfig,
			LoggerProvider: &otelconf.LoggerProvider{},
		}),
		otelconf.WithLoggerProviderOptions(sdklog.WithProcessor(buf)),
	)
	if err != nil {
		return nil, nil, errors.Join(err, shutdown.Shutdown(ctx))
	}

	lp := sdk.LoggerProvider()
	logger = logger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return newInProcessCore(core, lp)
	}))
	publishLogBuffer(buf)

	return logger, func(ctx context.Context) error {
		releaseLogBuffer(buf)
		return errors.Join(sdk.Shutdown(ctx), shutdown.Shutdown(ctx))
	}, nil
}
