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

package selftelemetryreceiver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/telemetry/inprocesstelemetry"
)

func TestLogsScrapeConvertsRecords(t *testing.T) {
	ctx := context.Background()
	buf := inprocesstelemetry.NewLogBuffer(16)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(buf),
		sdklog.WithResource(resource.NewSchemaless(attribute.String("service.name", "test-collector"))),
	)
	t.Cleanup(func() { _ = lp.Shutdown(ctx) })

	logger := lp.Logger("test/scope", otellog.WithInstrumentationAttributes(attribute.String("otelcol.component.id", "x")))
	var r otellog.Record
	r.SetBody(attribute.StringValue("hello"))
	r.SetSeverity(otellog.SeverityWarn)
	r.AddAttributes(attribute.String("k", "v"))
	logger.Emit(ctx, r)

	s := newSelfLogsScraper(zap.NewNop())
	s.bufferFn = func() *inprocesstelemetry.LogBuffer { return buf }
	require.NoError(t, s.start(ctx, componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, s.shutdown(ctx)) })

	ld, err := s.scrape(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, ld.LogRecordCount())

	rl := ld.ResourceLogs().At(0)
	v, ok := rl.Resource().Attributes().Get("service.name")
	require.True(t, ok)
	assert.Equal(t, "test-collector", v.Str())

	sl := rl.ScopeLogs().At(0)
	assert.Equal(t, "test/scope", sl.Scope().Name())
	id, ok := sl.Scope().Attributes().Get("otelcol.component.id")
	require.True(t, ok)
	assert.Equal(t, "x", id.Str())

	lr := sl.LogRecords().At(0)
	assert.Equal(t, "hello", lr.Body().Str())
	assert.Equal(t, plog.SeverityNumberWarn, lr.SeverityNumber())
	k, ok := lr.Attributes().Get("k")
	require.True(t, ok)
	assert.Equal(t, "v", k.Str())

	// The buffer is drained: a second scrape is empty.
	ld, err = s.scrape(ctx)
	require.NoError(t, err)
	assert.Zero(t, ld.LogRecordCount())
}

func TestLogsScrapeWarnsOnDroppedRecords(t *testing.T) {
	ctx := context.Background()
	buf := inprocesstelemetry.NewLogBuffer(1)
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(buf))
	t.Cleanup(func() { _ = lp.Shutdown(ctx) })
	for _, body := range []string{"a", "b", "c"} {
		var r otellog.Record
		r.SetBody(attribute.StringValue(body))
		lp.Logger("t").Emit(ctx, r)
	}

	core, logs := observer.New(zapcore.WarnLevel)
	s := newSelfLogsScraper(zap.New(core))
	s.bufferFn = func() *inprocesstelemetry.LogBuffer { return buf }
	require.NoError(t, s.start(ctx, componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, s.shutdown(ctx)) })

	ld, err := s.scrape(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, ld.LogRecordCount())
	require.Equal(t, 1, logs.Len())
	assert.Equal(t, uint64(2), logs.All()[0].ContextMap()["dropped_records"])
}

func TestLogsScrapeWithoutBuffer(t *testing.T) {
	ctx := context.Background()
	core, logs := observer.New(zapcore.WarnLevel)
	s := newSelfLogsScraper(zap.New(core))
	s.bufferFn = func() *inprocesstelemetry.LogBuffer { return nil }
	require.NoError(t, s.start(ctx, componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, s.shutdown(ctx)) })

	for range 2 {
		ld, err := s.scrape(ctx)
		require.NoError(t, err)
		assert.Zero(t, ld.LogRecordCount())
	}
	assert.Equal(t, 1, logs.Len(), "the missing-buffer warning is logged once")
}
