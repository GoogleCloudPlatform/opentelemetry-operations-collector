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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/service/telemetry"
)

func record(body string) *sdklog.Record {
	var r sdklog.Record
	r.SetBody(attribute.StringValue(body))
	return &r
}

func bodies(records []sdklog.Record) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.Body().AsString())
	}
	return out
}

func TestLogBuffer_DrainInOrder(t *testing.T) {
	ctx := context.Background()
	b := NewLogBuffer(4)
	for _, s := range []string{"a", "b", "c"} {
		require.NoError(t, b.OnEmit(ctx, record(s)))
	}
	assert.Equal(t, 3, b.Len())

	got, dropped := b.Drain()
	assert.Equal(t, []string{"a", "b", "c"}, bodies(got))
	assert.Zero(t, dropped)
	assert.Zero(t, b.Len())

	got, dropped = b.Drain()
	assert.Empty(t, got)
	assert.Zero(t, dropped)
}

func TestLogBuffer_DropsOldestWhenFull(t *testing.T) {
	ctx := context.Background()
	b := NewLogBuffer(3)
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		require.NoError(t, b.OnEmit(ctx, record(s)))
	}
	got, dropped := b.Drain()
	assert.Equal(t, []string{"c", "d", "e"}, bodies(got))
	assert.Equal(t, uint64(2), dropped)

	// The drop count is reset by Drain, and the ring keeps working after wrapping.
	require.NoError(t, b.OnEmit(ctx, record("f")))
	got, dropped = b.Drain()
	assert.Equal(t, []string{"f"}, bodies(got))
	assert.Zero(t, dropped)
}

func TestLogBuffer_CopiesRecords(t *testing.T) {
	ctx := context.Background()
	b := NewLogBuffer(2)
	r := record("original")
	require.NoError(t, b.OnEmit(ctx, r))
	// The SDK reuses records after OnEmit returns.
	r.SetBody(attribute.StringValue("mutated"))

	got, _ := b.Drain()
	assert.Equal(t, []string{"original"}, bodies(got))
}

func TestLogBuffer_Shutdown(t *testing.T) {
	ctx := context.Background()
	b := NewLogBuffer(2)
	require.NoError(t, b.OnEmit(ctx, record("before")))
	assert.True(t, b.Enabled(ctx, sdklog.EnabledParameters{}))

	require.NoError(t, b.Shutdown(ctx))
	assert.False(t, b.Enabled(ctx, sdklog.EnabledParameters{}))
	require.NoError(t, b.OnEmit(ctx, record("after")))

	// Records buffered before shutdown can still be drained.
	got, _ := b.Drain()
	assert.Equal(t, []string{"before"}, bodies(got))
}

func TestNewLogBuffer_DefaultCapacity(t *testing.T) {
	assert.Equal(t, DefaultLogBufferCapacity, NewLogBuffer(0).capacity)
}

func loggerSettings(core zapcore.Core) telemetry.LoggerSettings {
	res := pcommon.NewResource()
	res.Attributes().PutStr("service.name", "otelcol-test")
	set := telemetry.LoggerSettings{
		Settings: telemetry.Settings{
			BuildInfo: component.NewDefaultBuildInfo(),
			Resource:  &res,
		},
	}
	if core != nil {
		set.BuildZapLogger = func(zap.Config, ...zap.Option) (*zap.Logger, error) {
			return zap.New(core), nil
		}
	}
	return set
}

func TestCreateLogger_PublishesLogBuffer(t *testing.T) {
	ctx := context.Background()
	f := NewFactory()

	observed, stderr := observer.New(zapcore.InfoLevel)
	logger, shutdown, err := f.CreateLogger(ctx, loggerSettings(observed), f.CreateDefaultConfig())
	require.NoError(t, err)

	buf := Logs()
	require.NotNil(t, buf)

	logger.Info("hello", zap.String("k", "v"))
	logger.Debug("filtered out by the logger's level")

	// The regular (stderr) output is unchanged.
	assert.Equal(t, 1, stderr.FilterMessage("hello").Len())

	got, dropped := buf.Drain()
	require.Len(t, got, 1)
	assert.Zero(t, dropped)
	assert.Equal(t, "hello", got[0].Body().AsString())
	assert.Equal(t, log.SeverityInfo, got[0].Severity())
	assert.Equal(t, loggerScopeName, got[0].InstrumentationScope().Name)

	var attrs []attribute.KeyValue
	got[0].WalkAttributes(func(kv attribute.KeyValue) bool {
		attrs = append(attrs, kv)
		return true
	})
	assert.Contains(t, attrs, attribute.String("k", "v"))

	res := got[0].Resource()
	v, ok := res.Set().Value("service.name")
	require.True(t, ok)
	assert.Equal(t, "otelcol-test", v.AsString())

	require.NoError(t, shutdown.Shutdown(ctx))
	assert.Nil(t, Logs(), "shutting the logger down unpublishes its buffer")
}

func TestCreateLogger_GateDisabled(t *testing.T) {
	defaultGate(t)
	ctx := context.Background()
	f := NewFactory()

	observed, _ := observer.New(zapcore.InfoLevel)
	logger, shutdown, err := f.CreateLogger(ctx, loggerSettings(observed), f.CreateDefaultConfig())
	require.NoError(t, err)
	logger.Info("hello")
	assert.Nil(t, Logs())
	require.NoError(t, shutdown.Shutdown(ctx))
}

// scopeField builds a zap field with the same shape (an InlineMarshaler with a
// type named scopeAttributesField) as the one upstream attaches to component
// loggers. The real one lives in an internal upstream package; the
// selftelemetry receiver's end-to-end test covers it.
type scopeAttributesField struct{ fields []zapcore.Field }

func (s scopeAttributesField) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	for _, f := range s.fields {
		f.AddTo(enc)
	}
	return nil
}

func TestExtractScopeAttributes(t *testing.T) {
	// Not recognized: wrong package path (this test package), so it is treated
	// as a regular field.
	f := zap.Inline(scopeAttributesField{fields: []zapcore.Field{zap.String("otelcol.component.id", "x")}})
	_, ok := extractScopeAttributes(f)
	assert.False(t, ok)

	_, ok = extractScopeAttributes(zap.String("a", "b"))
	assert.False(t, ok)
}

func TestToAttribute(t *testing.T) {
	assert.Equal(t, attribute.String("s", "v"), toAttribute("s", "v"))
	assert.Equal(t, attribute.Bool("b", true), toAttribute("b", true))
	assert.Equal(t, attribute.Int64("i", 3), toAttribute("i", int64(3)))
	assert.Equal(t, attribute.Float64("f", 1.5), toAttribute("f", 1.5))
	assert.Equal(t, attribute.String("o", "[1 2]"), toAttribute("o", []int{1, 2}))
}
