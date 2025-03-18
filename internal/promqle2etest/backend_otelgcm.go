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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	gcm "cloud.google.com/go/monitoring/apiv3/v2"
	"github.com/efficientgo/e2e"
	e2emon "github.com/efficientgo/e2e/monitoring"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/compliance/promqle2e"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var _ promqle2e.Backend = OtelGCMBackend{}

// OtelGCMBackend represents an OpenTelemetry Collector scraping
// metrics and pushing to GCM API for consumption.
// This follows https://cloud.google.com/stackdriver/docs/managed-prometheus/setup-otel.
type OtelGCMBackend struct {
	Image string
	Name  string
	GCMSA []byte
}

func (o OtelGCMBackend) Ref() string {
	return o.Name
}

// newOtelCollector creates a new OpenTelemetry Collector runnable.
func newOtelCollector(env e2e.Environment, name string, image string, scrapeTargetAddress string, cluster, location, project string, gcmSA []byte) *e2emon.InstrumentedRunnable {
	ports := map[string]int{"http": 9090}

	f := env.Runnable(name).WithPorts(ports).Future()

	// NOTE(bwplotka): Starting from https://cloud.google.com/stackdriver/docs/managed-prometheus/setup-otel#run-off-gcp
	// but with extra things to make this actually work (TODO: update the docs)
	// * report_extra_scrape_metrics for compatibility.
	// * Telemetry in Prometheus format.
	// * GCM exporter: extra_metrics_config: enable_target_info: false and enable_scope_info: false
	// * Also: feature gate for native histograms in flags
	config := fmt.Sprintf(`
receivers:
  prometheus:
    report_extra_scrape_metrics: true
    config:
      scrape_configs:
      - job_name: 'test'
        scrape_interval: 5s
        scrape_timeout: 5s
        static_configs:
        - targets: [%s]
        metric_relabel_configs:
        - regex: instance
          action: labeldrop # TODO(bwplotka): This does not really work, we still see instance label.
        - target_label: 'collector'
          replacement: '%s'

processors:
  resource:
    attributes:
    - key: "cluster"
      value: "%s"
      action: upsert
    - key: "location"
      value: "%s"
      action: upsert

  transform:
    # "location", "cluster", "namespace", "job", "instance", and "project_id" are reserved, and
    # metrics containing these labels will be rejected. Prefix them with exported_ to prevent this.
    # TODO(bwplotka): Update this and docs. Below gives warning 13:02:20 otel-gcm: 2025-04-14T12:02:20.243Z	info	ottl@v0.123.0/parser_collection.go:447	one or more paths were modified to include their context prefix, please rewrite them accordingly.
    metric_statements:
    - context: datapoint
      statements:
      - set(attributes["exported_location"], attributes["location"])
      - delete_key(attributes, "location")
      - set(attributes["exported_cluster"], attributes["cluster"])
      - delete_key(attributes, "cluster")
      - set(attributes["exported_namespace"], attributes["namespace"])
      - delete_key(attributes, "namespace")
      - set(attributes["exported_job"], attributes["job"])
      - delete_key(attributes, "job")
      - set(attributes["exported_instance"], attributes["instance"])
      - delete_key(attributes, "instance")
      - set(attributes["exported_project_id"], attributes["project_id"])
      - delete_key(attributes, "project_id")

  batch:
    # batch metrics before sending to reduce API usage
    send_batch_max_size: 200
    send_batch_size: 200
    timeout: 5s

  memory_limiter:
    # drop metrics if memory usage gets too high
    check_interval: 1s
    limit_percentage: 65
    spike_limit_percentage: 20

# Note that the googlemanagedprometheus exporter block is intentionally blank
exporters:
  googlemanagedprometheus:
    project: "%s"
    # TODO(bwplotka): Do we need to disable cumulative_normalization explicitly?
    metric:
      # TODO(bwplotka): Update docs? Change defaults? Those metrics are a bad idea to include (IMO).
      # We are working in Prometheus OSS to slowly promote more resource attr as labels (if not all one day).
      extra_metrics_config:
        enable_target_info: false
        enable_scope_info: false

service:
  pipelines:
    metrics:
      receivers: [prometheus]
      processors: [batch, memory_limiter, resource, transform]
      exporters: [googlemanagedprometheus]
  telemetry:
    # Internal collector metrics for debugging.
    # https://opentelemetry.io/docs/collector/internal-telemetry/#configure-internal-metrics
    metrics:
      readers:
        - pull:
            exporter:
              prometheus:
                host: '0.0.0.0'
                port: 9090
`, scrapeTargetAddress, name, cluster, location, project)
	if err := os.WriteFile(filepath.Join(f.Dir(), "config.yml"), []byte(config), 0600); err != nil {
		return e2emon.AsInstrumented(e2e.NewFailedRunnable(name, fmt.Errorf("create otel config failed: %w", err)), "http")
	}

	if err := os.WriteFile(filepath.Join(f.Dir(), "gcm-sa.json"), gcmSA, 0600); err != nil {
		return e2emon.AsInstrumented(e2e.NewFailedRunnable(name, fmt.Errorf("write gcm SA failed: %w", err)), "http")
	}

	args := map[string]string{
		"--config": filepath.Join(f.Dir(), "config.yml"),
		"--feature-gates=receiver.prometheusreceiver.EnableNativeHistograms": "",
		"--feature-gates=exporter.googlemanagedprometheus.intToDouble":       "",
	}

	return e2emon.AsInstrumented(f.Init(e2e.StartOptions{
		Image:   image,
		Command: e2e.NewCommand("", e2e.BuildArgs(args)...),
		// Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200), TODO(bwplotka): Configure health checks in otel?
		EnvVars: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": filepath.Join(f.Dir(), "gcm-sa.json")},
	}), "http")
}

func (o OtelGCMBackend) StartAndWaitReady(t testing.TB, env e2e.Environment) promqle2e.RunningBackend {
	t.Helper()

	ctx := t.Context()

	creds, err := google.CredentialsFromJSON(ctx, o.GCMSA, gcm.DefaultAuthScopes()...)
	if err != nil {
		t.Fatalf("create credentials from JSON: %s", err)
	}

	// Fake, does not matter.
	cluster := "pe-github-action"
	location := "europe-west3-a"

	cl, err := api.NewClient(api.Config{
		Address: fmt.Sprintf("https://monitoring.googleapis.com/v1/projects/%s/location/global/prometheus", creds.ProjectID),
		Client:  oauth2.NewClient(ctx, creds.TokenSource),
	})
	if err != nil {
		t.Fatalf("create Prometheus client: %s", err)
	}

	replayer := promqle2e.StartIngestByScrapeReplayer(t, env)

	otelCollector := newOtelCollector(env, o.Name, o.Image, replayer.Endpoint(env), cluster, location, creds.ProjectID, o.GCMSA)
	if err := e2e.StartAndWaitReady(otelCollector); err != nil {
		t.Fatal(err)
	}
	return promqle2e.NewRunningScrapeReplayBasedBackend(
		replayer,
		map[string]string{
			"cluster":    cluster,
			"location":   location,
			"project_id": creds.ProjectID,
			"collector":  o.Name,
			"job":        "test",
			// TODO(bwplotka): Label drop this properly. Somehow metric relabel instance
			// labeldrop is not enough. Something with service.instance.id?
			"instance": replayer.Endpoint(env),
		},
		v1.NewAPI(cl),
	)
}
