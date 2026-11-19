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

package selftelemetryreceiver_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/pdata/plog"
)

// componentLog is one internal log record together with its instrumentation
// scope and resource attributes.
type componentLog struct {
	body     string
	scope    map[string]any
	resource map[string]any
}

func flattenLogs(batches []plog.Logs) []componentLog {
	var out []componentLog
	for _, ld := range batches {
		for _, rl := range ld.ResourceLogs().All() {
			res := rl.Resource().Attributes().AsRaw()
			for _, sl := range rl.ScopeLogs().All() {
				scope := sl.Scope().Attributes().AsRaw()
				for _, lr := range sl.LogRecords().All() {
					out = append(out, componentLog{body: lr.Body().AsString(), scope: scope, resource: res})
				}
			}
		}
	}
	return out
}

// TestEndToEndLogsNoHostEndpoint runs a full Collector built with the
// inprocesstelemetry factory, with no internal-telemetry endpoint at all (no
// metric reader, no log processor), and checks that the Collector's own logs
// reach a pipeline through the selftelemetry receiver, with the per-component
// attributes as instrumentation scope attributes (as upstream's OTLP log
// export produces them) and the Collector's resource.
func TestEndToEndLogsNoHostEndpoint(t *testing.T) {
	enableInProcessReader(t)
	sink := &sinkExporter{}
	startCollector(t, `
receivers:
  selftelemetry:
    collection_interval: 200ms
    initial_delay: 0s
exporters:
  sink: {}
service:
  telemetry:
    logs:
      level: info
      processors: []
    metrics:
      level: normal
      readers: []
  pipelines:
    logs:
      receivers: [selftelemetry]
      exporters: [sink]
`, sink)

	// Development-stability components log a notice with their component
	// logger at startup; it must arrive with the component's scope attributes.
	var found componentLog
	require.Eventually(t, func() bool {
		for _, l := range flattenLogs(sink.allLogs()) {
			if l.scope["otelcol.component.id"] == "selftelemetry" && l.scope["otelcol.component.kind"] == "receiver" {
				found = l
				return true
			}
		}
		return false
	}, 30*time.Second, 100*time.Millisecond, "component logs did not flow in-process with scope attributes")

	assert.NotEmpty(t, found.body)
	assert.NotContains(t, found.scope, "otelcol.component.id_missing")
	assert.Contains(t, found.resource, "service.instance.id")
	assert.Contains(t, found.resource, "service.name")
}
