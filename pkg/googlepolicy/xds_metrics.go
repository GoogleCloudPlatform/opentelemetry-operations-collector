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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/xdsclient/metrics"
)

const xdsMeterName = "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"

var (
	xdsMetricsMu       sync.Mutex
	activeXDSReporters = make(map[*otelMetricsReporter]struct{})
	defaultXDSMeter    metric.MeterProvider
	defaultXDSMeterGen uint64
)

// SetXDSMeterProvider registers the OpenTelemetry MeterProvider used by active
// and future xDS policy managers that were not constructed with an explicit
// WithMeterProvider option. Calling SetXDSMeterProvider(nil) unregisters the
// provider (for example on extension shutdown).
func SetXDSMeterProvider(mp metric.MeterProvider) {
	xdsMetricsMu.Lock()
	defaultXDSMeterGen++
	gen := defaultXDSMeterGen
	defaultXDSMeter = mp
	reporters := make([]*otelMetricsReporter, 0, len(activeXDSReporters))
	for r := range activeXDSReporters {
		reporters = append(reporters, r)
	}
	xdsMetricsMu.Unlock()

	for _, r := range reporters {
		r.setFallbackMeterProvider(mp, gen)
	}
}

type counterKey struct {
	serverURI    string
	resourceType string
}

// otelMetricsReporter implements clients.MetricsReporter using OpenTelemetry
// metric instruments per gRFC A78.
type otelMetricsReporter struct {
	target     string
	explicitMP metric.MeterProvider
	logger     *zap.Logger

	// bindMu serializes MeterProvider binding/unbinding and close() without
	// ever being acquired from observeAsyncMetrics (which runs under the OTel
	// SDK pipeline lock). Lock order: bindMu -> OTel pipeline lock -> mu.
	bindMu   sync.Mutex
	boundGen uint64
	reg      metric.Registration
	closed   bool

	mu                     sync.Mutex
	resourceUpdatesValid   metric.Int64Counter
	resourceUpdatesInvalid metric.Int64Counter
	serverFailure          metric.Int64Counter

	pendingValid    map[counterKey]int64
	pendingInvalid  map[counterKey]int64
	pendingFailures map[string]int64
	asyncReporters  map[clients.AsyncReporter]struct{}
}

var _ clients.MetricsReporter = (*otelMetricsReporter)(nil)

func newOTelMetricsReporter(mp metric.MeterProvider, target string, logger *zap.Logger) *otelMetricsReporter {
	if logger == nil {
		logger = zap.NewNop()
	}
	r := &otelMetricsReporter{
		target:          target,
		explicitMP:      mp,
		logger:          logger,
		pendingValid:    make(map[counterKey]int64),
		pendingInvalid:  make(map[counterKey]int64),
		pendingFailures: make(map[string]int64),
		asyncReporters:  make(map[clients.AsyncReporter]struct{}),
	}

	if mp != nil {
		r.bindMeterProvider(mp, 0, false)
		return r
	}

	xdsMetricsMu.Lock()
	activeXDSReporters[r] = struct{}{}
	fallbackMP := defaultXDSMeter
	gen := defaultXDSMeterGen
	xdsMetricsMu.Unlock()

	r.setFallbackMeterProvider(fallbackMP, gen)
	return r
}

func (r *otelMetricsReporter) setFallbackMeterProvider(mp metric.MeterProvider, gen uint64) {
	if r.explicitMP != nil {
		return
	}
	r.bindMeterProvider(mp, gen, true)
}

func (r *otelMetricsReporter) bindMeterProvider(mp metric.MeterProvider, gen uint64, checkGen bool) {
	r.bindMu.Lock()
	defer r.bindMu.Unlock()

	if r.closed {
		return
	}
	if checkGen {
		if gen < r.boundGen {
			return
		}
		r.boundGen = gen
	}

	if r.reg != nil {
		_ = r.reg.Unregister()
		r.reg = nil
	}

	if mp == nil {
		r.mu.Lock()
		r.resourceUpdatesValid = nil
		r.resourceUpdatesInvalid = nil
		r.serverFailure = nil
		r.mu.Unlock()
		return
	}

	meter := mp.Meter(xdsMeterName)

	validCounter, err := meter.Int64Counter(
		"grpc.xds_client.resource_updates_valid",
		metric.WithDescription("A counter of resources received that were considered valid."),
		metric.WithUnit("{resource}"),
	)
	r.warnInstrumentError("grpc.xds_client.resource_updates_valid", err)
	invalidCounter, err := meter.Int64Counter(
		"grpc.xds_client.resource_updates_invalid",
		metric.WithDescription("A counter of resources received that were considered invalid."),
		metric.WithUnit("{resource}"),
	)
	r.warnInstrumentError("grpc.xds_client.resource_updates_invalid", err)
	serverFailureCounter, err := meter.Int64Counter(
		"grpc.xds_client.server_failure",
		metric.WithDescription("A counter of xDS servers going from healthy to unhealthy."),
		metric.WithUnit("{failure}"),
	)
	r.warnInstrumentError("grpc.xds_client.server_failure", err)
	connectedGauge, err := meter.Int64ObservableGauge(
		"grpc.xds_client.connected",
		metric.WithDescription("Whether or not the xDS client currently has a working ADS stream to the xDS server."),
		metric.WithUnit("{bool}"),
	)
	r.warnInstrumentError("grpc.xds_client.connected", err)
	resourcesGauge, err := meter.Int64ObservableGauge(
		"grpc.xds_client.resources",
		metric.WithDescription("How many xDS resources are currently associated with each xDS client cache state."),
		metric.WithUnit("{resource}"),
	)
	r.warnInstrumentError("grpc.xds_client.resources", err)

	// Only observe the gauges that were created.
	var gauges []metric.Observable
	if connectedGauge != nil {
		gauges = append(gauges, connectedGauge)
	}
	if resourcesGauge != nil {
		gauges = append(gauges, resourcesGauge)
	}
	if len(gauges) > 0 {
		// Capture the gauges created on this meter so a concurrent rebind
		// cannot mix instruments from two different meters in one callback
		// invocation.
		reg, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
			return r.observeAsyncMetrics(o, connectedGauge, resourcesGauge)
		}, gauges...)
		if err != nil {
			r.logger.Warn("Failed to register the xDS client metrics callback; "+
				"grpc.xds_client.connected and grpc.xds_client.resources may not be reported", zap.Error(err))
		}
		if reg != nil {
			r.reg = reg
		}
	}

	// A counter that could not be created stays nil, so ReportMetric keeps
	// buffering for it. Leave its pending counts in place for the next bind
	// instead of dropping them.
	var (
		pendingValid, pendingInvalid map[counterKey]int64
		pendingFailures              map[string]int64
	)
	r.mu.Lock()
	r.resourceUpdatesValid = validCounter
	r.resourceUpdatesInvalid = invalidCounter
	r.serverFailure = serverFailureCounter
	if validCounter != nil {
		pendingValid, r.pendingValid = r.pendingValid, make(map[counterKey]int64)
	}
	if invalidCounter != nil {
		pendingInvalid, r.pendingInvalid = r.pendingInvalid, make(map[counterKey]int64)
	}
	if serverFailureCounter != nil {
		pendingFailures, r.pendingFailures = r.pendingFailures, make(map[string]int64)
	}
	r.mu.Unlock()

	ctx := context.Background()
	for k, count := range pendingValid {
		validCounter.Add(ctx, count, metric.WithAttributes(
			attribute.String("grpc.target", r.target),
			attribute.String("grpc.xds.server", k.serverURI),
			attribute.String("grpc.xds.resource_type", k.resourceType),
		))
	}
	for k, count := range pendingInvalid {
		invalidCounter.Add(ctx, count, metric.WithAttributes(
			attribute.String("grpc.target", r.target),
			attribute.String("grpc.xds.server", k.serverURI),
			attribute.String("grpc.xds.resource_type", k.resourceType),
		))
	}
	for serverURI, count := range pendingFailures {
		serverFailureCounter.Add(ctx, count, metric.WithAttributes(
			attribute.String("grpc.target", r.target),
			attribute.String("grpc.xds.server", serverURI),
		))
	}
}

// warnInstrumentError logs a failure to create a metric instrument. The
// instrument may still be usable, so the caller carries on with whatever was
// returned.
func (r *otelMetricsReporter) warnInstrumentError(name string, err error) {
	if err != nil {
		r.logger.Warn("Failed to create xDS client metric instrument",
			zap.String("metric", name), zap.Error(err))
	}
}

func (r *otelMetricsReporter) ReportMetric(m any) {
	ctx := context.Background()
	switch v := m.(type) {
	case *metrics.ResourceUpdateValid:
		r.mu.Lock()
		counter := r.resourceUpdatesValid
		if counter == nil {
			r.pendingValid[counterKey{serverURI: v.ServerURI, resourceType: v.ResourceType}]++
		}
		r.mu.Unlock()
		if counter != nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("grpc.target", r.target),
				attribute.String("grpc.xds.server", v.ServerURI),
				attribute.String("grpc.xds.resource_type", v.ResourceType),
			))
		}
	case *metrics.ResourceUpdateInvalid:
		r.mu.Lock()
		counter := r.resourceUpdatesInvalid
		if counter == nil {
			r.pendingInvalid[counterKey{serverURI: v.ServerURI, resourceType: v.ResourceType}]++
		}
		r.mu.Unlock()
		if counter != nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("grpc.target", r.target),
				attribute.String("grpc.xds.server", v.ServerURI),
				attribute.String("grpc.xds.resource_type", v.ResourceType),
			))
		}
	case *metrics.ServerFailure:
		r.mu.Lock()
		counter := r.serverFailure
		if counter == nil {
			r.pendingFailures[v.ServerURI]++
		}
		r.mu.Unlock()
		if counter != nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("grpc.target", r.target),
				attribute.String("grpc.xds.server", v.ServerURI),
			))
		}
	}
}

func (r *otelMetricsReporter) RegisterAsyncReporter(reporter clients.AsyncReporter) func() {
	r.mu.Lock()
	r.asyncReporters[reporter] = struct{}{}
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		delete(r.asyncReporters, reporter)
		r.mu.Unlock()
	}
}

func (r *otelMetricsReporter) observeAsyncMetrics(
	o metric.Observer,
	connectedGauge metric.Int64ObservableGauge,
	resourcesGauge metric.Int64ObservableGauge,
) error {
	r.mu.Lock()
	reporters := make([]clients.AsyncReporter, 0, len(r.asyncReporters))
	for rep := range r.asyncReporters {
		reporters = append(reporters, rep)
	}
	r.mu.Unlock()

	rec := &otelAsyncRecorder{
		target:         r.target,
		observer:       o,
		connectedGauge: connectedGauge,
		resourcesGauge: resourcesGauge,
	}
	for _, rep := range reporters {
		_ = rep.Report(rec)
	}
	return nil
}

func (r *otelMetricsReporter) close() {
	if r.explicitMP == nil {
		xdsMetricsMu.Lock()
		delete(activeXDSReporters, r)
		xdsMetricsMu.Unlock()
	}

	r.bindMu.Lock()
	defer r.bindMu.Unlock()

	r.closed = true
	if r.reg != nil {
		_ = r.reg.Unregister()
		r.reg = nil
	}

	r.mu.Lock()
	r.resourceUpdatesValid = nil
	r.resourceUpdatesInvalid = nil
	r.serverFailure = nil
	r.mu.Unlock()
}

type otelAsyncRecorder struct {
	target         string
	observer       metric.Observer
	connectedGauge metric.Int64ObservableGauge
	resourcesGauge metric.Int64ObservableGauge
}

func (a *otelAsyncRecorder) ReportMetric(m any) {
	switch v := m.(type) {
	case *metrics.XDSClientConnected:
		if a.connectedGauge == nil {
			return
		}
		a.observer.ObserveInt64(a.connectedGauge, v.Value, metric.WithAttributes(
			attribute.String("grpc.target", a.target),
			attribute.String("grpc.xds.server", v.ServerURI),
		))
	case *metrics.XDSClientResourceStats:
		if a.resourcesGauge == nil {
			return
		}
		a.observer.ObserveInt64(a.resourcesGauge, v.Count, metric.WithAttributes(
			attribute.String("grpc.target", a.target),
			attribute.String("grpc.xds.authority", v.Authority),
			attribute.String("grpc.xds.resource_type", v.ResourceType),
			attribute.String("grpc.xds.cache_state", v.CacheState),
		))
	}
}
