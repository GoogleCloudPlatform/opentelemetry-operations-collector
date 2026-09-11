// Copyright 2025 Google LLC
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

package promqle2etest

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/compliance/promqle2e"
)

// TestPromOtelGCM_PrometheusCounter_NoCT tests a basic counter sample behaviour
// with a known CT limitation across the following ingestion flows:
// * target --PromProto--> Prometheus (referencing, ideal, OSS behaviour).
// * target --PromProto--> Prometheus GMP fork --GCM API--> GCM.
// * target --PromProto--> OpenTelemetry Collector --GCM API--> GCM.
// * target --PromProto--> OpenTelemetry Collector (+MSTP) --GCM API--> GCM.
//
// The main goal is to have a basic acceptance test on the non-trivial behaviours across multiple ingestion pipelines.
// Currently, this test is for manual run only; to run add GCM_SECRET envvar containing GCM API read and write access (and adjust timeout).
//
// TODO(bwplotka): In future we could add more pipelines to test and compare e.g.
// * target --PromProto--> Prometheus vanilla --PRW 2.0--> OpenTelemetry Collector (+MSTP) --GCM API-->GCM.
// * target --PromProto--> Prometheus vanilla --PRW 2.0--> GCM.
// * target --PromProto--> OpenTelemetry Collector (+MSTP) --OTLP--> GCM.
func TestPromOtelGCM_PrometheusCounter_NoCT(t *testing.T) {
	const interval = 15 * time.Second

	// target --PromProto--> Prometheus.
	prom := promqle2e.PrometheusBackend{
		Name:  "prom",
		Image: "quay.io/prometheus/prometheus:v3.2.0",
	}

	// target --PromProto--> Prometheus GMP fork --GCM API--> GCM.
	promForkGCM := PrometheusForkGCMBackend{
		Name:  "prom-fork-gcm",
		Image: "gke.gcr.io/prometheus-engine/prometheus:v2.45.3-gmp.10-gke.0",
		GCMSA: GCMServiceAccountOrFail(t),
	}

	// target --PromProto?--> OpenTelemetry Collector --GCM API--> GCM.
	// For no-ct cases, this essentially uses GCM internal
	// logic for "cannibalization" algorithm.
	otelGCM := OtelGCMBackend{
		Name: "otel-gcm",
		// Current docs recommend otel/opentelemetry-collector-contrib:0.106.0
		// Should we use this repo instead?
		Image: "otel/opentelemetry-collector-contrib:0.123.0",
		GCMSA: GCMServiceAccountOrFail(t),
	}

	// target --PromProto--> OpenTelemetry Collector (+MSTP) --GCM API--> GCM.
	// Similar to `otel-gcm` but we use a new metricstarttimeprocessor (MSTP) processor
	// to adjust counter samples without CT.
	otelMSTPGCM := OtelGCMBackend{
		Name: "otel-mstp-gcm",
		// TODO(bwplotka): Replace with upstream once https://github.com/open-telemetry/opentelemetry-collector-contrib/pull/38594 is merged.
		Image: "us-east1-docker.pkg.dev/ridwanmsharif-dev/gboc/otelcol-google:0.122.1",
		ExtraProcessors: map[string]string{
			"metricstarttime": `
    strategy: subtract_initial_point`,
		},
		GCMSA: GCMServiceAccountOrFail(t),
	}

	pt := promqle2e.NewScrapeStyleTest(t)
	pt.SetCurrentTime(time.Now().Add(-10 * time.Minute)) // We only do a few scrapes, so -10m buffer is enough.

	//nolint:promlinter // Test metric.
	counter := promauto.With(pt.Registerer()).NewCounterVec(prometheus.CounterOpts{
		Name:        "promqle2e_test_counter_total",
		Help:        "Test counter used by promqle2e test framework for acceptance tests.",
		ConstLabels: map[string]string{"repo": "github.com/GoogleCloudPlatform/opentelemetry-operations-collector"},
	}, []string{"foo"})
	var c prometheus.Counter

	// No metric expected, counterVec empty.
	pt.RecordScrape(interval)

	c = counter.WithLabelValues("bar")
	c.Add(200)
	pt.RecordScrape(interval).
		Expect(c, 200, prom)
	// Nothing is expected for GCM due to cannibalization required if the target does not emit CT (which this metric does not).
	// See https://cloud.google.com/stackdriver/docs/managed-prometheus/troubleshooting#counter-sums
	// TODO(bwplotka): Fix with b/259261536.

	c.Add(10)
	pt.RecordScrape(interval).
		Expect(c, 210, prom).
		// NOTE(bwplotka): This and following discrepancies are also expected due to the cannibalization
		// algorithm mentioned above.
		Expect(c, 10, promForkGCM).
		Expect(c, 10, otelGCM).
		Expect(c, 10, otelMSTPGCM)

	c.Add(40)
	pt.RecordScrape(interval).
		Expect(c, 250, prom).
		Expect(c, 50, promForkGCM).
		Expect(c, 50, otelGCM).
		Expect(c, 50, otelMSTPGCM)

	// Reset to 0 (simulating instrumentation resetting metric or restarting target).
	counter.Reset()
	c = counter.WithLabelValues("bar")
	pt.RecordScrape(interval).
		Expect(c, 0, prom).
		// NOTE(bwplotka): This and following discrepancies are expected due to
		// GCM PromQL layer using MQL with delta alignment. What we get as a raw
		// counter is already reset-normalized (b/305901765) (plus cannibalization).
		Expect(c, 50, promForkGCM).
		Expect(c, 50, otelGCM)
	// NOTE(bwplotka): Here otelMSTPGCM misses the samples, which is not too bad,
	// but I propose to fix it upstream (https://github.com/open-telemetry/opentelemetry-collector-contrib/pull/38594#discussion_r2044120953).

	c.Add(150)
	pt.RecordScrape(interval).
		Expect(c, 150, prom).
		Expect(c, 200, promForkGCM).
		// NOTE(bwplotka): This is where Otel->GCM behaviour goes even more off vs
		// Prometheus and Prometheus fork. The reason is the "broken" true reset algorithm
		// in the prometheusreceiver, something we try to fix in MSTP
		// (https://github.com/open-telemetry/opentelemetry-collector-contrib/pull/38594).
		// TODO(bwplotka): Change GCP docs to recommended MSTP configuration once released (b/389130459).
		Expect(c, 0, otelGCM).
		// New MSTP flow is fine.
		Expect(c, 200, otelMSTPGCM)

	// Reset to 0 with addition.
	counter.Reset()
	c = counter.WithLabelValues("bar")
	c.Add(20)
	pt.RecordScrape(interval).
		Expect(c, 20, prom).
		Expect(c, 220, promForkGCM).
		Expect(c, 20, otelGCM). // Broken (b/389130459).
		Expect(c, 220, otelMSTPGCM)

	c.Add(50)
	pt.RecordScrape(interval).
		Expect(c, 70, prom).
		Expect(c, 270, promForkGCM).
		Expect(c, -130, otelGCM). // Broken (b/389130459).
		Expect(c, 270, otelMSTPGCM)

	c.Add(10)
	pt.RecordScrape(interval).
		Expect(c, 80, prom).
		Expect(c, 280, promForkGCM).
		Expect(c, -120, otelGCM). // Broken (b/389130459).
		Expect(c, 280, otelMSTPGCM)

	// Tricky reset case, unnoticeable reset for Prometheus without created timestamp as well.
	counter.Reset()
	c = counter.WithLabelValues("bar")
	c.Add(600)
	pt.RecordScrape(interval).
		Expect(c, 600, prom).
		Expect(c, 800, promForkGCM).
		Expect(c, 400, otelGCM). // Broken (b/389130459).
		Expect(c, 800, otelMSTPGCM)

	// Prometheus SDK used for replies actually emit CTs.
	// Remove all CTs explicitly to test the logic for non-provided CTs in the Prometheus ecosystem.
	pt.Transform(func(recordings [][]*dto.MetricFamily) [][]*dto.MetricFamily {
		for i := range recordings {
			for j := range recordings[i] {
				for k := range recordings[i][j].GetMetric() {
					if recordings[i][j].Metric[k].GetCounter() == nil {
						t.Fatalf("all recorded metrics should be counters")
					}
					recordings[i][j].Metric[k].Counter.CreatedTimestamp = nil
				}
			}
		}
		return recordings
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)
	pt.Run(ctx)
}
