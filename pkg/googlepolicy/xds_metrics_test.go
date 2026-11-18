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

package googlepolicy

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/xdsclient/metrics"
)

// fakeAsyncReporter reports a connected ADS stream on every collection.
type fakeAsyncReporter struct{}

func (*fakeAsyncReporter) Report(rec clients.AsyncMetricsRecorder) error {
	rec.ReportMetric(&metrics.XDSClientConnected{ServerURI: "server", Value: 1})
	return nil
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

func sumInt64Counter(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, pt := range findInt64SumPoints(rm, name) {
		total += pt.Value
	}
	return total
}

func reportOneOfEachCounter(r *otelMetricsReporter) {
	r.ReportMetric(&metrics.ResourceUpdateValid{ServerURI: "server", ResourceType: "TelemetryCollector"})
	r.ReportMetric(&metrics.ResourceUpdateInvalid{ServerURI: "server", ResourceType: "TelemetryCollector"})
	r.ReportMetric(&metrics.ServerFailure{ServerURI: "server"})
}

var xdsCounterNames = []string{
	"grpc.xds_client.resource_updates_valid",
	"grpc.xds_client.resource_updates_invalid",
	"grpc.xds_client.server_failure",
}

// Each counter event must be recorded exactly once, however many times the
// MeterProvider is bound, unbound, and bound again.
func TestOTelMetricsReporter_RebindRecordsEachEventOnce(t *testing.T) {
	t.Cleanup(func() { SetXDSMeterProvider(nil) })

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	r := newOTelMetricsReporter(nil, "target")
	t.Cleanup(r.close)

	// Reported before any provider is bound: buffered, then replayed once.
	reportOneOfEachCounter(r)
	SetXDSMeterProvider(mp)
	rm := collectMetrics(t, reader)
	for _, name := range xdsCounterNames {
		assert.Equal(t, int64(1), sumInt64Counter(rm, name), "%s after first bind", name)
	}

	// Binding the same provider again must not replay the event.
	SetXDSMeterProvider(mp)
	rm = collectMetrics(t, reader)
	for _, name := range xdsCounterNames {
		assert.Equal(t, int64(1), sumInt64Counter(rm, name), "%s after binding the same provider twice", name)
	}

	// Reported while detached: buffered, then replayed once on the next bind.
	SetXDSMeterProvider(nil)
	reportOneOfEachCounter(r)
	SetXDSMeterProvider(mp)
	rm = collectMetrics(t, reader)
	for _, name := range xdsCounterNames {
		assert.Equal(t, int64(2), sumInt64Counter(rm, name), "%s after bind -> nil -> bind", name)
	}

	// Reported while bound: recorded directly.
	reportOneOfEachCounter(r)
	rm = collectMetrics(t, reader)
	for _, name := range xdsCounterNames {
		assert.Equal(t, int64(3), sumInt64Counter(rm, name), "%s after a direct report", name)
	}
}

// SetXDSMeterProvider(nil) must unregister the async callback from the
// provider it was bound to.
func TestOTelMetricsReporter_NilProviderDetachesCallback(t *testing.T) {
	t.Cleanup(func() { SetXDSMeterProvider(nil) })

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	r := newOTelMetricsReporter(nil, "target")
	t.Cleanup(r.close)
	unregister := r.RegisterAsyncReporter(&fakeAsyncReporter{})
	t.Cleanup(unregister)

	SetXDSMeterProvider(mp)
	connected := findInt64GaugePoints(collectMetrics(t, reader), "grpc.xds_client.connected")
	require.Len(t, connected, 1, "connected gauge must be observed while bound")
	assert.Equal(t, int64(1), connected[0].Value)

	SetXDSMeterProvider(nil)
	assert.Empty(t, findInt64GaugePoints(collectMetrics(t, reader), "grpc.xds_client.connected"),
		"connected gauge must not be observed after the provider is unset")
}

// A metric collection runs the async callback under the SDK pipeline lock,
// while a rebind calls into the SDK. The two must not deadlock.
func TestOTelMetricsReporter_ConcurrentCollectAndRebind(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	r := newOTelMetricsReporter(nil, "target")
	unregister := r.RegisterAsyncReporter(&fakeAsyncReporter{})
	SetXDSMeterProvider(mp)

	const iterations = 2000
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for range iterations {
			var rm metricdata.ResourceMetrics
			_ = reader.Collect(context.Background(), &rm)
		}
	}()
	go func() {
		defer wg.Done()
		for i := range iterations {
			if i%2 == 0 {
				SetXDSMeterProvider(nil)
			} else {
				SetXDSMeterProvider(mp)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range iterations {
			reportOneOfEachCounter(r)
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		// No cleanup here: it would block on the same locks.
		t.Fatal("concurrent metric collection and MeterProvider rebind did not finish; likely deadlock")
	}

	// Every event is recorded exactly once across all the rebinds.
	SetXDSMeterProvider(mp)
	rm := collectMetrics(t, reader)
	for _, name := range xdsCounterNames {
		assert.Equal(t, int64(iterations), sumInt64Counter(rm, name), name)
	}

	unregister()
	r.close()
	SetXDSMeterProvider(nil)
	_ = mp.Shutdown(context.Background())
}
