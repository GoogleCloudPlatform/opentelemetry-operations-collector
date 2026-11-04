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
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/zap"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/receiver/selftelemetryreceiver/internal/inprocess"
)

// selfScraper pulls the Collector's internal metrics from the in-process
// reader and converts them to pdata.
//
// The conversion from the OTel Go SDK's metricdata representation to OTLP is
// done by the SDK's own OTLP/HTTP exporter, whose HTTP client is given an
// in-memory http.RoundTripper. The "request" never leaves the process: the
// RoundTripper decodes the OTLP protobuf body straight into pmetric.Metrics.
type selfScraper struct {
	logger *zap.Logger

	// readerFn returns the reader to collect from; overridable in tests.
	readerFn func() *sdkmetric.ManualReader

	mu        sync.Mutex
	transport *inMemoryTransport
	exporter  sdkmetric.Exporter
	warnOnce  sync.Once
}

func newSelfScraper(logger *zap.Logger) *selfScraper {
	return &selfScraper{
		logger:    logger,
		readerFn:  inprocess.Reader,
		transport: &inMemoryTransport{},
	}
}

func (s *selfScraper) start(ctx context.Context, _ component.Host) error {
	exp, err := otlpmetrichttp.New(ctx,
		// The endpoint is never dialed; the HTTP client's transport is in-memory.
		otlpmetrichttp.WithEndpoint("selftelemetry.invalid"),
		otlpmetrichttp.WithInsecure(),
		otlpmetrichttp.WithCompression(otlpmetrichttp.NoCompression),
		otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}),
		otlpmetrichttp.WithTimeout(time.Minute),
		otlpmetrichttp.WithHTTPClient(&http.Client{Transport: s.transport}),
	)
	if err != nil {
		return fmt.Errorf("failed to create in-memory OTLP converter: %w", err)
	}
	s.mu.Lock()
	s.exporter = exp
	s.mu.Unlock()
	return nil
}

func (s *selfScraper) shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exporter == nil {
		return nil
	}
	err := s.exporter.Shutdown(ctx)
	s.exporter = nil
	return err
}

func (s *selfScraper) scrape(ctx context.Context) (pmetric.Metrics, error) {
	reader := s.readerFn()
	if reader == nil {
		s.warnOnce.Do(func() {
			s.logger.Warn("No in-process telemetry reader is available; either internal metrics are disabled " +
				"(service::telemetry::metrics::level: none) or this binary was not built with the inprocesstelemetry factory")
		})
		return pmetric.NewMetrics(), nil
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		return pmetric.NewMetrics(), fmt.Errorf("failed to collect internal metrics: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exporter == nil {
		return pmetric.NewMetrics(), errors.New("selftelemetry receiver is not started")
	}
	if err := s.exporter.Export(ctx, &rm); err != nil {
		return pmetric.NewMetrics(), fmt.Errorf("failed to convert internal metrics: %w", err)
	}
	return s.transport.take(), nil
}

// inMemoryTransport is an http.RoundTripper that accepts an OTLP/HTTP metrics
// export request and keeps its decoded payload instead of sending it anywhere.
type inMemoryTransport struct {
	mu      sync.Mutex
	metrics pmetric.Metrics
	has     bool
}

func (t *inMemoryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return nil, errors.New("empty OTLP request body")
	}
	defer req.Body.Close()

	var body io.Reader = req.Body
	if req.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(req.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		body = gz
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	er := pmetricotlp.NewExportRequest()
	if err := er.UnmarshalProto(raw); err != nil {
		return nil, fmt.Errorf("failed to decode OTLP metrics: %w", err)
	}

	t.mu.Lock()
	t.metrics = er.Metrics()
	t.has = true
	t.mu.Unlock()

	return &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Request:    req,
	}, nil
}

func (t *inMemoryTransport) take() pmetric.Metrics {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.has {
		return pmetric.NewMetrics()
	}
	m := t.metrics
	t.metrics = pmetric.Metrics{}
	t.has = false
	return m
}
