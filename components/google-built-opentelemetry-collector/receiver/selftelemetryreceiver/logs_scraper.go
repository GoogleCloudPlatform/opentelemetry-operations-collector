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
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/telemetry/inprocesstelemetry"
)

// selfLogsScraper drains the Collector's internal log records from the
// in-process log buffer and converts them to pdata.
//
// Like selfScraper for metrics, the conversion from the OTel Go SDK's log
// records to OTLP is done by the SDK's own OTLP/HTTP exporter, whose HTTP
// client is given an in-memory http.RoundTripper. The "request" never leaves
// the process.
type selfLogsScraper struct {
	logger *zap.Logger

	// bufferFn returns the buffer to drain; overridable in tests.
	bufferFn func() *inprocesstelemetry.LogBuffer

	mu        sync.Mutex
	transport *inMemoryLogsTransport
	exporter  sdklog.Exporter
	warnOnce  sync.Once
}

// defaultLogBufferFn is the buffer lookup used by new scrapers; overridable in
// tests.
var defaultLogBufferFn = inprocesstelemetry.Logs

func newSelfLogsScraper(logger *zap.Logger) *selfLogsScraper {
	return &selfLogsScraper{
		logger:    logger,
		bufferFn:  defaultLogBufferFn,
		transport: &inMemoryLogsTransport{},
	}
}

// started reports whether start has been called and shutdown has not.
func (s *selfLogsScraper) started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exporter != nil
}

func (s *selfLogsScraper) start(ctx context.Context, _ component.Host) error {
	exp, err := otlploghttp.New(ctx,
		// The endpoint is never dialed; the HTTP client's transport is in-memory.
		otlploghttp.WithEndpoint("selftelemetry.invalid"),
		otlploghttp.WithInsecure(),
		otlploghttp.WithCompression(otlploghttp.NoCompression),
		otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: false}),
		otlploghttp.WithTimeout(time.Minute),
		otlploghttp.WithHTTPClient(&http.Client{Transport: s.transport}),
	)
	if err != nil {
		return fmt.Errorf("failed to create in-memory OTLP log converter: %w", err)
	}
	s.mu.Lock()
	s.exporter = exp
	s.mu.Unlock()
	return nil
}

func (s *selfLogsScraper) shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exporter == nil {
		return nil
	}
	err := s.exporter.Shutdown(ctx)
	s.exporter = nil
	return err
}

func (s *selfLogsScraper) scrape(ctx context.Context) (plog.Logs, error) {
	buf := s.bufferFn()
	if buf == nil {
		s.warnOnce.Do(func() {
			s.logger.Warn("No in-process log buffer is available; the telemetry.inProcessReader feature gate is not enabled " +
				"(--feature-gates=+telemetry.inProcessReader), " +
				"or this binary was not built with the inprocesstelemetry factory")
		})
		return plog.NewLogs(), nil
	}

	records, dropped := buf.Drain()
	if dropped > 0 {
		// This warning is itself an internal log record, and is delivered by
		// the next collection.
		s.logger.Warn("Internal log records were dropped because the in-process log buffer was full; "+
			"consider lowering collection_interval",
			zap.Uint64("dropped_records", dropped))
	}
	if len(records) == 0 {
		return plog.NewLogs(), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exporter == nil {
		return plog.NewLogs(), errors.New("selftelemetry receiver is not started")
	}
	if err := s.exporter.Export(ctx, records); err != nil {
		return plog.NewLogs(), fmt.Errorf("failed to convert internal logs: %w", err)
	}
	return s.transport.take(), nil
}

// inMemoryLogsTransport is an http.RoundTripper that accepts OTLP/HTTP logs
// export requests and accumulates their decoded payload instead of sending it
// anywhere.
type inMemoryLogsTransport struct {
	mu   sync.Mutex
	logs plog.Logs
	has  bool
}

func (t *inMemoryLogsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
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

	er := plogotlp.NewExportRequest()
	if err := er.UnmarshalProto(raw); err != nil {
		return nil, fmt.Errorf("failed to decode OTLP logs: %w", err)
	}

	t.mu.Lock()
	if !t.has {
		t.logs = plog.NewLogs()
		t.has = true
	}
	// The exporter may split one Export into several requests; keep them all.
	er.Logs().ResourceLogs().MoveAndAppendTo(t.logs.ResourceLogs())
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

func (t *inMemoryLogsTransport) take() plog.Logs {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.has {
		return plog.NewLogs()
	}
	l := t.logs
	t.logs = plog.Logs{}
	t.has = false
	return l
}
