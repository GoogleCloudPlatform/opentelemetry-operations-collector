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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// withTestReader points new scrapers at a fresh in-test MeterProvider and
// returns a counter recorded through it.
func withTestReader(t *testing.T) metric.Int64Counter {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	prev := defaultReaderFn
	defaultReaderFn = func() *sdkmetric.ManualReader { return reader }
	t.Cleanup(func() { defaultReaderFn = prev })

	c, err := mp.Meter("test").Int64Counter("otelcol_test_counter")
	require.NoError(t, err)
	return c
}

func newTestReceiver(t *testing.T, mutate func(*Config), next consumer.Metrics) receiver.Metrics {
	t.Helper()
	cfg := createDefaultConfig().(*Config)
	if mutate != nil {
		mutate(cfg)
	}
	r, err := NewFactory().CreateMetrics(context.Background(), receivertest.NewNopSettings(componentType), cfg, next)
	require.NoError(t, err)
	return r
}

func counterValue(t *testing.T, md pmetric.Metrics) (int64, bool) {
	t.Helper()
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		sms := md.ResourceMetrics().At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				if ms.At(k).Name() == "otelcol_test_counter" {
					return ms.At(k).Sum().DataPoints().At(0).IntValue(), true
				}
			}
		}
	}
	return 0, false
}

func TestDefaultConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	assert.Equal(t, time.Minute, cfg.CollectionInterval)
	assert.Equal(t, 30*time.Second, cfg.Timeout)
}

// TestShutdownDeliversFinalCollection checks that values recorded after the
// last periodic collection are still delivered when the receiver shuts down.
func TestShutdownDeliversFinalCollection(t *testing.T) {
	ctx := context.Background()
	counter := withTestReader(t)
	sink := &consumertest.MetricsSink{}
	r := newTestReceiver(t, func(c *Config) {
		c.CollectionInterval = time.Hour // only the immediate first collection runs
		c.InitialDelay = 0
	}, sink)

	counter.Add(ctx, 1)
	require.NoError(t, r.Start(ctx, componenttest.NewNopHost()))
	require.Eventually(t, func() bool { return len(sink.AllMetrics()) == 1 }, 10*time.Second, 10*time.Millisecond)
	v, ok := counterValue(t, sink.AllMetrics()[0])
	require.True(t, ok)
	require.Equal(t, int64(1), v)

	// Recorded after the last periodic collection; previously lost.
	counter.Add(ctx, 4)
	require.NoError(t, r.Shutdown(ctx))

	all := sink.AllMetrics()
	require.Len(t, all, 2, "expected a final collection on shutdown")
	v, ok = counterValue(t, all[1])
	require.True(t, ok)
	assert.Equal(t, int64(5), v)
}

// blockingConsumer blocks every ConsumeMetrics call until its context is done.
type blockingConsumer struct{}

func (blockingConsumer) Capabilities() consumer.Capabilities { return consumer.Capabilities{} }

func (blockingConsumer) ConsumeMetrics(ctx context.Context, _ pmetric.Metrics) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestShutdownDoesNotHangWhenDownstreamBlocks checks that the collection
// timeout bounds both periodic and final collections, so a stuck downstream
// cannot block receiver shutdown forever.
func TestShutdownDoesNotHangWhenDownstreamBlocks(t *testing.T) {
	ctx := context.Background()
	counter := withTestReader(t)
	counter.Add(ctx, 1)
	r := newTestReceiver(t, func(c *Config) {
		c.CollectionInterval = time.Hour
		c.InitialDelay = 0
		c.Timeout = 200 * time.Millisecond
	}, blockingConsumer{})

	require.NoError(t, r.Start(ctx, componenttest.NewNopHost()))
	time.Sleep(50 * time.Millisecond) // let the first collection start blocking

	done := make(chan error, 1)
	go func() { done <- r.Shutdown(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown hung while downstream was blocked")
	}
}

func TestShutdownWithoutStart(t *testing.T) {
	withTestReader(t)
	sink := &consumertest.MetricsSink{}
	r := newTestReceiver(t, nil, sink)
	require.NoError(t, r.Shutdown(context.Background()))
	assert.Empty(t, sink.AllMetrics())
}

func TestShutdownWithoutReaderDeliversNothing(t *testing.T) {
	prev := defaultReaderFn
	defaultReaderFn = func() *sdkmetric.ManualReader { return nil }
	t.Cleanup(func() { defaultReaderFn = prev })

	ctx := context.Background()
	sink := &consumertest.MetricsSink{}
	r := newTestReceiver(t, func(c *Config) { c.InitialDelay = 0 }, sink)
	require.NoError(t, r.Start(ctx, componenttest.NewNopHost()))
	require.NoError(t, r.Shutdown(ctx))
	for _, md := range sink.AllMetrics() {
		assert.Zero(t, md.DataPointCount())
	}
}
