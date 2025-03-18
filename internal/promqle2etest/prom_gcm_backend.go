package promqle2etest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	gcm "cloud.google.com/go/monitoring/apiv3/v2"
	"github.com/efficientgo/e2e"
	e2emon "github.com/efficientgo/e2e/monitoring"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/compliance/promqle2e"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/textparse"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"golang.org/x/oauth2"

	"github.com/go-kit/log"
	"golang.org/x/oauth2/google"
)

var _ promqle2e.Backend = PrometheusForkGCMBackend{}

// GCMServiceAccountOrFail gets the Google SA JSON content from GCM_SECRET
// environment variable or fails.
func GCMServiceAccountOrFail(t testing.TB) []byte {
	saJSON := []byte(os.Getenv("GCM_SECRET"))
	if len(saJSON) == 0 {
		t.Fatal("GCMServiceAccountOrFail: no GCM_SECRET env var provided, can't run the test")
	}
	return saJSON
}

// PrometheusForkGCMBackend represents a Prometheus GMP fork scraping OpenMetrics
// metrics and pushing to GCM API for consumption.
type PrometheusForkGCMBackend struct {
	Image string
	Name  string
	GCMSA []byte
}

func (p PrometheusForkGCMBackend) Ref() string {
	return p.Name
}

// newPrometheus creates a new Prometheus runnable.
func newPrometheus(env e2e.Environment, name string, image string, scrapeTargetAddress string, flagOverride map[string]string) *e2emon.Prometheus {
	ports := map[string]int{"http": 9090}

	f := env.Runnable(name).WithPorts(ports).Future()
	config := fmt.Sprintf(`
global:
  external_labels:
    collector: %v
scrape_configs:
- job_name: 'test'
  scrape_interval: 5s
  scrape_timeout: 5s
  static_configs:
  - targets: [%s]
  metric_relabel_configs:
  - regex: instance
    action: labeldrop
`, name, scrapeTargetAddress)
	if err := os.WriteFile(filepath.Join(f.Dir(), "prometheus.yml"), []byte(config), 0600); err != nil {
		return &e2emon.Prometheus{Runnable: e2e.NewFailedRunnable(name, fmt.Errorf("create prometheus config failed: %w", err))}
	}

	args := map[string]string{
		"--web.listen-address":               fmt.Sprintf(":%d", ports["http"]),
		"--config.file":                      filepath.Join(f.Dir(), "prometheus.yml"),
		"--storage.tsdb.path":                f.Dir(),
		"--enable-feature=exemplar-storage":  "",
		"--enable-feature=native-histograms": "",
		"--storage.tsdb.no-lockfile":         "",
		"--storage.tsdb.retention.time":      "1d",
		"--storage.tsdb.wal-compression":     "",
		"--storage.tsdb.min-block-duration":  "2h",
		"--storage.tsdb.max-block-duration":  "2h",
		"--web.enable-lifecycle":             "",
		"--log.format":                       "json",
		"--log.level":                        "info",
	}
	if flagOverride != nil {
		args = e2e.MergeFlagsWithoutRemovingEmpty(args, flagOverride)
	}

	p := e2emon.AsInstrumented(f.Init(e2e.StartOptions{
		Image:     image,
		Command:   e2e.NewCommandWithoutEntrypoint("prometheus", e2e.BuildArgs(args)...),
		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
		User:      strconv.Itoa(os.Getuid()),
	}), "http")

	return &e2emon.Prometheus{
		Runnable:     p,
		Instrumented: p,
	}
}

type promForkGCMBackend struct {
	replayer         *promqle2e.IngestByScrapeReplayer
	collectionLabels map[string]string

	api        v1.API
	prometheus *e2emon.Prometheus
}

func (p PrometheusForkGCMBackend) StartAndWaitReady(t testing.TB, env e2e.Environment) promqle2e.RunningBackend {
	t.Helper()

	ctx := t.Context()

	creds, err := google.CredentialsFromJSON(ctx, p.GCMSA, gcm.DefaultAuthScopes()...)
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
	prom := newPrometheus(env, p.Name, p.Image, replayer.Endpoint(env), map[string]string{
		// Flags
	}})

	exporterOpts := export.ExporterOpts{
		UserAgentEnv:        "pe-github-action-test",
		Cluster:             cluster,
		Location:            location,
		ProjectID:           creds.ProjectID,
		CredentialsFromJSON: l.gcmSA,
	}
	exporterOpts.DefaultUnsetFields()
	l.e, err = export.New(ctx, log.NewJSONLogger(os.Stderr), nil, exporterOpts, export.NopLease())
	if err != nil {
		t.Fatalf("create exporter: %v", err)
	}

	// Apply empty config, so resources labels are attached.
	if err := l.e.ApplyConfig(&config.DefaultConfig, nil); err != nil {
		t.Fatalf("apply config: %v", err)
	}
	l.e.SetLabelsByIDFunc(func(ref storage.SeriesRef) labels.Labels {
		return l.labelsByRef[ref]
	})

	go func() {
		if err := l.e.Run(); err != nil {
			t.Logf("running exporter: %s", err)
		}
	}()

	return &promForkGCMBackend{
		replayer: replayer,
		api:      v1.NewAPI(cl),
		collectionLabels: map[string]string{
			"cluster":    cluster,
			"location":   location,
			"project_id": creds.ProjectID,
		},
		prometheus: prom,
	}
}

/*
 --[no-]export.disable      Disable exporting to GCM.
      --export.endpoint="monitoring.googleapis.com:443"
                                 GCM API endpoint to send metric data to.
      --export.compression=none  The compression format to use for gRPC requests ('none' or 'gzip').
      --export.credentials-file=""
                                 Credentials file for authentication with the GCM API.
      --export.label.project-id=""
                                 Default project ID set for all exported data. Prefer setting the external label "project_id" in the Prometheus configuration if not using the auto-discovered default.
      --export.user-agent-mode=unspecified
                                 Mode for user agent used for requests against the GCM API. Valid values are "gke", "kubectl", "on-prem", "baremetal" or "unspecified".
      --export.label.location=""
                                 The default location set for all exported data. Prefer setting the external label "location" in the Prometheus configuration if not using the auto-discovered default.
      --export.label.cluster=""  The default cluster set for all scraped targets. Prefer setting the external label "cluster" in the Prometheus configuration if not using the auto-discovered default.
      --export.match= ...        A Prometheus time series matcher. Can be repeated. Every time series must match at least one of the matchers to be exported. This flag can be used equivalently to the match[] parameter of the Prometheus federation endpoint to selectively export data. (Example: --export.match='{job="prometheus"}' --export.match='{__name__=~"job:.*"})
      --export.debug.metric-prefix="prometheus.googleapis.com"
                                 Google Cloud Monitoring metric prefix to use.
      --[no-]export.debug.disable-auth
                                 Disable authentication (for debugging purposes).
      --export.debug.batch-size=200
                                 Maximum number of points to send in one batch to the GCM API.
      --export.debug.shard-count=1024
                                 Number of shards that track series to send.
      --export.debug.shard-buffer-size=2048
                                 The buffer size for each individual shard. Each element in buffer (queue) consists of sample and hash.
      --export.token-url=""      The request URL to generate token that's needed to ingest metrics to the project
      --export.token-body=""     The request Body to generate token that's needed to ingest metrics to the project.
      --export.quota-project=""  The projectID of an alternative project for quota attribution.
      --export.debug.fetch-metadata-timeout=10s
                                 The total timeout for the initial gathering of the best-effort GCP data from the metadata server. This data is used for special labels required by Prometheus metrics (e.g. project id, location, cluster name), as well as information for the user agent. This is done on startup, so make sure this work to be faster than your readiness and liveliness probes.
      --export.ha.backend=none   Which backend to use to coordinate HA pairs that both send metric data to the GCM API. Valid values are "none" or "kube"
      --export.ha.kube.config=""
                                 Path to kube config file.
      --export.ha.kube.namespace=""
                                 Namespace for the HA locking resource. Must be identical across replicas. May be set through the KUBE_NAMESPACE environment variable. ($KUBE_NAMESPACE)
      --export.ha.kube.name=""   Name for the HA locking resource. Must be identical across replicas. May be set through the KUBE_NAME environment variable. ($KUBE_NAME)

 */
func (p *promForkGCMBackend) API() v1.API {
	return p.api
}

func (p *promForkGCMBackend) CollectionLabels() map[string]string {
	return p.collectionLabels
}

func (p *promForkGCMBackend) IngestSamples(ctx context.Context, t testing.TB, recorded [][]*dto.MetricFamily) {
	p.replayer.IngestSamples(ctx, t, recorded)
}
