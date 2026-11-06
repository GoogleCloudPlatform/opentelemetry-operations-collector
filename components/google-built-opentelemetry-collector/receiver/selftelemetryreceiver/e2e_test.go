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
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/receiver/selftelemetryreceiver"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/telemetry/inprocesstelemetry"
)

// TestEndToEndMatchesPrometheusEndpoint runs a full Collector built with the
// inprocesstelemetry factory, with:
//   - the classic Prometheus pull endpoint for internal metrics (the "before"
//     path that e.g. the Ops Agent scrapes today), and
//   - the selftelemetry receiver feeding a pipeline (the "after" path).
//
// It then checks that the two paths expose the same metrics, with the same
// attribute sets, and consistent values.
func TestEndToEndMatchesPrometheusEndpoint(t *testing.T) {
	port := freePort(t)
	sink := &sinkExporter{}

	cfgYAML := fmt.Sprintf(`
receivers:
  selftelemetry:
    collection_interval: 500ms
    initial_delay: 0s
exporters:
  sink: {}
service:
  telemetry:
    logs:
      level: warn
    metrics:
      level: detailed
      readers:
        - pull:
            exporter:
              prometheus:
                host: localhost
                port: %d
                without_scope_info: true
                without_units: true
                without_type_suffix: true
  pipelines:
    metrics:
      receivers: [selftelemetry]
      exporters: [sink]
`, port)

	startCollector(t, cfgYAML, sink)

	// Wait until the self pipeline has run a few times so that the receiver's
	// and exporter's own metrics have been created.
	require.Eventually(t, func() bool {
		return sink.batches() >= 4
	}, 30*time.Second, 100*time.Millisecond, "selftelemetry receiver never delivered metrics")

	// Compare a Prometheus scrape (taken first) with the next in-process batch
	// (taken after). A new series may legitimately appear between the two
	// snapshots, so retry a few times before declaring a mismatch.
	var lastDiff string
	for attempt := 0; attempt < 10; attempt++ {
		prom := scrapePrometheus(t, port)
		after := sink.batches()
		require.Eventually(t, func() bool { return sink.batches() > after }, 10*time.Second, 50*time.Millisecond)
		otlp := flatten(sink.latest())

		lastDiff = compare(prom, otlp)
		if lastDiff == "" {
			t.Logf("Prometheus endpoint and selftelemetry receiver agree on %d series across %d metrics",
				len(prom), countMetrics(prom))
			assertResource(t, sink.latest())
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Prometheus endpoint and selftelemetry receiver disagree:\n%s", lastDiff)
}

// TestEndToEndNoHostEndpoint checks that internal metrics still flow when the
// Collector exposes no internal-telemetry endpoint at all
// (`readers: []`), which upstream otelconftelemetry rejects.
func TestEndToEndNoHostEndpoint(t *testing.T) {
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
      level: warn
    metrics:
      level: normal
      readers: []
  pipelines:
    metrics:
      receivers: [selftelemetry]
      exporters: [sink]
`, sink)

	require.Eventually(t, func() bool {
		if sink.batches() < 2 {
			return false
		}
		got := flatten(sink.latest())
		for k := range got {
			if strings.HasPrefix(k, "otelcol_receiver_accepted_metric_points{") {
				return true
			}
		}
		return false
	}, 30*time.Second, 100*time.Millisecond, "internal metrics did not flow without a reader endpoint")
}

func startCollector(t *testing.T, cfgYAML string, sink *sinkExporter) {
	t.Helper()
	col, err := otelcol.NewCollector(otelcol.CollectorSettings{
		BuildInfo: component.NewDefaultBuildInfo(),
		Factories: func() (otelcol.Factories, error) {
			recv, err := otelcol.MakeFactoryMap[receiver.Factory](selftelemetryreceiver.NewFactory())
			if err != nil {
				return otelcol.Factories{}, err
			}
			exp, err := otelcol.MakeFactoryMap[exporter.Factory](sink.factory())
			if err != nil {
				return otelcol.Factories{}, err
			}
			return otelcol.Factories{
				Telemetry: inprocesstelemetry.NewFactory(),
				Receivers: recv,
				Exporters: exp,
			}, nil
		},
		ConfigProviderSettings: otelcol.ConfigProviderSettings{
			ResolverSettings: confmap.ResolverSettings{
				URIs:              []string{"yaml:" + cfgYAML},
				ProviderFactories: []confmap.ProviderFactory{yamlprovider.NewFactory()},
			},
		},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- col.Run(ctx) }()
	t.Cleanup(func() {
		col.Shutdown()
		cancel()
		require.NoError(t, <-runErr)
	})
}

func assertResource(t *testing.T, md pmetric.Metrics) {
	t.Helper()
	require.Positive(t, md.ResourceMetrics().Len())
	attrs := md.ResourceMetrics().At(0).Resource().Attributes()
	for _, k := range []string{"service.name", "service.instance.id", "service.version"} {
		_, ok := attrs.Get(k)
		require.Truef(t, ok, "resource attribute %q missing; got %v", k, attrs.AsRaw())
	}
}

// series is a flattened view of one time series: metric name + sorted labels.
type series struct {
	value float64 // counter/gauge value, or histogram count
	kind  string  // "counter", "gauge", "histogram"
}

var invalidLabelChars = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func seriesKey(name string, labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%s=%q", k, labels[k])
	}
	b.WriteString("}")
	return b.String()
}

func metricOf(key string) string { return key[:strings.Index(key, "{")] }

func countMetrics(m map[string]series) int {
	names := map[string]bool{}
	for k := range m {
		names[metricOf(k)] = true
	}
	return len(names)
}

func scrapePrometheus(t *testing.T, port int) map[string]series {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(resp.Body)
	require.NoError(t, err)

	out := map[string]series{}
	for name, mf := range families {
		if name == "target_info" {
			continue // Resource; compared separately.
		}
		if strings.HasPrefix(name, "promhttp_") {
			// Emitted by the Prometheus HTTP handler itself (not an OTel
			// instrument), so it only exists on the pull endpoint.
			continue
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			var s series
			switch mf.GetType() {
			case dto.MetricType_COUNTER:
				s = series{value: m.GetCounter().GetValue(), kind: "counter"}
			case dto.MetricType_GAUGE:
				s = series{value: m.GetGauge().GetValue(), kind: "gauge"}
			case dto.MetricType_HISTOGRAM:
				s = series{value: float64(m.GetHistogram().GetSampleCount()), kind: "histogram"}
			default:
				t.Fatalf("unexpected Prometheus type %v for %s", mf.GetType(), name)
			}
			out[seriesKey(strings.TrimSuffix(name, "_total"), labels)] = s
		}
	}
	return out
}

func flatten(md pmetric.Metrics) map[string]series {
	out := map[string]series{}
	labelsOf := func(attrs pcommon.Map) map[string]string {
		l := map[string]string{}
		attrs.Range(func(k string, v pcommon.Value) bool {
			l[invalidLabelChars.ReplaceAllString(k, "_")] = v.AsString()
			return true
		})
		return l
	}
	numVal := func(dp pmetric.NumberDataPoint) float64 {
		if dp.ValueType() == pmetric.NumberDataPointValueTypeInt {
			return float64(dp.IntValue())
		}
		return dp.DoubleValue()
	}
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		sms := md.ResourceMetrics().At(i).ScopeMetrics()
		for j := 0; j < sms.Len(); j++ {
			ms := sms.At(j).Metrics()
			for k := 0; k < ms.Len(); k++ {
				m := ms.At(k)
				name := strings.TrimSuffix(invalidLabelChars.ReplaceAllString(m.Name(), "_"), "_total")
				switch m.Type() {
				case pmetric.MetricTypeSum:
					kind := "gauge"
					if m.Sum().IsMonotonic() {
						kind = "counter"
					}
					for d := 0; d < m.Sum().DataPoints().Len(); d++ {
						dp := m.Sum().DataPoints().At(d)
						out[seriesKey(name, labelsOf(dp.Attributes()))] = series{value: numVal(dp), kind: kind}
					}
				case pmetric.MetricTypeGauge:
					for d := 0; d < m.Gauge().DataPoints().Len(); d++ {
						dp := m.Gauge().DataPoints().At(d)
						out[seriesKey(name, labelsOf(dp.Attributes()))] = series{value: numVal(dp), kind: "gauge"}
					}
				case pmetric.MetricTypeHistogram:
					for d := 0; d < m.Histogram().DataPoints().Len(); d++ {
						dp := m.Histogram().DataPoints().At(d)
						out[seriesKey(name, labelsOf(dp.Attributes()))] = series{value: float64(dp.Count()), kind: "histogram"}
					}
				}
			}
		}
	}
	return out
}

// compare returns a human-readable description of the differences between a
// Prometheus snapshot taken at t0 and an in-process snapshot taken at t1 > t0,
// or "" if they are consistent.
func compare(prom, otlp map[string]series) string {
	var diffs []string
	for k, p := range prom {
		o, ok := otlp[k]
		if !ok {
			diffs = append(diffs, "only in Prometheus: "+k)
			continue
		}
		if o.kind != p.kind {
			diffs = append(diffs, fmt.Sprintf("kind mismatch for %s: prom=%s otlp=%s", k, p.kind, o.kind))
			continue
		}
		// Cumulative series can only grow between t0 and t1.
		if (p.kind == "counter" || p.kind == "histogram") && o.value < p.value {
			diffs = append(diffs, fmt.Sprintf("value went backwards for %s: prom=%v otlp=%v", k, p.value, o.value))
		}
	}
	for k := range otlp {
		if _, ok := prom[k]; !ok {
			diffs = append(diffs, "only in selftelemetry: "+k)
		}
	}
	sort.Strings(diffs)
	return strings.Join(diffs, "\n")
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// sinkExporter is a minimal metrics exporter that records every batch.
type sinkExporter struct {
	mu   sync.Mutex
	got  []pmetric.Metrics
	once sync.Once
	f    exporter.Factory
}

func (s *sinkExporter) factory() exporter.Factory {
	s.once.Do(func() {
		s.f = exporter.NewFactory(
			component.MustNewType("sink"),
			func() component.Config { return &struct{}{} },
			exporter.WithMetrics(func(context.Context, exporter.Settings, component.Config) (exporter.Metrics, error) {
				return s, nil
			}, component.StabilityLevelDevelopment),
		)
	})
	return s.f
}

func (*sinkExporter) Start(context.Context, component.Host) error { return nil }
func (*sinkExporter) Shutdown(context.Context) error              { return nil }
func (*sinkExporter) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (s *sinkExporter) ConsumeMetrics(_ context.Context, md pmetric.Metrics) error {
	cp := pmetric.NewMetrics()
	md.CopyTo(cp)
	s.mu.Lock()
	s.got = append(s.got, cp)
	s.mu.Unlock()
	return nil
}

func (s *sinkExporter) batches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

func (s *sinkExporter) latest() pmetric.Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.got[len(s.got)-1]
}
