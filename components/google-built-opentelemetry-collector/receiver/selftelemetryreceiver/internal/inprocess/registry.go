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

// Package inprocess holds the process-global handoff point between the
// Collector's internal telemetry MeterProvider (created by the telemetry
// factory) and the selftelemetry receiver (created later, as part of the
// pipeline graph).
//
// The handoff is a plain in-memory pointer: no listener, socket, port, pipe
// or file is involved.
package inprocess // import "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/receiver/selftelemetryreceiver/internal/inprocess"

import (
	"sync"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var (
	mu     sync.RWMutex
	reader *sdkmetric.ManualReader
)

// NewReader creates a new ManualReader, publishes it as the current
// in-process reader and returns it so it can be registered with a
// MeterProvider.
//
// The Collector re-creates its telemetry providers whenever the service is
// rebuilt (e.g. on config reload), so the most recently created reader always
// wins.
func NewReader() *sdkmetric.ManualReader {
	r := sdkmetric.NewManualReader()
	mu.Lock()
	reader = r
	mu.Unlock()
	return r
}

// Reader returns the current in-process reader, or nil if internal telemetry
// metrics are disabled or the in-process telemetry factory is not in use.
func Reader() *sdkmetric.ManualReader {
	mu.RLock()
	defer mu.RUnlock()
	return reader
}

// Release unpublishes r if it is still the current reader. It is called when
// the MeterProvider that owns r is shut down.
func Release(r *sdkmetric.ManualReader) {
	mu.Lock()
	defer mu.Unlock()
	if reader == r {
		reader = nil
	}
}
