package promqle2etest

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/compliance/promqle2e"
)

// TestPromOtelGCM_PrometheusCounter_NoCT tests a basic counter sample behaviour
// with a known CT limitation across 3 ingestion flows:
// * target --PromProto--> Prometheus (referencing, ideal, OSS behaviour).
// * target --PromProto--> Prometheus GMP fork --GCM API--> GCM.
// * target --PromProto--> OpenTelemetry Collector (Google Operations build) --GCM API--> GCM.
//
// The main goal is to have a basic acceptance test on the non-trivial behaviours across multiple ingestion pipelines.
//
// TODO(bwplotka): In future we could add more pipelines to test and compare e.g.
// * target --PromProto--> Prometheus vanilla --PRW 2.0--> OpenTelemetry Collector --GCM API-->GCM.
// * target --PromProto--> Prometheus vanilla --PRW 2.0--> GCM.
// * target --PromProto--> OpenTelemetry Collector --OTLP--> GCM.
func TestPromOtelGCM_PrometheusCounter_NoCT(t *testing.T) {
	// t.Skip("For manual run only for now; to run comment this out and run with GCM_SECRET envvar containing GCM API read and write access.")

	const interval = 30 * time.Second

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
	otelGCM := OtelGCMBackend{
		Name: "otel-gcm",
		// Current docs recommend otel/opentelemetry-collector-contrib:0.106.0
		// Should we use this repo instead?
		Image: "otel/opentelemetry-collector-contrib:0.123.0",
		GCMSA: GCMServiceAccountOrFail(t),
	}

	pt := promqle2e.NewScrapeStyleTest(t)

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
		Expect(c, 10, otelGCM).
		Expect(c, 10, promForkGCM).
		Expect(c, 210, prom)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)
	pt.Run(ctx)
}
