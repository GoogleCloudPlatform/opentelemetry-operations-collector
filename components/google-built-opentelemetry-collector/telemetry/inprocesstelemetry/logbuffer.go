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
	"context"
	"sync"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// DefaultLogBufferCapacity is the maximum number of internal log records the
// in-process log buffer holds between two drains. When it is full, the oldest
// record is dropped to make room for the newest one, so a consumer that stops
// draining (or was never configured) costs a bounded amount of memory.
const DefaultLogBufferCapacity = 8192

// LogBuffer is an OpenTelemetry SDK log processor that keeps the Collector's
// internal log records in memory until a consumer, such as the selftelemetry
// receiver, drains them. It is the logs counterpart of the in-process metric
// reader: no listener, socket, port, pipe or file is involved.
//
// It is safe for concurrent use.
type LogBuffer struct {
	mu       sync.Mutex
	capacity int
	// records is a ring buffer of up to capacity records. start is the index
	// of the oldest record and n is the number of buffered records.
	records []sdklog.Record
	start   int
	n       int
	dropped uint64
	closed  bool
}

var _ sdklog.Processor = (*LogBuffer)(nil)

// NewLogBuffer returns an empty LogBuffer that holds at most capacity
// records. A capacity < 1 means DefaultLogBufferCapacity.
func NewLogBuffer(capacity int) *LogBuffer {
	if capacity < 1 {
		capacity = DefaultLogBufferCapacity
	}
	return &LogBuffer{
		capacity: capacity,
		records:  make([]sdklog.Record, capacity),
	}
}

// Enabled reports whether the buffer accepts records. It accepts every record
// until it is shut down; level filtering and sampling have already been
// applied by the Collector's logger before records reach the processor.
func (b *LogBuffer) Enabled(context.Context, sdklog.EnabledParameters) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.closed
}

// OnEmit stores a copy of record, dropping the oldest buffered record if the
// buffer is full. It never blocks on a consumer.
func (b *LogBuffer) OnEmit(_ context.Context, record *sdklog.Record) error {
	if record == nil {
		return nil
	}
	// The SDK reuses records after OnEmit returns; keep an independent copy.
	clone := record.Clone()

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	if b.n == b.capacity {
		b.records[b.start] = clone
		b.start = (b.start + 1) % b.capacity
		b.dropped++
		return nil
	}
	b.records[(b.start+b.n)%b.capacity] = clone
	b.n++
	return nil
}

// Drain returns the buffered records, oldest first, and the number of records
// dropped because the buffer was full since the previous Drain. It empties the
// buffer and resets the drop count.
func (b *LogBuffer) Drain() (records []sdklog.Record, dropped uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	records = make([]sdklog.Record, b.n)
	for i := range b.n {
		idx := (b.start + i) % b.capacity
		records[i] = b.records[idx]
		b.records[idx] = sdklog.Record{}
	}
	dropped = b.dropped
	b.start, b.n, b.dropped = 0, 0, 0
	return records, dropped
}

// Len returns the number of buffered records.
func (b *LogBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.n
}

// ForceFlush is a no-op: records are only ever handed over by Drain.
func (b *LogBuffer) ForceFlush(context.Context) error {
	return nil
}

// Shutdown stops the buffer from accepting new records. Records that are
// already buffered can still be drained, so a consumer's final collection on
// shutdown does not lose them.
func (b *LogBuffer) Shutdown(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

// The in-process log buffer is handed from the telemetry factory (which
// creates it with the Collector's logger) to consumers such as the
// selftelemetry receiver (created later, as part of the pipeline graph), the
// same way the in-process metric reader is.
var (
	logBufferMu sync.RWMutex
	logBuffer   *LogBuffer
)

// Logs returns the LogBuffer attached to the Collector's current internal
// logger, or nil if the telemetry.inProcessReader feature gate is disabled or
// the binary was not built with this telemetry factory.
//
// Callers should look the buffer up on every collection rather than caching
// it: the Collector re-creates its logger whenever the service is rebuilt
// (e.g. on config reload).
func Logs() *LogBuffer {
	logBufferMu.RLock()
	defer logBufferMu.RUnlock()
	return logBuffer
}

// publishLogBuffer publishes b as the current in-process log buffer. The most
// recently published buffer always wins.
func publishLogBuffer(b *LogBuffer) {
	logBufferMu.Lock()
	logBuffer = b
	logBufferMu.Unlock()
}

// releaseLogBuffer unpublishes b if it is still the current buffer.
func releaseLogBuffer(b *LogBuffer) {
	logBufferMu.Lock()
	defer logBufferMu.Unlock()
	if logBuffer == b {
		logBuffer = nil
	}
}
