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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otelconf "go.opentelemetry.io/contrib/otelconf/v0.3.0"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/service/telemetry"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"
)

func meterSettings() telemetry.MeterSettings {
	res := pcommon.NewResource()
	res.Attributes().PutStr("service.name", "otelcol-test")
	return telemetry.MeterSettings{
		Settings: telemetry.Settings{
			BuildInfo: component.NewDefaultBuildInfo(),
			Resource:  &res,
		},
		Logger: zap.NewNop(),
	}
}

// noReaders returns the default config with the default Prometheus reader
// removed so the test does not bind localhost:8888.
func noReaders(t *testing.T, f telemetry.Factory) *Config {
	cfg, ok := f.CreateDefaultConfig().(*Config)
	require.True(t, ok)
	cfg.Upstream().Metrics.Readers = nil
	return cfg
}

func TestDefaultConfigMatchesUpstream(t *testing.T) {
	assert.Equal(t, otelconftelemetry.NewFactory().CreateDefaultConfig(), NewFactory().CreateDefaultConfig().(*Config).Upstream())
}

func TestConfigUnmarshalAndValidate(t *testing.T) {
	f := NewFactory()

	// Same YAML schema as upstream, including the default Prometheus reader
	// being replaced by an explicit empty list.
	cfg := f.CreateDefaultConfig().(*Config)
	require.NoError(t, confmap.NewFromStringMap(map[string]any{
		"logs":    map[string]any{"level": "warn"},
		"metrics": map[string]any{"level": "detailed", "readers": []any{}},
	}).Unmarshal(cfg))
	assert.Equal(t, configtelemetry.LevelDetailed, cfg.Upstream().Metrics.Level)
	assert.Empty(t, cfg.Upstream().Metrics.Readers)
	assert.Equal(t, "warn", cfg.Upstream().Logs.Level.String())

	// Upstream rejects "no readers"; we accept it because the in-process
	// reader always exists.
	require.Error(t, confmap.Validate(cfg.Upstream()))
	require.NoError(t, confmap.Validate(cfg))

	// Other upstream validation rules still apply.
	cfg.Upstream().Metrics.Level = configtelemetry.LevelNormal
	cfg.Upstream().Metrics.Views = []otelconf.View{{}}
	require.ErrorContains(t, confmap.Validate(cfg), "views can only be set")

	// Unknown keys are still rejected.
	cfg = f.CreateDefaultConfig().(*Config)
	require.Error(t, confmap.NewFromStringMap(map[string]any{"bogus": 1}).Unmarshal(cfg))

	// Marshaling produces exactly the upstream representation.
	ours, theirs := confmap.New(), confmap.New()
	require.NoError(t, ours.Marshal(f.CreateDefaultConfig()))
	require.NoError(t, theirs.Marshal(otelconftelemetry.NewFactory().CreateDefaultConfig()))
	assert.Equal(t, theirs.ToStringMap(), ours.ToStringMap())
}

func TestMeterProviderPublishesInProcessReader(t *testing.T) {
	ctx := context.Background()
	f := NewFactory()

	mp, err := f.CreateMeterProvider(ctx, meterSettings(), noReaders(t, f))
	require.NoError(t, err)

	r := Reader()
	require.NotNil(t, r)

	c, err := mp.Meter("test").Int64Counter("otelcol_test_counter")
	require.NoError(t, err)
	c.Add(ctx, 7)

	var rm metricdata.ResourceMetrics
	require.NoError(t, r.Collect(ctx, &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)
	sum := rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	assert.Equal(t, int64(7), sum.DataPoints[0].Value)
	v, ok := rm.Resource.Set().Value("service.name")
	require.True(t, ok)
	assert.Equal(t, "otelcol-test", v.AsString())

	require.NoError(t, mp.Shutdown(ctx))
	assert.Nil(t, Reader(), "reader must be released on shutdown")
}

func TestNewestMeterProviderWins(t *testing.T) {
	ctx := context.Background()
	f := NewFactory()

	mp1, err := f.CreateMeterProvider(ctx, meterSettings(), noReaders(t, f))
	require.NoError(t, err)
	r1 := Reader()

	mp2, err := f.CreateMeterProvider(ctx, meterSettings(), noReaders(t, f))
	require.NoError(t, err)
	r2 := Reader()
	require.NotSame(t, r1, r2)

	// Shutting down the old provider (as happens after a reload) must not
	// unpublish the new provider's reader.
	require.NoError(t, mp1.Shutdown(ctx))
	assert.Same(t, r2, Reader())
	require.NoError(t, mp2.Shutdown(ctx))
	assert.Nil(t, Reader())
}

func TestLevelNoneHasNoReader(t *testing.T) {
	ctx := context.Background()
	f := NewFactory()
	cfg := noReaders(t, f)
	cfg.Upstream().Metrics.Level = configtelemetry.LevelNone

	mp, err := f.CreateMeterProvider(ctx, meterSettings(), cfg)
	require.NoError(t, err)
	assert.Nil(t, Reader())
	require.NoError(t, mp.Shutdown(ctx))
}
