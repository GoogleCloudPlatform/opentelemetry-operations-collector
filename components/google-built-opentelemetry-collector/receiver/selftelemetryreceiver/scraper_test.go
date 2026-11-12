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
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/zap"
)

func TestScrapeConvertsAllInstrumentKinds(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(resource.NewSchemaless(attribute.String("service.name", "test-collector"))),
	)
	t.Cleanup(func() { _ = mp.Shutdown(ctx) })

	meter := mp.Meter("test/scope")
	counter, err := meter.Int64Counter("otelcol_test_counter")
	require.NoError(t, err)
	hist, err := meter.Float64Histogram("otelcol_test_histogram", metric.WithExplicitBucketBoundaries(1, 10))
	require.NoError(t, err)
	_, err = meter.Int64ObservableGauge("otelcol_test_gauge", metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
		o.Observe(42, metric.WithAttributes(attribute.String("k", "v")))
		return nil
	}))
	require.NoError(t, err)

	counter.Add(ctx, 3, metric.WithAttributes(attribute.String("exporter", "x")))
	hist.Record(ctx, 5)

	s := newSelfScraper(zap.NewNop())
	s.readerFn = func() *sdkmetric.ManualReader { return reader }
	require.NoError(t, s.start(ctx, componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, s.shutdown(ctx)) })

	md, err := s.scrape(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, md.ResourceMetrics().Len())

	rm := md.ResourceMetrics().At(0)
	svc, ok := rm.Resource().Attributes().Get("service.name")
	require.True(t, ok)
	assert.Equal(t, "test-collector", svc.Str())
	require.Equal(t, 1, rm.ScopeMetrics().Len())
	assert.Equal(t, "test/scope", rm.ScopeMetrics().At(0).Scope().Name())

	byName := map[string]pmetric.Metric{}
	ms := rm.ScopeMetrics().At(0).Metrics()
	for i := 0; i < ms.Len(); i++ {
		byName[ms.At(i).Name()] = ms.At(i)
	}

	c := byName["otelcol_test_counter"]
	require.Equal(t, pmetric.MetricTypeSum, c.Type())
	assert.True(t, c.Sum().IsMonotonic())
	assert.Equal(t, pmetric.AggregationTemporalityCumulative, c.Sum().AggregationTemporality())
	assert.Equal(t, int64(3), c.Sum().DataPoints().At(0).IntValue())
	exp, _ := c.Sum().DataPoints().At(0).Attributes().Get("exporter")
	assert.Equal(t, "x", exp.Str())

	h := byName["otelcol_test_histogram"]
	require.Equal(t, pmetric.MetricTypeHistogram, h.Type())
	assert.Equal(t, uint64(1), h.Histogram().DataPoints().At(0).Count())
	assert.Equal(t, []uint64{0, 1, 0}, h.Histogram().DataPoints().At(0).BucketCounts().AsRaw())

	g := byName["otelcol_test_gauge"]
	require.Equal(t, pmetric.MetricTypeGauge, g.Type())
	assert.Equal(t, int64(42), g.Gauge().DataPoints().At(0).IntValue())

	// A second scrape returns cumulative values again (not an empty batch).
	counter.Add(ctx, 2, metric.WithAttributes(attribute.String("exporter", "x")))
	md, err = s.scrape(ctx)
	require.NoError(t, err)
	ms = md.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics()
	for i := 0; i < ms.Len(); i++ {
		if ms.At(i).Name() == "otelcol_test_counter" {
			assert.Equal(t, int64(5), ms.At(i).Sum().DataPoints().At(0).IntValue())
		}
	}
}

func TestScrapeWithoutReaderIsEmpty(t *testing.T) {
	ctx := context.Background()
	s := newSelfScraper(zap.NewNop())
	s.readerFn = func() *sdkmetric.ManualReader { return nil }
	require.NoError(t, s.start(ctx, componenttest.NewNopHost()))
	t.Cleanup(func() { require.NoError(t, s.shutdown(ctx)) })

	md, err := s.scrape(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, md.DataPointCount())
}
