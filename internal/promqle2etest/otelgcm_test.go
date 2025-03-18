package promqle2etest

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/compliance/promqle2e"
)

func TestPrometheusCounter_OtelGCM_e2e(t *testing.T) {
	const interval = 30 * time.Second

	// Prometheus binary for the test clarity, allowing to reference to the upstream.
	prom := promqle2e.PrometheusBackend{
		Name:  "prom",
		Image: "quay.io/prometheus/prometheus:v3.2.0",
	}
	promForkGCM := PrometheusForkGCMBackend{Name: "prom-fork-gcm", Image: "gke.gcr.io/prometheus-engine/prometheus:v2.45.3-gmp.10-gke.0", GCMSA: GCMServiceAccountOrFail(t)}
	//otelGCM := NewPrometheusForkGCMBackend(t, "otel-gcm", "", GCMServiceAccountOrFail(t))

	pt := promqle2e.NewScrapeStyleTest(t)

	//nolint:promlinter // Test metric.
	counter := promauto.With(pt.Registerer()).NewCounterVec(prometheus.CounterOpts{
		Name: "promqle2e_test_counter_total",
		Help: "Test counter used by promqle2e test framework for acceptance tests.",
	}, []string{"foo"})
	var c prometheus.Counter

	// No metric expected, counterVec empty.
	pt.RecordScrape(interval)

	c = counter.WithLabelValues("bar")
	c.Add(200)
	pt.RecordScrape(interval).
		Expect(c, 200, prom1).
		Expect(c, 200, prom2)

	/*
		// No metric.
				scrape(interval)

				c = counter.WithLabelValues("bar")
				c.Add(200)

				scrape(interval).
					Expect(200, c, prom)
				// Nothing is expected for GMP due to cannibalization.
				// See https://cloud.google.com/stackdriver/docs/managed-prometheus/troubleshooting#counter-sums
				// TODO(bwplotka): Fix with b/259261536.

				c.Add(10)
				scrape(interval).
					Expect(10, c, export).
					Expect(210, c, prom)

				c.Add(40)
				scrape(interval).
					Expect(50, c, export).
					Expect(250, c, prom)

				// Reset to 0 (simulating instrumentation resetting metric or restarting target).
				counter.Reset()
				c = counter.WithLabelValues("bar")
				scrape(interval).
					// NOTE(bwplotka): This and following discrepancies are expected due to
					// GCM PromQL layer using MQL with delta alignment. What we get as a raw
					// counter is already reset-normalized (b/305901765) (plus cannibalization).
					Expect(50, c, export).
					Expect(0, c, prom)

				c.Add(150)
				scrape(interval).
					Expect(200, c, export).
					Expect(150, c, prom)

				// Reset to 0 with addition.
				counter.Reset()
				c = counter.WithLabelValues("bar")
				c.Add(20)
				scrape(interval).
					Expect(220, c, export).
					Expect(20, c, prom)

				c.Add(50)
				scrape(interval).
					Expect(270, c, export).
					Expect(70, c, prom)

				c.Add(10)
				scrape(interval).
					Expect(280, c, export).
					Expect(80, c, prom)

				// Tricky reset case, unnoticeable reset for Prometheus without created timestamp as well.
				counter.Reset()
				c = counter.WithLabelValues("bar")
				c.Add(600)
				scrape(interval).
					Expect(800, c, export).
					Expect(600, c, prom)
	*/

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	pt.Run(ctx)
}
