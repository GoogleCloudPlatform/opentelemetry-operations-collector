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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/xds/clients/xdsclient/metrics"
)

const xdsMeterName = "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"

// otelMetricsReporter implements clients.MetricsReporter using OpenTelemetry
// metric instruments per gRFC A78.
type otelMetricsReporter struct {
	target string
	meter  metric.Meter

	resourceUpdatesValid   metric.Int64Counter
	resourceUpdatesInvalid metric.Int64Counter
	serverFailure          metric.Int64Counter
	connectedGauge         metric.Int64ObservableGauge
	resourcesGauge         metric.Int64ObservableGauge

	mu             sync.Mutex
	asyncReporters map[clients.AsyncReporter]struct{}
	reg            metric.Registration
}

var _ clients.MetricsReporter = (*otelMetricsReporter)(nil)

func newOTelMetricsReporter(mp metric.MeterProvider, target string) *otelMetricsReporter {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	meter := mp.Meter(xdsMeterName)

	validCounter, _ := meter.Int64Counter(
		"grpc.xds_client.resource_updates_valid",
		metric.WithDescription("A counter of resources received that were considered valid."),
		metric.WithUnit("{resource}"),
	)
	invalidCounter, _ := meter.Int64Counter(
		"grpc.xds_client.resource_updates_invalid",
		metric.WithDescription("A counter of resources received that were considered invalid."),
		metric.WithUnit("{resource}"),
	)
	serverFailureCounter, _ := meter.Int64Counter(
		"grpc.xds_client.server_failure",
		metric.WithDescription("A counter of xDS servers going from healthy to unhealthy."),
		metric.WithUnit("{failure}"),
	)
	connectedGauge, _ := meter.Int64ObservableGauge(
		"grpc.xds_client.connected",
		metric.WithDescription("Whether or not the xDS client currently has a working ADS stream to the xDS server."),
		metric.WithUnit("{bool}"),
	)
	resourcesGauge, _ := meter.Int64ObservableGauge(
		"grpc.xds_client.resources",
		metric.WithDescription("How many xDS resources are currently associated with each xDS client cache state."),
		metric.WithUnit("{resource}"),
	)

	r := &otelMetricsReporter{
		target:                 target,
		meter:                  meter,
		resourceUpdatesValid:   validCounter,
		resourceUpdatesInvalid: invalidCounter,
		serverFailure:          serverFailureCounter,
		connectedGauge:         connectedGauge,
		resourcesGauge:         resourcesGauge,
		asyncReporters:         make(map[clients.AsyncReporter]struct{}),
	}

	reg, err := meter.RegisterCallback(r.observeAsyncMetrics, connectedGauge, resourcesGauge)
	if err == nil {
		r.reg = reg
	}
	return r
}

func (r *otelMetricsReporter) ReportMetric(m any) {
	ctx := context.Background()
	switch v := m.(type) {
	case *metrics.ResourceUpdateValid:
		r.resourceUpdatesValid.Add(ctx, 1, metric.WithAttributes(
			attribute.String("grpc.target", r.target),
			attribute.String("grpc.xds.server", v.ServerURI),
			attribute.String("grpc.xds.resource_type", v.ResourceType),
		))
	case *metrics.ResourceUpdateInvalid:
		r.resourceUpdatesInvalid.Add(ctx, 1, metric.WithAttributes(
			attribute.String("grpc.target", r.target),
			attribute.String("grpc.xds.server", v.ServerURI),
			attribute.String("grpc.xds.resource_type", v.ResourceType),
		))
	case *metrics.ServerFailure:
		r.serverFailure.Add(ctx, 1, metric.WithAttributes(
			attribute.String("grpc.target", r.target),
			attribute.String("grpc.xds.server", v.ServerURI),
		))
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

func (r *otelMetricsReporter) observeAsyncMetrics(_ context.Context, o metric.Observer) error {
	r.mu.Lock()
	reporters := make([]clients.AsyncReporter, 0, len(r.asyncReporters))
	for rep := range r.asyncReporters {
		reporters = append(reporters, rep)
	}
	r.mu.Unlock()

	rec := &otelAsyncRecorder{
		target:         r.target,
		observer:       o,
		connectedGauge: r.connectedGauge,
		resourcesGauge: r.resourcesGauge,
	}
	for _, rep := range reporters {
		_ = rep.Report(rec)
	}
	return nil
}

func (r *otelMetricsReporter) close() {
	r.mu.Lock()
	reg := r.reg
	r.reg = nil
	r.mu.Unlock()
	if reg != nil {
		_ = reg.Unregister()
	}
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
		a.observer.ObserveInt64(a.connectedGauge, v.Value, metric.WithAttributes(
			attribute.String("grpc.target", a.target),
			attribute.String("grpc.xds.server", v.ServerURI),
		))
	case *metrics.XDSClientResourceStats:
		a.observer.ObserveInt64(a.resourcesGauge, v.Count, metric.WithAttributes(
			attribute.String("grpc.target", a.target),
			attribute.String("grpc.xds.authority", v.Authority),
			attribute.String("grpc.xds.resource_type", v.ResourceType),
			attribute.String("grpc.xds.cache_state", v.CacheState),
		))
	}
}
