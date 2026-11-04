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
	"sync"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// The in-process reader is the handoff point between the Collector's internal
// telemetry MeterProvider (created by this factory when the service starts)
// and consumers such as the selftelemetry receiver (created later, as part of
// the pipeline graph). The handoff is a plain in-memory pointer: no listener,
// socket, port, pipe or file is involved.
var (
	readerMu sync.RWMutex
	reader   *sdkmetric.ManualReader
)

// Reader returns the ManualReader attached to the Collector's current internal
// telemetry MeterProvider, or nil if internal metrics are disabled
// (service::telemetry::metrics::level: none) or the binary was not built with
// this telemetry factory.
//
// Callers should look the reader up on every collection rather than caching
// it: the Collector re-creates its telemetry providers whenever the service is
// rebuilt (e.g. on config reload).
func Reader() *sdkmetric.ManualReader {
	readerMu.RLock()
	defer readerMu.RUnlock()
	return reader
}

// newReader creates a new ManualReader and publishes it as the current
// in-process reader. The most recently created reader always wins.
func newReader() *sdkmetric.ManualReader {
	r := sdkmetric.NewManualReader()
	readerMu.Lock()
	reader = r
	readerMu.Unlock()
	return r
}

// releaseReader unpublishes r if it is still the current reader. It is called
// when the MeterProvider that owns r is shut down.
func releaseReader(r *sdkmetric.ManualReader) {
	readerMu.Lock()
	defer readerMu.Unlock()
	if reader == r {
		reader = nil
	}
}
