package promqle2etest

//
//import (
//	"context"
//	"fmt"
//	"net"
//	"net/http"
//	"os"
//	"path/filepath"
//	"strconv"
//	"testing"
//	"time"
//
//	"github.com/efficientgo/e2e"
//	e2emon "github.com/efficientgo/e2e/monitoring"
//	"github.com/go-kit/log"
//	"github.com/prometheus/client_golang/api"
//	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
//	"github.com/prometheus/client_golang/prometheus/promhttp"
//	dto "github.com/prometheus/client_model/go"
//	"github.com/prometheus/compliance/promqle2e"
//	"github.com/thanos-io/thanos/pkg/runutil"
//)
//
//var _ promqle2e.Backend = OtelGCMBackend{}
//
//// gcmServiceAccountOrFail gets the Google SA JSON content from GCM_SECRET
//// environment variable or fails.
//func gcmServiceAccountOrFail(t testing.TB) []byte {
//	saJSON := []byte(os.Getenv("GCM_SECRET"))
//	if len(saJSON) == 0 {
//		t.Fatal("GCMServiceAccountOrFail: no GCM_SECRET env var provided, can't run the test")
//	}
//	return saJSON
//}
//
//var defaultOtelGCMBackend = OtelGCMBackend{
//	Image: "quay.io/prometheus/prometheus:v3.2.0",
//	Name:  "otel-gcm",
//}
//
//// OtelGCMBackend represents an OpenTelemetry OSS collector using prometheus
//// receiver to scrape OpenMetrics metric and deliver to GCM for consumption.
//// See defaultOtelGCMBackend for defaults.
//type OtelGCMBackend struct {
//	Image string
//	Name  string
//}
//
//func (opts OtelGCMBackend) Ref() string {
//	if opts.Name == "" {
//		return defaultOtelGCMBackend.Name
//	}
//	return opts.Name
//}
//
//type otelGCMBackend struct {
//	g                *promqle2e.RecordedGatherer
//	collectionLabels map[string]string
//
//	api           v1.API
//	otelCollector *e2emon.InstrumentedRunnable
//}
//
//func (opts OtelGCMBackend) StartAndWaitReady(t testing.TB, env e2e.Environment) promqle2e.RunningBackend {
//	if opts.Image == "" {
//		opts.Image = defaultOtelGCMBackend.Image
//	}
//	if opts.Name == "" {
//		opts.Name = defaultOtelGCMBackend.Name
//	}
//
//	p := &otelGCMBackend{
//		g:                promqle2e.NewRecordedGatherer(),
//		collectionLabels: map[string]string{"job": "test"},
//	}
//
//	// Setup local HTTP server with OpenMetrics /metrics page.
//	m := http.NewServeMux()
//	m.Handle("/metrics", promhttp.HandlerFor(p.g, promhttp.HandlerOpts{
//		EnableOpenMetrics: true,
//	}))
//
//	// Listen on all addresses, since we need to connect to it from docker container.
//	list, err := net.Listen("tcp", "0.0.0.0:0")
//	if err != nil {
//		t.Fatal(err)
//	}
//
//	_, port, err := net.SplitHostPort(list.Addr().String())
//	if err != nil {
//		t.Fatal(err)
//	}
//
//	// Start the server.
//	s := http.Server{Handler: m}
//	go func() { _ = s.Serve(list) }()
//	env.AddCloser(func() { _ = s.Close() })
//
//	// Create Prometheus container that scrapes our server.
//	p.p = newPrometheus(env, opts.Name, opts.Image, net.JoinHostPort(env.HostAddr(), port), nil)
//
//	// Because of scrape config, we expect a job label on top of app labels.
//	p.collectionLabels = map[string]string{"job": "test"}
//
//	if err := e2e.StartAndWaitReady(p.p); err != nil {
//		t.Fatalf("can't start %v: %v", opts.Name, err)
//	}
//
//	cl, err := api.NewClient(api.Config{Address: "http://" + p.p.Endpoint("http")})
//	if err != nil {
//		t.Fatalf("failed to create Prometheus client for %v: %s", opts.Name, err)
//	}
//	p.api = v1.NewAPI(cl)
//	return p
//}
//
//func newPrometheus(env e2e.Environment, name string, image string, scrapeTargetAddress string, flagOverride map[string]string) *e2emon.Prometheus {
//	ports := map[string]int{"http": 9090}
//
//	f := env.Runnable(name).WithPorts(ports).Future()
//	config := fmt.Sprintf(`
//global:
//  external_labels:
//    collector: %v
//scrape_configs:
//- job_name: 'test'
//  scrape_interval: 5s
//  scrape_timeout: 5s
//  static_configs:
//  - targets: [%s]
//  metric_relabel_configs:
//  - regex: instance
//    action: labeldrop
//`, name, scrapeTargetAddress)
//	if err := os.WriteFile(filepath.Join(f.Dir(), "prometheus.yml"), []byte(config), 0600); err != nil {
//		return &e2emon.Prometheus{Runnable: e2e.NewFailedRunnable(name, fmt.Errorf("create prometheus config failed: %w", err))}
//	}
//
//	args := map[string]string{
//		"--web.listen-address":               fmt.Sprintf(":%d", ports["http"]),
//		"--config.file":                      filepath.Join(f.Dir(), "prometheus.yml"),
//		"--storage.tsdb.path":                f.Dir(),
//		"--enable-feature=exemplar-storage":  "",
//		"--enable-feature=native-histograms": "",
//		"--storage.tsdb.no-lockfile":         "",
//		"--storage.tsdb.retention.time":      "1d",
//		"--storage.tsdb.wal-compression":     "",
//		"--storage.tsdb.min-block-duration":  "2h",
//		"--storage.tsdb.max-block-duration":  "2h",
//		"--web.enable-lifecycle":             "",
//		"--log.format":                       "json",
//		"--log.level":                        "info",
//	}
//	if flagOverride != nil {
//		args = e2e.MergeFlagsWithoutRemovingEmpty(args, flagOverride)
//	}
//
//	p := e2emon.AsInstrumented(f.Init(e2e.StartOptions{
//		Image:     image,
//		Command:   e2e.NewCommandWithoutEntrypoint("prometheus", e2e.BuildArgs(args)...),
//		Readiness: e2e.NewHTTPReadinessProbe("http", "/-/ready", 200, 200),
//		User:      strconv.Itoa(os.Getuid()),
//	}), "http")
//
//	return &e2emon.Prometheus{
//		Runnable:     p,
//		Instrumented: p,
//	}
//}
//
//func (p *runningOtelGCMBackend) API() v1.API {
//	return p.api
//}
//
//func (p *runningOtelGCMBackend) CollectionLabels() map[string]string {
//	return p.collectionLabels
//}
//
//func (p *runningOtelGCMBackend) IngestSamples(ctx context.Context, t testing.TB, recorded [][]*dto.MetricFamily) {
//	t.Helper()
//
//	p.g.mu.Lock()
//	p.g.i = 0
//	p.g.recordedScrapes = recorded
//	p.g.mu.Unlock()
//
//	if err := runutil.RetryWithLog(log.NewJSONLogger(os.Stderr), 10*time.Second, ctx.Done(), func() error {
//		p.g.mu.Lock()
//		iter := p.g.i
//		p.g.mu.Unlock()
//
//		if iter < len(p.g.recordedScrapes) {
//			return fmt.Errorf("backend didn't scrape the target enough number of times, got %v, expected %v", iter, len(p.g.recordedScrapes))
//		}
//		return nil
//	}); err != nil {
//		t.Fatal(t.Name(), err, "within expected time")
//	}
//}
