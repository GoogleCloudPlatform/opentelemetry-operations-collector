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

package policy_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/extension/googlecontrolplaneextension"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/processor/googlepolicyprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/filelogsource"
	"github.com/google/go-cmp/cmp"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/filelogreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/iisreceiver"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/envprovider"
	"go.opentelemetry.io/collector/confmap/provider/fileprovider"
	"go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/nopexporter"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/extension/xextension/storage"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.yaml.in/yaml/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/encoding/gzip"
)

var (
	otelcolFlag = flag.String("otelcol", os.Getenv("OTELCOL"), "optional path to compiled otelcol-google binary (defaults to running otelcol.NewCollector in-process)")
	updateFlag  = flag.Bool("update", false, "update golden expected_*.yaml files")
)

func resolveOtelcolBinary(t *testing.T) string {
	t.Helper()
	if *otelcolFlag != "" {
		abs, err := filepath.Abs(*otelcolFlag)
		if err != nil {
			t.Fatalf("failed to resolve --otelcol path %q: %v", *otelcolFlag, err)
		}
		if _, err := os.Stat(abs); err != nil {
			t.Fatalf("--otelcol binary not found at %q: %v", abs, err)
		}
		return abs
	}
	return ""
}

type mockLogsService struct {
	plogotlp.UnimplementedGRPCServer
	parent *mockOTLPServer
}

func (s *mockLogsService) Export(_ context.Context, req plogotlp.ExportRequest) (plogotlp.ExportResponse, error) {
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	cloned := plog.NewLogs()
	req.Logs().CopyTo(cloned)
	s.parent.logs = append(s.parent.logs, cloned)
	return plogotlp.NewExportResponse(), nil
}

type mockMetricsService struct {
	pmetricotlp.UnimplementedGRPCServer
	parent *mockOTLPServer
}

func (s *mockMetricsService) Export(_ context.Context, req pmetricotlp.ExportRequest) (pmetricotlp.ExportResponse, error) {
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	cloned := pmetric.NewMetrics()
	req.Metrics().CopyTo(cloned)
	s.parent.metrics = append(s.parent.metrics, cloned)
	return pmetricotlp.NewExportResponse(), nil
}

type mockTracesService struct {
	ptraceotlp.UnimplementedGRPCServer
	parent *mockOTLPServer
}

func (s *mockTracesService) Export(_ context.Context, req ptraceotlp.ExportRequest) (ptraceotlp.ExportResponse, error) {
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	cloned := ptrace.NewTraces()
	req.Traces().CopyTo(cloned)
	s.parent.traces = append(s.parent.traces, cloned)
	return ptraceotlp.NewExportResponse(), nil
}

type mockOTLPServer struct {
	srv     *grpc.Server
	mu      sync.Mutex
	logs    []plog.Logs
	metrics []pmetric.Metrics
	traces  []ptrace.Traces
}

func (s *mockOTLPServer) CollectedLogs() []plog.Logs {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]plog.Logs, len(s.logs))
	copy(out, s.logs)
	return out
}

func (s *mockOTLPServer) CollectedMetrics() []pmetric.Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]pmetric.Metrics, len(s.metrics))
	copy(out, s.metrics)
	return out
}

func (s *mockOTLPServer) CollectedTraces() []ptrace.Traces {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ptrace.Traces, len(s.traces))
	copy(out, s.traces)
	return out
}

func startMockOTLPServer(t *testing.T) (*mockOTLPServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen for mock OTLP server: %v", err)
	}
	s := &mockOTLPServer{
		srv: grpc.NewServer(),
	}
	plogotlp.RegisterGRPCServer(s.srv, &mockLogsService{parent: s})
	pmetricotlp.RegisterGRPCServer(s.srv, &mockMetricsService{parent: s})
	ptraceotlp.RegisterGRPCServer(s.srv, &mockTracesService{parent: s})
	go func() {
		_ = s.srv.Serve(ln)
	}()
	t.Cleanup(func() {
		s.srv.GracefulStop()
	})
	return s, ln.Addr().String()
}

var (
	ephemeralPortMu      sync.Mutex
	ephemeralPortCounter int = 21000
)

func allocateEphemeralPorts(t *testing.T, count int) []int {
	t.Helper()
	ephemeralPortMu.Lock()
	defer ephemeralPortMu.Unlock()
	ports := make([]int, count)
	for i := 0; i < count; i++ {
		ephemeralPortCounter++
		if ephemeralPortCounter > 32000 {
			ephemeralPortCounter = 21001
		}
		ports[i] = ephemeralPortCounter
	}
	return ports
}

func readYAMLAsJSONBytes(t *testing.T, yamlPath string) []byte {
	t.Helper()
	rawYAML, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", yamlPath, err)
	}
	var obj any
	if err := yaml.Unmarshal(rawYAML, &obj); err != nil {
		t.Fatalf("failed to unmarshal YAML %s: %v", yamlPath, err)
	}
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("failed to marshal input YAML to JSON: %v", err)
	}
	return jsonBytes
}

func readInputLogs(t *testing.T, yamlPath string) plog.Logs {
	t.Helper()
	unmarshaler := &plog.JSONUnmarshaler{}
	logs, err := unmarshaler.UnmarshalLogs(readYAMLAsJSONBytes(t, yamlPath))
	if err != nil {
		t.Fatalf("failed to unmarshal OTLP logs from %s: %v", yamlPath, err)
	}
	return logs
}

func readInputMetrics(t *testing.T, yamlPath string) pmetric.Metrics {
	t.Helper()
	unmarshaler := &pmetric.JSONUnmarshaler{}
	metrics, err := unmarshaler.UnmarshalMetrics(readYAMLAsJSONBytes(t, yamlPath))
	if err != nil {
		t.Fatalf("failed to unmarshal OTLP metrics from %s: %v", yamlPath, err)
	}
	return metrics
}

func readInputTraces(t *testing.T, yamlPath string) ptrace.Traces {
	t.Helper()
	unmarshaler := &ptrace.JSONUnmarshaler{}
	traces, err := unmarshaler.UnmarshalTraces(readYAMLAsJSONBytes(t, yamlPath))
	if err != nil {
		t.Fatalf("failed to unmarshal OTLP traces from %s: %v", yamlPath, err)
	}
	return traces
}

func normalizeJSONDoc(t *testing.T, jsonBytes []byte, testStartTime time.Time) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(jsonBytes, &doc); err != nil {
		t.Fatalf("failed to unmarshal exported JSON to map: %v", err)
	}
	sanitizeTimestampAndSort(doc, testStartTime)
	return doc
}

func normalizeExportedLogs(t *testing.T, batches []plog.Logs, testStartTime time.Time) []map[string]any {
	t.Helper()
	marshaler := &plog.JSONMarshaler{}
	var out []map[string]any
	for _, batch := range batches {
		if batch.LogRecordCount() == 0 {
			continue
		}
		jsonBytes, err := marshaler.MarshalLogs(batch)
		if err != nil {
			t.Fatalf("failed to marshal exported logs: %v", err)
		}
		out = append(out, normalizeJSONDoc(t, jsonBytes, testStartTime))
	}
	return out
}

func normalizeExportedMetrics(t *testing.T, batches []pmetric.Metrics, testStartTime time.Time) []map[string]any {
	t.Helper()
	marshaler := &pmetric.JSONMarshaler{}
	var out []map[string]any
	for _, batch := range batches {
		if batch.MetricCount() == 0 {
			continue
		}
		jsonBytes, err := marshaler.MarshalMetrics(batch)
		if err != nil {
			t.Fatalf("failed to marshal exported metrics: %v", err)
		}
		out = append(out, normalizeJSONDoc(t, jsonBytes, testStartTime))
	}
	return out
}

func normalizeExportedTraces(t *testing.T, batches []ptrace.Traces, testStartTime time.Time) []map[string]any {
	t.Helper()
	marshaler := &ptrace.JSONMarshaler{}
	var out []map[string]any
	for _, batch := range batches {
		if batch.SpanCount() == 0 {
			continue
		}
		jsonBytes, err := marshaler.MarshalTraces(batch)
		if err != nil {
			t.Fatalf("failed to marshal exported traces: %v", err)
		}
		out = append(out, normalizeJSONDoc(t, jsonBytes, testStartTime))
	}
	return out
}

func sanitizeTimestampAndSort(v any, testStartTime time.Time) {
	switch v := v.(type) {
	case map[string]any:
		for key, value := range v {
			if strings.Contains(strings.ToLower(key), "timeunixnano") {
				if timeStr, ok := value.(string); ok {
					if nano, err := strconv.ParseInt(timeStr, 10, 64); err == nil && time.Unix(0, nano).After(testStartTime) {
						v[key] = "now"
					}
				}
			} else {
				sanitizeTimestampAndSort(value, testStartTime)
			}
		}
	case []any:
		sort.SliceStable(v, func(i, j int) bool {
			m1, ok1 := v[i].(map[string]any)
			m2, ok2 := v[j].(map[string]any)
			if ok1 && ok2 {
				k1, ok1 := m1["key"].(string)
				k2, ok2 := m2["key"].(string)
				if ok1 && ok2 {
					return k1 < k2
				}
			}
			return false
		})
		for _, item := range v {
			sanitizeTimestampAndSort(item, testStartTime)
		}
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("failed to read directory %s: %v", src, err)
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", dst, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			t.Fatalf("failed to read file %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), data, 0644); err != nil {
			t.Fatalf("failed to write file %s: %v", entry.Name(), err)
		}
	}
}

func assertGoldenYAML(t *testing.T, expectedPath string, got []map[string]any, stderrLog string) {
	t.Helper()
	gotBytes, err := yaml.Marshal(got)
	if err != nil {
		t.Fatalf("failed to marshal golden YAML: %v", err)
	}

	if *updateFlag || os.Getenv("UPDATE_GOLDEN") == "true" {
		if err := os.WriteFile(expectedPath, gotBytes, 0644); err != nil {
			t.Fatalf("failed to write golden file %s: %v", expectedPath, err)
		}
		return
	}

	wantBytes, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("failed to read expected golden file %s (run with -update to generate): %v", expectedPath, err)
	}
	var want []map[string]any
	if err := yaml.Unmarshal(wantBytes, &want); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", expectedPath, err)
	}

	var gotNormalized []map[string]any
	if err := yaml.Unmarshal(gotBytes, &gotNormalized); err != nil {
		t.Fatalf("failed to unmarshal normalized got YAML: %v", err)
	}

	if diff := cmp.Diff(want, gotNormalized); diff != "" {
		if len(stderrLog) > 4096 {
			stderrLog = stderrLog[:4096]
		}
		t.Fatalf("exported telemetry mismatch for %s (-want +got):\n%s\ncollector stderr:\n%s", expectedPath, diff, stderrLog)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type stubComponentConfig struct{}

func (*stubComponentConfig) Unmarshal(*confmap.Conf) error {
	return nil
}

// stubExtension is a no-op extension. The factory must return a real value
// rather than nil, because the provider's service::extensions list is started
// as is, and starting a nil extension would crash the collector. It implements
// storage.Extension so that storage-backed receivers (e.g. filelog, windowseventlog)
// can retrieve a storage client in in-process integration tests.
type stubExtension struct {
	component.StartFunc
	component.ShutdownFunc
}

func (*stubExtension) GetClient(context.Context, component.Kind, component.ID, string) (storage.Client, error) {
	return storage.NewNopClient(), nil
}

var _ storage.Extension = (*stubExtension)(nil)

func newStubExtensionFactory(typeStr string) extension.Factory {
	return extension.NewFactory(
		component.MustNewType(typeStr),
		func() component.Config { return &stubComponentConfig{} },
		func(context.Context, extension.Settings, component.Config) (extension.Extension, error) {
			return &stubExtension{}, nil
		},
		component.StabilityLevelAlpha,
	)
}

func newStubExporterFactory(typeStr string) exporter.Factory {
	return exporter.NewFactory(
		component.MustNewType(typeStr),
		func() component.Config { return &stubComponentConfig{} },
	)
}

type stubReceiver struct {
	component.StartFunc
	component.ShutdownFunc
}

func newStubReceiverFactory(typeStr string) receiver.Factory {
	return receiver.NewFactory(
		component.MustNewType(typeStr),
		func() component.Config { return &stubComponentConfig{} },
		receiver.WithTraces(func(context.Context, receiver.Settings, component.Config, consumer.Traces) (receiver.Traces, error) {
			return &stubReceiver{}, nil
		}, component.StabilityLevelAlpha),
		receiver.WithMetrics(func(context.Context, receiver.Settings, component.Config, consumer.Metrics) (receiver.Metrics, error) {
			return &stubReceiver{}, nil
		}, component.StabilityLevelAlpha),
		receiver.WithLogs(func(context.Context, receiver.Settings, component.Config, consumer.Logs) (receiver.Logs, error) {
			return &stubReceiver{}, nil
		}, component.StabilityLevelAlpha),
	)
}

type stubTracesProcessor struct {
	component.StartFunc
	component.ShutdownFunc
	consumer.Traces
}

type stubMetricsProcessor struct {
	component.StartFunc
	component.ShutdownFunc
	consumer.Metrics
}

type stubLogsProcessor struct {
	component.StartFunc
	component.ShutdownFunc
	consumer.Logs
}

type envDetectorAttributesConfig struct {
	Included []string `mapstructure:"included"`
	Excluded []string `mapstructure:"excluded"`
}

type envDetectorConfig struct {
	Attributes envDetectorAttributesConfig `mapstructure:"attributes"`
}

type resourceDetectionConfig struct {
	Detectors []string          `mapstructure:"detectors"`
	Override  bool              `mapstructure:"override"`
	Timeout   time.Duration     `mapstructure:"timeout"`
	Env       envDetectorConfig `mapstructure:"env"`
	System    any               `mapstructure:"system"`
}

func matchEnvWildcardPatterns(patterns []string, key string) bool {
	for _, p := range patterns {
		quoted := regexp.QuoteMeta(p)
		expr := "(?s)^" + strings.ReplaceAll(strings.ReplaceAll(quoted, `\*`, `.*`), `\?`, `.`) + "$"
		if matched, err := regexp.MatchString(expr, key); err == nil && matched {
			return true
		}
	}
	return false
}

func applyEnvResourceAttributes(attrs pcommon.Map, cfg *resourceDetectionConfig) {
	envStr := os.Getenv("OTEL_RESOURCE_ATTRIBUTES")
	if envStr == "" {
		return
	}
	for _, pair := range strings.Split(envStr, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 {
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			if k != "" {
				if len(cfg.Env.Attributes.Included) > 0 && !matchEnvWildcardPatterns(cfg.Env.Attributes.Included, k) {
					continue
				}
				if len(cfg.Env.Attributes.Excluded) > 0 && matchEnvWildcardPatterns(cfg.Env.Attributes.Excluded, k) {
					continue
				}
				if _, exists := attrs.Get(k); !exists || cfg.Override {
					attrs.PutStr(k, v)
				}
			}
		}
	}
}

type resourceDetectionTracesProcessor struct {
	component.StartFunc
	component.ShutdownFunc
	cfg *resourceDetectionConfig
	consumer.Traces
}

func (p *resourceDetectionTracesProcessor) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	if slices.Contains(p.cfg.Detectors, "env") {
		for i := 0; i < td.ResourceSpans().Len(); i++ {
			rs := td.ResourceSpans().At(i)
			applyEnvResourceAttributes(rs.Resource().Attributes(), p.cfg)
		}
	}
	return p.Traces.ConsumeTraces(ctx, td)
}

type resourceDetectionMetricsProcessor struct {
	component.StartFunc
	component.ShutdownFunc
	cfg *resourceDetectionConfig
	consumer.Metrics
}

func (p *resourceDetectionMetricsProcessor) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	if slices.Contains(p.cfg.Detectors, "env") {
		for i := 0; i < md.ResourceMetrics().Len(); i++ {
			rm := md.ResourceMetrics().At(i)
			applyEnvResourceAttributes(rm.Resource().Attributes(), p.cfg)
		}
	}
	return p.Metrics.ConsumeMetrics(ctx, md)
}

type resourceDetectionLogsProcessor struct {
	component.StartFunc
	component.ShutdownFunc
	cfg *resourceDetectionConfig
	consumer.Logs
}

func (p *resourceDetectionLogsProcessor) ConsumeLogs(ctx context.Context, ld plog.Logs) error {
	if slices.Contains(p.cfg.Detectors, "env") {
		for i := 0; i < ld.ResourceLogs().Len(); i++ {
			rl := ld.ResourceLogs().At(i)
			applyEnvResourceAttributes(rl.Resource().Attributes(), p.cfg)
		}
	}
	return p.Logs.ConsumeLogs(ctx, ld)
}

func newResourceDetectionProcessorFactory() processor.Factory {
	return processor.NewFactory(
		component.MustNewType("resourcedetection"),
		func() component.Config { return &resourceDetectionConfig{} },
		processor.WithTraces(func(_ context.Context, _ processor.Settings, cfg component.Config, next consumer.Traces) (processor.Traces, error) {
			return &resourceDetectionTracesProcessor{cfg: cfg.(*resourceDetectionConfig), Traces: next}, nil
		}, component.StabilityLevelAlpha),
		processor.WithMetrics(func(_ context.Context, _ processor.Settings, cfg component.Config, next consumer.Metrics) (processor.Metrics, error) {
			return &resourceDetectionMetricsProcessor{cfg: cfg.(*resourceDetectionConfig), Metrics: next}, nil
		}, component.StabilityLevelAlpha),
		processor.WithLogs(func(_ context.Context, _ processor.Settings, cfg component.Config, next consumer.Logs) (processor.Logs, error) {
			return &resourceDetectionLogsProcessor{cfg: cfg.(*resourceDetectionConfig), Logs: next}, nil
		}, component.StabilityLevelAlpha),
	)
}

type transformContextStatements struct {
	Context    string   `mapstructure:"context"`
	Statements []string `mapstructure:"statements"`
}

type transformProcessorConfig struct {
	ErrorMode        string                       `mapstructure:"error_mode"`
	LogStatements    []transformContextStatements `mapstructure:"log_statements"`
	MetricStatements []transformContextStatements `mapstructure:"metric_statements"`
	TraceStatements  []transformContextStatements `mapstructure:"trace_statements"`
}

var deleteKeyAttrRegex = regexp.MustCompile(`^delete_key\(attributes,\s*"([^"]+)"\)$`)

func applyResourceTransformStatements(attrs pcommon.Map, groups []transformContextStatements) {
	for _, g := range groups {
		if g.Context != "resource" {
			continue
		}
		for _, stmt := range g.Statements {
			if m := deleteKeyAttrRegex.FindStringSubmatch(strings.TrimSpace(stmt)); len(m) == 2 {
				attrs.Remove(m[1])
			}
		}
	}
}

type transformTracesProcessor struct {
	component.StartFunc
	component.ShutdownFunc
	stmts []transformContextStatements
	consumer.Traces
}

func (p *transformTracesProcessor) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		applyResourceTransformStatements(td.ResourceSpans().At(i).Resource().Attributes(), p.stmts)
	}
	return p.Traces.ConsumeTraces(ctx, td)
}

type transformMetricsProcessor struct {
	component.StartFunc
	component.ShutdownFunc
	stmts []transformContextStatements
	consumer.Metrics
}

func (p *transformMetricsProcessor) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		applyResourceTransformStatements(md.ResourceMetrics().At(i).Resource().Attributes(), p.stmts)
	}
	return p.Metrics.ConsumeMetrics(ctx, md)
}

type transformLogsProcessor struct {
	component.StartFunc
	component.ShutdownFunc
	stmts []transformContextStatements
	consumer.Logs
}

func (p *transformLogsProcessor) ConsumeLogs(ctx context.Context, ld plog.Logs) error {
	for i := 0; i < ld.ResourceLogs().Len(); i++ {
		applyResourceTransformStatements(ld.ResourceLogs().At(i).Resource().Attributes(), p.stmts)
	}
	return p.Logs.ConsumeLogs(ctx, ld)
}

func newTransformProcessorFactory() processor.Factory {
	return processor.NewFactory(
		component.MustNewType("transform"),
		func() component.Config { return &transformProcessorConfig{} },
		processor.WithTraces(func(_ context.Context, _ processor.Settings, cfg component.Config, next consumer.Traces) (processor.Traces, error) {
			return &transformTracesProcessor{stmts: cfg.(*transformProcessorConfig).TraceStatements, Traces: next}, nil
		}, component.StabilityLevelAlpha),
		processor.WithMetrics(func(_ context.Context, _ processor.Settings, cfg component.Config, next consumer.Metrics) (processor.Metrics, error) {
			return &transformMetricsProcessor{stmts: cfg.(*transformProcessorConfig).MetricStatements, Metrics: next}, nil
		}, component.StabilityLevelAlpha),
		processor.WithLogs(func(_ context.Context, _ processor.Settings, cfg component.Config, next consumer.Logs) (processor.Logs, error) {
			return &transformLogsProcessor{stmts: cfg.(*transformProcessorConfig).LogStatements, Logs: next}, nil
		}, component.StabilityLevelAlpha),
	)
}

func newStubProcessorFactory(typeStr string) processor.Factory {
	return processor.NewFactory(
		component.MustNewType(typeStr),
		func() component.Config { return &stubComponentConfig{} },
		processor.WithTraces(func(_ context.Context, _ processor.Settings, _ component.Config, next consumer.Traces) (processor.Traces, error) {
			return &stubTracesProcessor{Traces: next}, nil
		}, component.StabilityLevelAlpha),
		processor.WithMetrics(func(_ context.Context, _ processor.Settings, _ component.Config, next consumer.Metrics) (processor.Metrics, error) {
			return &stubMetricsProcessor{Metrics: next}, nil
		}, component.StabilityLevelAlpha),
		processor.WithLogs(func(_ context.Context, _ processor.Settings, _ component.Config, next consumer.Logs) (processor.Logs, error) {
			return &stubLogsProcessor{Logs: next}, nil
		}, component.StabilityLevelAlpha),
	)
}

func testCollectorFactories() (otelcol.Factories, error) {
	var err error
	factories := otelcol.Factories{
		Telemetry: otelconftelemetry.NewFactory(),
	}
	factories.Extensions, err = otelcol.MakeFactoryMap[extension.Factory](
		newStubExtensionFactory("googleclientauth"),
		newStubExtensionFactory("file_storage"),
		googlecontrolplaneextension.NewFactory(),
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	factories.Receivers, err = otelcol.MakeFactoryMap[receiver.Factory](
		otlpreceiver.NewFactory(),
		filelogreceiver.NewFactory(),
		newStubReceiverFactory("nop"),
		newStubReceiverFactory("hostmetrics"),
		iisreceiver.NewFactory(),
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	factories.Exporters, err = otelcol.MakeFactoryMap[exporter.Factory](
		otlpexporter.NewFactory(),
		nopexporter.NewFactory(),
		newStubExporterFactory("googlecloud"),
		newStubExporterFactory("googlemanagedprometheus"),
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	factories.Processors, err = otelcol.MakeFactoryMap[processor.Factory](
		googlepolicyprocessor.NewFactory(),
		newResourceDetectionProcessorFactory(),
		newTransformProcessorFactory(),
		newStubProcessorFactory("queue_batch"),
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	return factories, nil
}

type collectorHandle struct {
	shutdown func(t *testing.T)
	logs     func() string
}

func startCollector(t *testing.T, otelcolBin, policiesDir string, pipelineCfgPaths ...string) *collectorHandle {
	t.Helper()
	uris := make([]string, 0, 1+len(pipelineCfgPaths))
	uris = append(uris, fmt.Sprintf("googlecontrolplane:file:%s", filepath.ToSlash(policiesDir)))
	for _, cfgPath := range pipelineCfgPaths {
		uris = append(uris, fmt.Sprintf("file:%s", filepath.ToSlash(cfgPath)))
	}
	return startCollectorWithURIs(t, otelcolBin, uris...)
}

func startCollectorWithURIs(t *testing.T, otelcolBin string, configURIs ...string) *collectorHandle {
	t.Helper()
	if otelcolBin == "" {
		return startInProcessCollector(t, configURIs...)
	}
	return startSubprocessCollector(t, otelcolBin, configURIs...)
}

func startInProcessCollector(t *testing.T, configURIs ...string) *collectorHandle {
	t.Helper()
	t.Setenv("FLEET_ID", "test-fleet")

	logBuf := &syncBuffer{}
	set := otelcol.CollectorSettings{
		BuildInfo:               component.NewDefaultBuildInfo(),
		Factories:               testCollectorFactories,
		DisableGracefulShutdown: true,
		SkipSettingGRPCLogger:   true,
		ConfigProviderSettings: otelcol.ConfigProviderSettings{
			ResolverSettings: confmap.ResolverSettings{
				URIs: configURIs,
				ProviderFactories: []confmap.ProviderFactory{
					googlecontrolplaneprovider.NewFactory(),
					fileprovider.NewFactory(),
					envprovider.NewFactory(),
					yamlprovider.NewFactory(),
				},
				DefaultScheme: "env",
			},
		},
		LoggingOptions: []zap.Option{
			zap.WrapCore(func(core zapcore.Core) zapcore.Core {
				enc := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
				bufCore := zapcore.NewCore(enc, zapcore.AddSync(logBuf), zapcore.DebugLevel)
				return zapcore.NewTee(core, bufCore)
			}),
		},
	}

	col, err := otelcol.NewCollector(set)
	if err != nil {
		t.Fatalf("failed to create in-process collector: %v", err)
	}

	colDone := make(chan struct{})
	var runErr error
	go func() {
		defer close(colDone)
		runErr = col.Run(context.Background())
	}()

	var shutdownOnce sync.Once
	shutdownFn := func(t *testing.T) {
		shutdownOnce.Do(func() {
			col.Shutdown()
			select {
			case <-colDone:
				if runErr != nil {
					t.Logf("in-process collector exited with: %v", runErr)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("timed out waiting for in-process collector shutdown.\nlogs:\n%s", logBuf.String())
			}
			googlepolicy.SetActivePolicySet(nil)
		})
	}
	t.Cleanup(func() {
		shutdownFn(t)
	})

	deadline := time.Now().Add(15 * time.Second)
	for {
		if col.GetState() == otelcol.StateRunning {
			break
		}
		select {
		case <-colDone:
			t.Fatalf("in-process collector exited before becoming ready: %v\nlogs:\n%s", runErr, logBuf.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for in-process collector readiness (state=%v).\nlogs:\n%s", col.GetState(), logBuf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}

	return &collectorHandle{
		shutdown: shutdownFn,
		logs:     logBuf.String,
	}
}

func startSubprocessCollector(t *testing.T, otelcolBin string, configURIs ...string) *collectorHandle {
	t.Helper()
	args := make([]string, 0, len(configURIs))
	for _, uri := range configURIs {
		args = append(args, fmt.Sprintf("--config=%s", uri))
	}
	cmd := exec.Command(otelcolBin, args...)
	cmd.Env = append(os.Environ(), "FLEET_ID=test-fleet")

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("failed to attach stderr pipe: %v", err)
	}
	stderrLog := &syncBuffer{}
	cmd.Stdout = stderrLog

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start otelcol-google: %v", err)
	}

	readyCh := make(chan bool, 1)
	scannerDone := make(chan struct{})
	var readyOnce sync.Once

	go func() {
		defer close(scannerDone)
		defer readyOnce.Do(func() { readyCh <- false })
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			_, _ = stderrLog.Write([]byte(line + "\n"))
			if strings.Contains(line, "Everything is ready. Begin running and processing data.") {
				readyOnce.Do(func() { readyCh <- true })
			}
		}
	}()

	var waitOnce sync.Once
	var waitErr error
	waitProcess := func() error {
		waitOnce.Do(func() {
			<-scannerDone
			waitErr = cmd.Wait()
		})
		return waitErr
	}

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = waitProcess()
	})

	select {
	case ready := <-readyCh:
		if !ready {
			t.Fatalf("collector exited before becoming ready.\nstderr:\n%s", stderrLog.String())
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("timed out waiting for collector readiness.\nstderr:\n%s", stderrLog.String())
	}

	return &collectorHandle{
		shutdown: func(t *testing.T) {
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Logf("warning: failed to send SIGTERM to collector: %v", err)
			}
			done := make(chan error, 1)
			go func() {
				done <- waitProcess()
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Logf("collector exited with: %v", err)
				}
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("timed out waiting for collector graceful shutdown")
			}
		},
		logs: stderrLog.String,
	}
}

// testPipelineConfig is merged on top of the config generated by the
// googlecontrolplane provider. It rewires the built-in self-metrics receiver
// (otlp/default_self_metrics) and default destination exporter
// (otlp_grpc/default_gcp_destination) in place to the test's local mock servers
// with auth disabled and insecure TLS, so tests never dial real GCP endpoints.
//
// Self telemetry goes to its own mock server so it never mixes with the
// telemetry the policy test cases assert on. The self metrics receiver is
// moved to an ephemeral port, and the internal metrics reader pushes to it on
// a short interval so tests can observe it quickly, with a short timeout
// because its final flush on shutdown runs after that receiver has already
// stopped.
func testPipelineConfig(otlpReceiverAddr, selfReceiverAddr, mockExporterAddr, selfExporterAddr string) string {
	return fmt.Sprintf(`receivers:
  otlp/test_in:
    protocols:
      grpc:
        endpoint: %[1]s
  otlp/default_self_metrics:
    protocols:
      grpc:
        endpoint: %[2]s
exporters:
  otlp_grpc/test_out:
    endpoint: %[3]s
    sending_queue:
      enabled: false
    tls:
      insecure: true
  otlp_grpc/default_gcp_destination:
    endpoint: %[4]s
    auth:
      enabled: false
    sending_queue:
      enabled: false
    tls:
      insecure: true
service:
  telemetry:
    logs:
      processors: []
    metrics:
      readers:
        - periodic:
            interval: 100
            timeout: 200
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://%[2]s
                insecure: true
  pipelines:
    logs/test:
      receivers: [otlp/test_in]
      processors: [googlepolicy]
      exporters: [otlp_grpc/test_out]
    metrics/test:
      receivers: [otlp/test_in]
      processors: [googlepolicy]
      exporters: [otlp_grpc/test_out]
    traces/test:
      receivers: [otlp/test_in]
      processors: [googlepolicy]
      exporters: [otlp_grpc/test_out]
`, otlpReceiverAddr, selfReceiverAddr, mockExporterAddr, selfExporterAddr)
}

func TestPolicyIntegration(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)

	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("failed to read testdata directory: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		caseName := entry.Name()
		t.Run(caseName, func(t *testing.T) {
			if otelcolBin != "" {
				t.Parallel()
			}
			runPolicyTestCase(t, otelcolBin, caseName)
		})
	}
}

type testHarness struct {
	mockSrv          *mockOTLPServer
	selfMockSrv      *mockOTLPServer
	col              *collectorHandle
	conn             *grpc.ClientConn
	otlpReceiverAddr string
	selfReceiverAddr string
	pipelineCfgPath  string
}

func stageTestPipelineConfig(t *testing.T, tempDir string) (string, string, string, *mockOTLPServer, *mockOTLPServer) {
	t.Helper()
	mockSrv, mockExporterAddr := startMockOTLPServer(t)
	selfMockSrv, selfExporterAddr := startMockOTLPServer(t)
	ports := allocateEphemeralPorts(t, 2)
	otlpReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[0])
	selfReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[1])

	pipelineCfg := testPipelineConfig(otlpReceiverAddr, selfReceiverAddr, mockExporterAddr, selfExporterAddr)
	pipelineCfgPath := filepath.Join(tempDir, "test_pipeline.yaml")
	if err := os.WriteFile(pipelineCfgPath, []byte(pipelineCfg), 0644); err != nil {
		t.Fatalf("failed to write test_pipeline.yaml: %v", err)
	}
	return pipelineCfgPath, otlpReceiverAddr, selfReceiverAddr, mockSrv, selfMockSrv
}

func stageEnableSelfLogsConfig(t *testing.T, tempDir, selfReceiverAddr string) string {
	t.Helper()
	enableSelfLogsCfg := fmt.Sprintf(`service:
  telemetry:
    logs:
      level: info
      processors:
        - batch:
            schedule_delay: 250
            export_timeout: 200
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://%s
                insecure: true
                timeout: 200
`, selfReceiverAddr)
	enableSelfLogsPath := filepath.Join(tempDir, "enable_self_logs.yaml")
	if err := os.WriteFile(enableSelfLogsPath, []byte(enableSelfLogsCfg), 0644); err != nil {
		t.Fatalf("failed to write enable_self_logs.yaml: %v", err)
	}
	return enableSelfLogsPath
}

func newTestHarness(t *testing.T, tempDir string) *testHarness {
	t.Helper()
	pipelineCfgPath, otlpReceiverAddr, selfReceiverAddr, mockSrv, selfMockSrv := stageTestPipelineConfig(t, tempDir)
	return &testHarness{
		mockSrv:          mockSrv,
		selfMockSrv:      selfMockSrv,
		otlpReceiverAddr: otlpReceiverAddr,
		selfReceiverAddr: selfReceiverAddr,
		pipelineCfgPath:  pipelineCfgPath,
	}
}

func (h *testHarness) start(t *testing.T, otelcolBin, policiesDir string, extraCfgPaths ...string) {
	t.Helper()
	cfgPaths := append([]string{h.pipelineCfgPath}, extraCfgPaths...)
	h.col = startCollector(t, otelcolBin, policiesDir, cfgPaths...)

	conn, err := grpc.NewClient(h.otlpReceiverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		h.col.shutdown(t)
		t.Fatalf("failed to create gRPC client to %s: %v", h.otlpReceiverAddr, err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})
	h.conn = conn
}

func startTestHarness(t *testing.T, otelcolBin, tempDir, policiesDir string, extraCfgPaths ...string) *testHarness {
	t.Helper()
	h := newTestHarness(t, tempDir)
	h.start(t, otelcolBin, policiesDir, extraCfgPaths...)
	return h
}

func startTestHarnessWithSelfLogs(t *testing.T, otelcolBin, tempDir, policiesDir string, extraCfgPaths ...string) *testHarness {
	t.Helper()
	h := newTestHarness(t, tempDir)
	selfLogsCfgPath := stageEnableSelfLogsConfig(t, tempDir, h.selfReceiverAddr)
	h.start(t, otelcolBin, policiesDir, append([]string{selfLogsCfgPath}, extraCfgPaths...)...)
	return h
}

func runPolicyTestCase(t *testing.T, otelcolBin, caseName string) {
	caseDir := filepath.Join("testdata", caseName)
	tempDir := t.TempDir()

	policiesDir := filepath.Join(tempDir, "policies")
	copyDir(t, filepath.Join(caseDir, "policies"), policiesDir)

	testStartTime := time.Now()
	h := startTestHarness(t, otelcolBin, tempDir, policiesDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	inputLogsPath := filepath.Join(caseDir, "input_logs.yaml")
	inputMetricsPath := filepath.Join(caseDir, "input_metrics.yaml")
	inputTracesPath := filepath.Join(caseDir, "input_traces.yaml")

	hasLogs := fileExists(inputLogsPath)
	hasMetrics := fileExists(inputMetricsPath)
	hasTraces := fileExists(inputTracesPath)

	if !hasLogs && !hasMetrics && !hasTraces {
		t.Fatalf("test case %q must provide at least one of input_logs.yaml, input_metrics.yaml, or input_traces.yaml", caseName)
	}

	if hasLogs {
		inputLogs := readInputLogs(t, inputLogsPath)
		client := plogotlp.NewGRPCClient(h.conn)
		if _, err := client.Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
			t.Fatalf("failed to export logs to collector: %v\nstderr:\n%s", err, h.col.logs())
		}
	}

	if hasMetrics {
		inputMetrics := readInputMetrics(t, inputMetricsPath)
		client := pmetricotlp.NewGRPCClient(h.conn)
		if _, err := client.Export(ctx, pmetricotlp.NewExportRequestFromMetrics(inputMetrics)); err != nil {
			t.Fatalf("failed to export metrics to collector: %v\nstderr:\n%s", err, h.col.logs())
		}
	}

	if hasTraces {
		inputTraces := readInputTraces(t, inputTracesPath)
		client := ptraceotlp.NewGRPCClient(h.conn)
		if _, err := client.Export(ctx, ptraceotlp.NewExportRequestFromTraces(inputTraces)); err != nil {
			t.Fatalf("failed to export traces to collector: %v\nstderr:\n%s", err, h.col.logs())
		}
	}

	h.col.shutdown(t)
	capturedStderr := h.col.logs()

	if hasLogs {
		gotLogs := normalizeExportedLogs(t, h.mockSrv.CollectedLogs(), testStartTime)
		assertGoldenYAML(t, filepath.Join(caseDir, "expected_logs.yaml"), gotLogs, capturedStderr)
	}

	if hasMetrics {
		gotMetrics := normalizeExportedMetrics(t, h.mockSrv.CollectedMetrics(), testStartTime)
		assertGoldenYAML(t, filepath.Join(caseDir, "expected_metrics.yaml"), gotMetrics, capturedStderr)
	}

	if hasTraces {
		gotTraces := normalizeExportedTraces(t, h.mockSrv.CollectedTraces(), testStartTime)
		assertGoldenYAML(t, filepath.Join(caseDir, "expected_traces.yaml"), gotTraces, capturedStderr)
	}
}

func TestPolicyHotReload(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir := t.TempDir()

	policiesDir := filepath.Join(tempDir, "policies")
	if err := os.MkdirAll(policiesDir, 0755); err != nil {
		t.Fatalf("failed to create policies directory: %v", err)
	}

	// Start with an initial log_filter policy that drops only "drop-initial".
	initialPolicy := `{
  "type": "log_filter",
  "id": "drop-initial-policy",
  "action": "ACTION_DROP",
  "matchers": [
    {
      "target": {
        "record_field": "LOG_RECORD_FIELD_BODY"
      },
      "predicate": {
        "equality": {
          "string_value": "drop-initial"
        }
      }
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(policiesDir, "01-log-filter.json"), []byte(initialPolicy), 0644); err != nil {
		t.Fatalf("failed to write initial policy: %v", err)
	}

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir)
	mockSrv := h.mockSrv
	col := h.col

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := plogotlp.NewGRPCClient(h.conn)

	buildLogBatch := func(bodies ...string) plog.Logs {
		ld := plog.NewLogs()
		rl := ld.ResourceLogs().AppendEmpty()
		rl.Resource().Attributes().PutStr("service.name", "hot-reload-test")
		sl := rl.ScopeLogs().AppendEmpty()
		for _, body := range bodies {
			lr := sl.LogRecords().AppendEmpty()
			lr.SetSeverityText("INFO")
			lr.Body().SetStr(body)
		}
		return ld
	}

	extractBodies := func(batches []plog.Logs) []string {
		var bodies []string
		for _, ld := range batches {
			for i := 0; i < ld.ResourceLogs().Len(); i++ {
				rl := ld.ResourceLogs().At(i)
				if svc, ok := rl.Resource().Attributes().Get("service.name"); !ok || svc.Str() != "hot-reload-test" {
					continue
				}
				for j := 0; j < rl.ScopeLogs().Len(); j++ {
					sl := rl.ScopeLogs().At(j)
					for k := 0; k < sl.LogRecords().Len(); k++ {
						bodies = append(bodies, sl.LogRecords().At(k).Body().Str())
					}
				}
			}
		}
		return bodies
	}

	// Phase 1: "drop-initial" is dropped, while "drop-after-reload" passes through.
	if _, err := client.Export(ctx, plogotlp.NewExportRequestFromLogs(buildLogBatch("drop-initial", "drop-after-reload"))); err != nil {
		t.Fatalf("phase 1 export failed: %v\nlogs:\n%s", err, col.logs())
	}

	phase1Bodies := extractBodies(mockSrv.CollectedLogs())
	if diff := cmp.Diff([]string{"drop-after-reload"}, phase1Bodies); diff != "" {
		t.Fatalf("phase 1 exported bodies mismatch (-want +got):\n%s\nlogs:\n%s", diff, col.logs())
	}

	// Phase 2: Write a new policy file into policiesDir while the collector is running to trigger fsnotify hot-reload.
	hotReloadPolicy := `{
  "type": "log_filter",
  "id": "drop-hot-reload-policy",
  "action": "ACTION_DROP",
  "matchers": [
    {
      "target": {
        "record_field": "LOG_RECORD_FIELD_BODY"
      },
      "predicate": {
        "equality": {
          "string_value": "drop-after-reload"
        }
      }
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(policiesDir, "02-hot-reload-filter.json"), []byte(hotReloadPolicy), 0644); err != nil {
		t.Fatalf("failed to write hot-reload policy file: %v", err)
	}

	// Wait for googlepolicyprocessor to recompile and drop "drop-after-reload".
	reloadDeadline := time.Now().Add(5 * time.Second)
	reloaded := false
	for time.Now().Before(reloadDeadline) {
		mockSrv.mu.Lock()
		mockSrv.logs = nil
		mockSrv.mu.Unlock()

		if _, err := client.Export(ctx, plogotlp.NewExportRequestFromLogs(buildLogBatch("drop-after-reload", "keep-after-reload"))); err != nil {
			t.Fatalf("phase 2 export failed: %v\nlogs:\n%s", err, col.logs())
		}

		phase2Bodies := extractBodies(mockSrv.CollectedLogs())
		if len(phase2Bodies) == 1 && phase2Bodies[0] == "keep-after-reload" {
			reloaded = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	col.shutdown(t)

	if !reloaded {
		t.Fatalf("timed out waiting for fsnotify policy hot-reload to drop 'drop-after-reload'.\ncollector logs:\n%s", col.logs())
	}
}

func TestUnsupportedPolicyEmitsErrorEvent(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir := t.TempDir()

	policiesDir := filepath.Join(tempDir, "policies")
	if err := os.MkdirAll(policiesDir, 0755); err != nil {
		t.Fatalf("failed to create policies directory: %v", err)
	}

	const wantPolicySetID = "projects/my-project/locations/us-central1/policySets/my-policy-set"
	const validPolicyID = wantPolicySetID + "/policies/valid-log-filter-policy"
	const unsupportedPolicy1ID = wantPolicySetID + "/policies/unsupported-policy-1"
	const unsupportedPolicy2ID = wantPolicySetID + "/policies/unsupported-policy-2"

	// Provide a valid log_filter policy alongside an unsupported policy type.
	validPolicy := fmt.Sprintf(`{
  "type": "log_filter",
  "id": %q,
  "action": "ACTION_DROP",
  "matchers": [
    {
      "target": {
        "record_field": "LOG_RECORD_FIELD_BODY"
      },
      "predicate": {
        "equality": {
          "string_value": "drop-me"
        }
      }
    }
  ]
}`, validPolicyID)
	unsupportedPolicy := fmt.Sprintf(`{
  "type": "unsupported_policy_type",
  "id": %q
}`, unsupportedPolicy1ID)
	if err := os.WriteFile(filepath.Join(policiesDir, "01-valid-filter.json"), []byte(validPolicy), 0644); err != nil {
		t.Fatalf("failed to write valid policy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(policiesDir, "02-unsupported.json"), []byte(unsupportedPolicy), 0644); err != nil {
		t.Fatalf("failed to write unsupported policy: %v", err)
	}

	mockSrv, mockExporterAddr := startMockOTLPServer(t)
	ports := allocateEphemeralPorts(t, 2)
	otlpReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[0])
	selfReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[1])

	// Route logs/default_self_metrics from otlp/default_self_metrics (BuiltInSelfMetricsPolicy on selfReceiverAddr)
	// to otlp_grpc/default_gcp_destination (rewired to mockExporterAddr) with a fast 50ms BatchLogRecordProcessor schedule_delay.
	pipelineCfg := fmt.Sprintf(`receivers:
  otlp/test_in:
    protocols:
      grpc:
        endpoint: %[1]s
  otlp/default_self_metrics:
    protocols:
      grpc:
        endpoint: %[3]s
exporters:
  otlp_grpc/test_out:
    endpoint: %[2]s
    sending_queue:
      enabled: false
    tls:
      insecure: true
  otlp_grpc/default_gcp_destination:
    endpoint: %[2]s
    auth:
      enabled: false
    sending_queue:
      enabled: false
    tls:
      insecure: true
service:
  telemetry:
    logs:
      level: info
      processors:
        - batch:
            schedule_delay: 50
            export_timeout: 200
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://%[3]s
                insecure: true
                timeout: 200
    metrics:
      level: none
      readers: []
  pipelines:
    logs/test:
      receivers: [otlp/test_in]
      processors: [googlepolicy]
      exporters: [otlp_grpc/test_out]
    metrics/test:
      receivers: [otlp/test_in]
      processors: [googlepolicy]
      exporters: [otlp_grpc/test_out]
    traces/test:
      receivers: [otlp/test_in]
      processors: [googlepolicy]
      exporters: [otlp_grpc/test_out]
`, otlpReceiverAddr, mockExporterAddr, selfReceiverAddr)

	pipelineCfgPath := filepath.Join(tempDir, "test_pipeline.yaml")
	if err := os.WriteFile(pipelineCfgPath, []byte(pipelineCfg), 0644); err != nil {
		t.Fatalf("failed to write test_pipeline.yaml: %v", err)
	}

	col := startCollector(t, otelcolBin, policiesDir, pipelineCfgPath)

	type capturedEvent struct {
		EventName      string
		PolicyID       string
		PolicySetID    string
		RevisionID     string
		ErrorType      string
		Body           string
		Severity       string
		SeverityNumber plog.SeverityNumber
	}

	findErrorEvents := func(batches []plog.Logs) []capturedEvent {
		var out []capturedEvent
		for _, ld := range batches {
			for i := 0; i < ld.ResourceLogs().Len(); i++ {
				rl := ld.ResourceLogs().At(i)
				for j := 0; j < rl.ScopeLogs().Len(); j++ {
					sl := rl.ScopeLogs().At(j)
					for k := 0; k < sl.LogRecords().Len(); k++ {
						lr := sl.LogRecords().At(k)
						evName, ok := lr.Attributes().Get("event.name")
						if !ok || evName.Str() != "gcp.policy.evaluate.error" {
							continue
						}
						var policyID, policySetID, revID, errType string
						if v, ok := lr.Attributes().Get("gcp.policy.id"); ok {
							policyID = v.Str()
						}
						if v, ok := lr.Attributes().Get("gcp.policy.set.id"); ok {
							policySetID = v.Str()
						}
						if v, ok := lr.Attributes().Get("gcp.policy.set.revision.id"); ok {
							revID = v.Str()
						}
						if v, ok := lr.Attributes().Get("error.type"); ok {
							errType = v.Str()
						}
						out = append(out, capturedEvent{
							EventName:      evName.Str(),
							PolicyID:       policyID,
							PolicySetID:    policySetID,
							RevisionID:     revID,
							ErrorType:      errType,
							Body:           lr.Body().Str(),
							Severity:       lr.SeverityText(),
							SeverityNumber: lr.SeverityNumber(),
						})
					}
				}
			}
		}
		return out
	}

	var gotEvents []capturedEvent
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		gotEvents = findErrorEvents(mockSrv.CollectedLogs())
		if len(gotEvents) >= 1 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if len(gotEvents) != 1 {
		col.shutdown(t)
		t.Fatalf("expected exactly 1 startup gcp.policy.evaluate.error event, got %d (%+v).\ncollector logs:\n%s", len(gotEvents), gotEvents, col.logs())
	}

	startupEvent := gotEvents[0]
	if startupEvent.PolicyID != unsupportedPolicy1ID {
		t.Errorf("gcp.policy.id = %q, want %q", startupEvent.PolicyID, unsupportedPolicy1ID)
	}
	if startupEvent.PolicySetID != wantPolicySetID {
		t.Errorf("gcp.policy.set.id = %q, want %q", startupEvent.PolicySetID, wantPolicySetID)
	}
	if startupEvent.RevisionID == "" {
		t.Errorf("expected non-empty gcp.policy.set.revision.id attribute on gcp.policy.evaluate.error event")
	}
	if startupEvent.ErrorType != "policy_type_not_found" {
		t.Errorf("error.type = %q, want %q", startupEvent.ErrorType, "policy_type_not_found")
	}
	if !strings.Contains(startupEvent.Body, "no driver found for policy type: unsupported_policy_type") {
		t.Errorf("event body = %q, want substring %q", startupEvent.Body, "no driver found for policy type: unsupported_policy_type")
	}
	if !strings.EqualFold(startupEvent.Severity, "ERROR") || startupEvent.SeverityNumber != plog.SeverityNumberError {
		t.Errorf("event severity = %q (%v), want ERROR (%v)", startupEvent.Severity, startupEvent.SeverityNumber, plog.SeverityNumberError)
	}

	// Now hot-reload a second unsupported policy into the directory and verify a new gcp.policy.evaluate.error event is emitted.
	hotReloadUnsupported := fmt.Sprintf(`{
  "type": "another_unsupported_type",
  "id": %q
}`, unsupportedPolicy2ID)
	if err := os.WriteFile(filepath.Join(policiesDir, "03-unsupported-hot-reload.json"), []byte(hotReloadUnsupported), 0644); err != nil {
		col.shutdown(t)
		t.Fatalf("failed to write hot-reloaded unsupported policy: %v", err)
	}

	var hotReloadEvent *capturedEvent
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		gotEvents = findErrorEvents(mockSrv.CollectedLogs())
		for i := range gotEvents {
			if gotEvents[i].PolicyID == unsupportedPolicy2ID {
				hotReloadEvent = &gotEvents[i]
				break
			}
		}
		if hotReloadEvent != nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	col.shutdown(t)

	if hotReloadEvent == nil {
		t.Fatalf("timed out waiting for hot-reloaded gcp.policy.evaluate.error event for %s (got %+v).\ncollector logs:\n%s", unsupportedPolicy2ID, gotEvents, col.logs())
	}
	if hotReloadEvent.PolicySetID != wantPolicySetID {
		t.Errorf("hot-reload gcp.policy.set.id = %q, want %q", hotReloadEvent.PolicySetID, wantPolicySetID)
	}
	if hotReloadEvent.RevisionID == "" || hotReloadEvent.RevisionID == startupEvent.RevisionID {
		t.Errorf("expected new revision ID on hot-reloaded error event, got %q (startup was %q)", hotReloadEvent.RevisionID, startupEvent.RevisionID)
	}
	if hotReloadEvent.ErrorType != "policy_type_not_found" {
		t.Errorf("hot-reload error.type = %q, want %q", hotReloadEvent.ErrorType, "policy_type_not_found")
	}
	if !strings.Contains(hotReloadEvent.Body, "no driver found for policy type: another_unsupported_type") {
		t.Errorf("hot-reload event body = %q, want substring %q", hotReloadEvent.Body, "no driver found for policy type: another_unsupported_type")
	}
}

// TestPolicySetActiveSelfMetric checks that the googlecontrolplane extension
// enabled by the self metrics policy is actually started, and that its gauge
// reaches the self metrics pipeline through the collector's internal telemetry.
func TestPolicySetActiveSelfMetric(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir)

	value, attrs, found := waitForPolicySetActive(h.selfMockSrv, 10*time.Second)

	h.col.shutdown(t)

	if !found {
		t.Fatalf("%s was not reported on the self metrics pipeline.\ncollector logs:\n%s", googlecontrolplaneextension.MetricPolicySetActive, h.col.logs())
	}
	if value <= 0 {
		t.Errorf("%s = %d, want positive integer revision (not -1 or 0)", googlecontrolplaneextension.MetricPolicySetActive, value)
	}
	if id, ok := attrs[googlecontrolplaneextension.AttrPolicySetID]; !ok || id != "" {
		t.Errorf("%s = %q (present=%v), want present and empty; attributes: %v", googlecontrolplaneextension.AttrPolicySetID, id, ok, attrs)
	}
}

// TestPolicySetActiveSelfMetric_NotEmittedWithoutProvider verifies
// that when the collector is started with a file config only (without
// googlecontrolplaneprovider), self-o11y metrics (gcp.policy.set.active)
// are not emitted even when internal collector telemetry metrics are enabled,
// both when googlecontrolplaneextension is omitted and when it is configured
// independently.
func TestPolicySetActiveSelfMetric_NotEmittedWithoutProvider(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)

	tests := []struct {
		name      string
		extension string
		processor string
	}{
		{
			name: "without_extension",
		},
		{
			name:      "with_extension_configured_independently",
			extension: "googlecontrolplane",
		},
		{
			name:      "with_extension_and_processor_configured_independently",
			extension: "googlecontrolplane",
			processor: "googlepolicy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			selfMockSrv, selfExporterAddr := startMockOTLPServer(t)

			cfg := fmt.Sprintf(`receivers:
  nop/in:
processors:
  googlepolicy:
exporters:
  nop/out:
extensions:
  googlecontrolplane:
service:
  extensions: [%s]
  telemetry:
    logs:
      processors: []
    metrics:
      readers:
        - periodic:
            interval: 100
            timeout: 200
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://%s
                insecure: true
  pipelines:
    logs:
      receivers: [nop/in]
      processors: [%s]
      exporters: [nop/out]
`, tc.extension, selfExporterAddr, tc.processor)
			cfgPath := filepath.Join(tempDir, "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
				t.Fatalf("failed to write config.yaml: %v", err)
			}

			col := startCollectorWithURIs(t, otelcolBin, fmt.Sprintf("file:%s", filepath.ToSlash(cfgPath)))

			gotMetrics := waitForMetrics(selfMockSrv, 5*time.Second)

			col.shutdown(t)

			if len(gotMetrics) == 0 {
				t.Fatalf("waitForMetrics() = empty, want standard internal collector metrics\ncollector logs:\n%s", col.logs())
			}
			if count, found := gotMetrics[googlecontrolplaneextension.MetricPolicySetActive]; found {
				t.Errorf("got %d occurrences of %q, want 0 (gotMetrics: %v)\ncollector logs:\n%s", count, googlecontrolplaneextension.MetricPolicySetActive, gotMetrics, col.logs())
			}
		})
	}
}

func stagePoliciesDir(t *testing.T, caseName string) (string, string) {
	t.Helper()
	tempDir := t.TempDir()
	policiesDir := filepath.Join(tempDir, "policies")
	copyDir(t, filepath.Join("testdata", caseName, "policies"), policiesDir)
	return tempDir, policiesDir
}

// waitCondition reports whether fn eventually returned true,
// checking immediately and then every checkEvery amount,
// until waitFor has elapsed, at which point it returns false.
// Adapted from https://github.com/golang/net/blob/v0.49.0/http2/http2_test.go#L115-L127.
func waitCondition(waitFor, checkEvery time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(checkEvery)
	}
	return false
}

func waitForMetrics(srv *mockOTLPServer, timeout time.Duration) map[string]int {
	var got map[string]int
	waitCondition(timeout, 50*time.Millisecond, func() bool {
		got = collectMetricNames(srv.CollectedMetrics())
		return len(got) > 0
	})
	return got
}

func waitForLog(srv *mockOTLPServer, timeout time.Duration, substr string) bool {
	return waitCondition(timeout, 50*time.Millisecond, func() bool {
		for _, body := range extractAllLogBodies(srv.CollectedLogs()) {
			if strings.Contains(body, substr) {
				return true
			}
		}
		return false
	})
}

func waitForPolicySetActive(selfMockSrv *mockOTLPServer, timeout time.Duration) (int64, map[string]string, bool) {
	var (
		value int64
		attrs map[string]string
		found bool
	)
	waitCondition(timeout, 50*time.Millisecond, func() bool {
		value, attrs, found = latestPolicySetActive(selfMockSrv.CollectedMetrics())
		return found
	})
	return value, attrs, found
}

// latestPolicySetActive returns the most recent gcp.policy.set.active data
// point found in the collected batches.
func latestPolicySetActive(batches []pmetric.Metrics) (int64, map[string]string, bool) {
	var (
		value int64
		attrs map[string]string
		found bool
	)
	for _, md := range batches {
		for i := 0; i < md.ResourceMetrics().Len(); i++ {
			sms := md.ResourceMetrics().At(i).ScopeMetrics()
			for j := 0; j < sms.Len(); j++ {
				ms := sms.At(j).Metrics()
				for k := 0; k < ms.Len(); k++ {
					m := ms.At(k)
					if m.Name() != googlecontrolplaneextension.MetricPolicySetActive || m.Type() != pmetric.MetricTypeGauge {
						continue
					}
					dps := m.Gauge().DataPoints()
					for l := 0; l < dps.Len(); l++ {
						dp := dps.At(l)
						value = dp.IntValue()
						attrs = map[string]string{}
						dp.Attributes().Range(func(k string, v pcommon.Value) bool {
							attrs[k] = v.AsString()
							return true
						})
						found = true
					}
				}
			}
		}
	}
	return value, attrs, found
}

func extractAllLogBodies(batches []plog.Logs) []string {
	var bodies []string
	for _, ld := range batches {
		for i := 0; i < ld.ResourceLogs().Len(); i++ {
			rl := ld.ResourceLogs().At(i)
			for j := 0; j < rl.ScopeLogs().Len(); j++ {
				sl := rl.ScopeLogs().At(j)
				for k := 0; k < sl.LogRecords().Len(); k++ {
					bodies = append(bodies, sl.LogRecords().At(k).Body().Str())
				}
			}
		}
	}
	return bodies
}

func collectMetricNames(batches []pmetric.Metrics) map[string]int {
	counts := map[string]int{}
	for _, md := range batches {
		for i := 0; i < md.ResourceMetrics().Len(); i++ {
			sms := md.ResourceMetrics().At(i).ScopeMetrics()
			for j := 0; j < sms.Len(); j++ {
				ms := sms.At(j).Metrics()
				for k := 0; k < ms.Len(); k++ {
					counts[ms.At(k).Name()]++
				}
			}
		}
	}
	return counts
}

func writeIndependentLogsPipelineConfig(t *testing.T, tempDir string) (string, plogotlp.GRPCClient, *mockOTLPServer) {
	t.Helper()
	independentMockSrv, independentExporterAddr := startMockOTLPServer(t)
	ports := allocateEphemeralPorts(t, 1)
	independentReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[0])

	independentCfg := fmt.Sprintf(`receivers:
  otlp/independent_in:
    protocols:
      grpc:
        endpoint: %s
exporters:
  otlp_grpc/independent_out:
    endpoint: %s
    sending_queue:
      enabled: false
    tls:
      insecure: true
service:
  pipelines:
    logs/independent:
      receivers: [otlp/independent_in]
      processors: []
      exporters: [otlp_grpc/independent_out]
`, independentReceiverAddr, independentExporterAddr)

	independentCfgPath := filepath.Join(tempDir, "independent_pipeline.yaml")
	if err := os.WriteFile(independentCfgPath, []byte(independentCfg), 0644); err != nil {
		t.Fatalf("failed to write independent_pipeline.yaml: %v", err)
	}

	indConn, err := grpc.NewClient(independentReceiverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create gRPC client to independent receiver %s: %v", independentReceiverAddr, err)
	}
	t.Cleanup(func() {
		_ = indConn.Close()
	})

	return independentCfgPath, plogotlp.NewGRPCClient(indConn), independentMockSrv
}

// TestFileConfigMerge_AppendReceiverToCanonicalPipeline verifies that an escape hatch
// file config can append a custom receiver (e.g. otlp/custom_in) to the canonical shared
// logs pipeline (logs/googlecontrolplane).
//
// Because googlecontrolplaneprovider enables confmap.enableMergeAppendOption, the
// customer-provided receiver is appended to [nop/googlecontrolplane], so logs sent to
// otlp/custom_in flow through the canonical pipeline: they are processed by
// resourcedetection/googlecontrolplane, transform/googlecontrolplane, and googlepolicy,
// before being exported to otlp_grpc/default_gcp_destination.
func TestFileConfigMerge_AppendReceiverToCanonicalPipeline(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

	ports := allocateEphemeralPorts(t, 1)
	customReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[0])

	appendReceiverCfg := fmt.Sprintf(`receivers:
  otlp/custom_in:
    protocols:
      grpc:
        endpoint: %s
service:
  pipelines:
    logs/googlecontrolplane:
      receivers: [otlp/custom_in]
`, customReceiverAddr)

	appendCfgPath := filepath.Join(tempDir, "append_receiver_canonical.yaml")
	if err := os.WriteFile(appendCfgPath, []byte(appendReceiverCfg), 0644); err != nil {
		t.Fatalf("failed to write append_receiver_canonical.yaml: %v", err)
	}

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir, appendCfgPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(customReceiverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect to custom receiver %s: %v", customReceiverAddr, err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	customClient := plogotlp.NewGRPCClient(conn)
	inputLogs := readInputLogs(t, filepath.Join("testdata", "drop_by_body_equals", "input_logs.yaml"))

	if _, err := customClient.Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
		t.Fatalf("failed to export logs to custom receiver on canonical pipeline: %v\nstderr:\n%s", err, h.col.logs())
	}

	h.col.shutdown(t)

	// In testPipelineConfig, otlp_grpc/default_gcp_destination is rewired to selfMockSrv.
	// Since logs/googlecontrolplane exports to default_gcp_destination, the filtered logs
	// must arrive on selfMockSrv.
	gotBodies := extractAllLogBodies(h.selfMockSrv.CollectedLogs())
	if diff := cmp.Diff([]string{"KEEP_THIS_LOG_LINE"}, gotBodies); diff != "" {
		t.Errorf("canonical pipeline bodies mismatch (-want +got):\n%s\nlogs:\n%s", diff, h.col.logs())
	}
}

// TestFileConfigMerge_AppendExporterToCanonicalPipeline verifies that an escape hatch
// file config can append a secondary exporter to the canonical shared logs pipeline
// (logs/googlecontrolplane).
//
// Because googlecontrolplaneprovider enables confmap.enableMergeAppendOption, the
// secondary exporter is appended to [otlp_grpc/default_gcp_destination], causing
// logs processed by the canonical pipeline (including googlepolicy filtering) to fan
// out to BOTH destinations.
func TestFileConfigMerge_AppendExporterToCanonicalPipeline(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

	secondaryMockSrv, secondaryExporterAddr := startMockOTLPServer(t)
	ports := allocateEphemeralPorts(t, 1)
	customReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[0])

	appendCfg := fmt.Sprintf(`receivers:
  otlp/custom_in:
    protocols:
      grpc:
        endpoint: %[1]s
exporters:
  otlp_grpc/secondary_out:
    endpoint: %[2]s
    sending_queue:
      enabled: false
    tls:
      insecure: true
service:
  pipelines:
    logs/googlecontrolplane:
      receivers: [otlp/custom_in]
      exporters: [otlp_grpc/secondary_out]
`, customReceiverAddr, secondaryExporterAddr)

	appendCfgPath := filepath.Join(tempDir, "append_canonical_pipeline.yaml")
	if err := os.WriteFile(appendCfgPath, []byte(appendCfg), 0644); err != nil {
		t.Fatalf("failed to write append_canonical_pipeline.yaml: %v", err)
	}

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir, appendCfgPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(customReceiverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect to custom receiver %s: %v", customReceiverAddr, err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	customClient := plogotlp.NewGRPCClient(conn)
	inputLogs := readInputLogs(t, filepath.Join("testdata", "drop_by_body_equals", "input_logs.yaml"))

	if _, err := customClient.Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
		t.Fatalf("failed to export logs to custom receiver: %v\nstderr:\n%s", err, h.col.logs())
	}

	h.col.shutdown(t)

	// Primary destination (selfMockSrv via otlp_grpc/default_gcp_destination) received filtered logs.
	gotPrimaryBodies := extractAllLogBodies(h.selfMockSrv.CollectedLogs())
	if diff := cmp.Diff([]string{"KEEP_THIS_LOG_LINE"}, gotPrimaryBodies); diff != "" {
		t.Errorf("primary exporter bodies mismatch (-want +got):\n%s\nlogs:\n%s", diff, h.col.logs())
	}

	// Appended secondary destination (secondaryMockSrv via otlp_grpc/secondary_out) also received filtered logs.
	gotSecondaryBodies := extractAllLogBodies(secondaryMockSrv.CollectedLogs())
	if diff := cmp.Diff([]string{"KEEP_THIS_LOG_LINE"}, gotSecondaryBodies); diff != "" {
		t.Errorf("secondary exporter bodies mismatch (-want +got):\n%s\nlogs:\n%s", diff, h.col.logs())
	}
}

// TestFileConfigMerge_IndependentPipeline verifies that when the collector is
// given an additional file config defining a pipeline completely independent of
// policy work (no googlepolicy processor), confmap deep-merges the receivers,
// exporters, and service.pipelines maps so both pipelines run side-by-side:
// the policy-managed pipeline filters out matching records, while the
// independent pipeline exports all records untouched.
func TestFileConfigMerge_IndependentPipeline(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

	independentCfgPath, independentClient, independentMockSrv := writeIndependentLogsPipelineConfig(t, tempDir)

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir, independentCfgPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	inputLogs := readInputLogs(t, filepath.Join("testdata", "drop_by_body_equals", "input_logs.yaml"))

	// Export the same log batch to both the policy-managed receiver and the independent receiver.
	policyClient := plogotlp.NewGRPCClient(h.conn)
	if _, err := policyClient.Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
		t.Fatalf("failed to export logs to policy-managed pipeline: %v\nstderr:\n%s", err, h.col.logs())
	}

	if _, err := independentClient.Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
		t.Fatalf("failed to export logs to independent pipeline: %v\nstderr:\n%s", err, h.col.logs())
	}

	h.col.shutdown(t)

	// Policy-managed pipeline drops "DROP_THIS_LOG_LINE" and retains only "KEEP_THIS_LOG_LINE".
	gotPolicyBodies := extractAllLogBodies(h.mockSrv.CollectedLogs())
	if diff := cmp.Diff([]string{"KEEP_THIS_LOG_LINE"}, gotPolicyBodies); diff != "" {
		t.Errorf("policy-managed pipeline bodies mismatch (-want +got):\n%s\nlogs:\n%s", diff, h.col.logs())
	}

	// Independent file-configured pipeline bypasses googlepolicy and retains both log records.
	gotIndependentBodies := extractAllLogBodies(independentMockSrv.CollectedLogs())
	if diff := cmp.Diff([]string{"KEEP_THIS_LOG_LINE", "DROP_THIS_LOG_LINE"}, gotIndependentBodies); diff != "" {
		t.Errorf("independent pipeline bodies mismatch (-want +got):\n%s\nlogs:\n%s", diff, h.col.logs())
	}
}

// TestFileConfigMerge_DisableSelfMetrics verifies that a subsequent file config
// can intertwine with the config generated by googlecontrolplaneprovider and
// disable self-metrics by overwriting service.telemetry.metrics (setting
// level: None and readers: []), while leaving policy evaluation on customer
// pipelines active.
func TestFileConfigMerge_DisableSelfMetrics(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

	disableSelfMetricsCfg := `service:
  telemetry:
    metrics:
      level: None
      readers: []
`
	disableCfgPath := filepath.Join(tempDir, "disable_self_metrics.yaml")
	if err := os.WriteFile(disableCfgPath, []byte(disableSelfMetricsCfg), 0644); err != nil {
		t.Fatalf("failed to write disable_self_metrics.yaml: %v", err)
	}

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir, disableCfgPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send logs through the policy-managed pipeline to verify googlepolicy still
	// filters telemetry and that no internal processor/receiver metrics leak out.
	inputLogs := readInputLogs(t, filepath.Join("testdata", "drop_by_body_equals", "input_logs.yaml"))
	client := plogotlp.NewGRPCClient(h.conn)
	if _, err := client.Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
		t.Fatalf("failed to export logs to collector: %v\nstderr:\n%s", err, h.col.logs())
	}

	// Wait across multiple 100ms periodic reader intervals to ensure no self-metrics are pushed.
	time.Sleep(300 * time.Millisecond)

	h.col.shutdown(t)

	gotBodies := extractAllLogBodies(h.mockSrv.CollectedLogs())
	if diff := cmp.Diff([]string{"KEEP_THIS_LOG_LINE"}, gotBodies); diff != "" {
		t.Errorf("policy-managed pipeline bodies mismatch (-want +got):\n%s\nlogs:\n%s", diff, h.col.logs())
	}

	if gotMetrics := collectMetricNames(h.selfMockSrv.CollectedMetrics()); len(gotMetrics) != 0 {
		t.Errorf("expected 0 self-metrics when disabled by file config override, got: %v", gotMetrics)
	}
	if _, _, found := latestPolicySetActive(h.selfMockSrv.CollectedMetrics()); found {
		t.Errorf("expected gcp.policy.set.active to not be reported when self-metrics are disabled")
	}
}

// TestFileConfigMerge_SelectiveSelfMetricsAndExtensionOverrides verifies finer-grained
// intertwining behaviors between googlecontrolplaneprovider and file configs:
//  1. Disabling service.telemetry.metrics (level: None, readers: []) while
//     keeping service.telemetry.logs enabled suppresses self-metrics while
//     preserving logs/default_self_metrics (so Weaver error events like
//     gcp.policy.evaluate.error still flow).
//  2. Because googlecontrolplaneprovider enables confmap.enableMergeAppendOption,
//     a subsequent file config that sets service.extensions or pipeline exporters
//     appends to (rather than overwrites) the existing slices:
//     - service.extensions retains [googleclientauth/default_gcp_destination, googlecontrolplane]
//     even when a subsequent file config sets service.extensions: [], so
//     gcp.policy.set.active = 1 continues to be reported.
//     - service.pipelines.logs/test.exporters appends [otlp_grpc/secondary_out]
//     to [otlp_grpc/test_out], fanning out filtered logs to both exporters.
func TestFileConfigMerge_SelectiveSelfMetricsAndExtensionOverrides(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)

	t.Run("disable_telemetry_metrics_keep_self_logs", func(t *testing.T) {
		tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

		unsupportedPolicy := `{
  "type": "unsupported_policy_type",
  "id": "projects/my-project/locations/us-central1/policySets/my-set/policies/bad-policy"
}`
		if err := os.WriteFile(filepath.Join(policiesDir, "02-unsupported.json"), []byte(unsupportedPolicy), 0644); err != nil {
			t.Fatalf("failed to write unsupported policy: %v", err)
		}

		baseCfgPath, _, selfReceiverAddr, _, selfMockSrv := stageTestPipelineConfig(t, tempDir)

		// Layer a second file config that re-enables the OTLP log processor to
		// selfReceiverAddr while disabling service.telemetry.metrics.
		overrideCfg := fmt.Sprintf(`service:
  telemetry:
    logs:
      level: info
      processors:
        - batch:
            schedule_delay: 50
            export_timeout: 200
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://%s
                insecure: true
                timeout: 200
    metrics:
      level: None
      readers: []
`, selfReceiverAddr)
		overrideCfgPath := filepath.Join(tempDir, "override_self_metrics_pipeline.yaml")
		if err := os.WriteFile(overrideCfgPath, []byte(overrideCfg), 0644); err != nil {
			t.Fatalf("failed to write override_self_metrics_pipeline.yaml: %v", err)
		}

		col := startCollector(t, otelcolBin, policiesDir, baseCfgPath, overrideCfgPath)

		// Wait for the startup gcp.policy.evaluate.error log event to arrive on selfMockSrv.
		var sawErrorEvent bool
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			for _, body := range extractAllLogBodies(selfMockSrv.CollectedLogs()) {
				if strings.Contains(body, "no driver found for policy type: unsupported_policy_type") {
					sawErrorEvent = true
					break
				}
			}
			if sawErrorEvent {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}

		// Wait across multiple 100ms periodic reader intervals to verify metrics stay suppressed.
		time.Sleep(250 * time.Millisecond)
		col.shutdown(t)

		if !sawErrorEvent {
			t.Fatalf("expected gcp.policy.evaluate.error log event on selfMockSrv.\ncollector logs:\n%s", col.logs())
		}
		if gotMetrics := collectMetricNames(selfMockSrv.CollectedMetrics()); len(gotMetrics) != 0 {
			t.Errorf("expected 0 metrics on selfMockSrv when service.telemetry.metrics is disabled, got: %v", gotMetrics)
		}
	})

	t.Run("append_pipeline_exporters_and_preserve_service_extensions", func(t *testing.T) {
		tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

		secondaryMockSrv, secondaryExporterAddr := startMockOTLPServer(t)

		// Because confmap.enableMergeAppendOption is enabled by googlecontrolplaneprovider,
		// setting service.extensions: [] and logs/test.exporters: [otlp_grpc/secondary_out]
		// appends to the existing slices rather than overwriting them.
		appendSliceCfg := fmt.Sprintf(`exporters:
  otlp_grpc/secondary_out:
    endpoint: %s
    sending_queue:
      enabled: false
    tls:
      insecure: true
service:
  extensions: []
  pipelines:
    logs/test:
      exporters: [otlp_grpc/secondary_out]
`, secondaryExporterAddr)
		appendSlicePath := filepath.Join(tempDir, "append_slice_override.yaml")
		if err := os.WriteFile(appendSlicePath, []byte(appendSliceCfg), 0644); err != nil {
			t.Fatalf("failed to write append_slice_override.yaml: %v", err)
		}

		h := startTestHarness(t, otelcolBin, tempDir, policiesDir, appendSlicePath)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		inputLogs := readInputLogs(t, filepath.Join("testdata", "drop_by_body_equals", "input_logs.yaml"))
		client := plogotlp.NewGRPCClient(h.conn)
		if _, err := client.Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
			t.Fatalf("failed to export logs: %v\nstderr:\n%s", err, h.col.logs())
		}

		// Verify googlecontrolplane extension remained enabled and reported positive gcp.policy.set.active revision.
		value, _, found := waitForPolicySetActive(h.selfMockSrv, 10*time.Second)

		h.col.shutdown(t)

		if !found || value <= 0 {
			t.Errorf("expected googlecontrolplane extension to remain enabled under confmap.enableMergeAppendOption and report positive gcp.policy.set.active revision, got found=%v value=%d\nlogs:\n%s", found, value, h.col.logs())
		}

		// Verify logs/test fanned out filtered logs to BOTH otlp_grpc/test_out and otlp_grpc/secondary_out.
		if diff := cmp.Diff([]string{"KEEP_THIS_LOG_LINE"}, extractAllLogBodies(h.mockSrv.CollectedLogs())); diff != "" {
			t.Errorf("primary exporter bodies mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"KEEP_THIS_LOG_LINE"}, extractAllLogBodies(secondaryMockSrv.CollectedLogs())); diff != "" {
			t.Errorf("secondary appended exporter bodies mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestFileConfigMerge_ProviderOrderPrecedence verifies how URI ordering affects
// merging when a file config is specified BEFORE googlecontrolplane:file:...
//
// Because confmap.Resolver merges providers left-to-right:
//   - Non-conflicting map entries in the earlier file config (such as an
//     independent pipeline) are preserved.
//   - Conflicting keys in the earlier file config (such as attempting to set
//     service.telemetry.metrics.level: None) are overwritten when
//     googlecontrolplaneprovider subsequently merges BuiltInSelfMetricsPolicy
//     (which sets service.telemetry.metrics.level: Normal).
func TestFileConfigMerge_ProviderOrderPrecedence(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

	independentCfgPath, indClient, independentMockSrv := writeIndependentLogsPipelineConfig(t, tempDir)

	// Earlier file config (evaluated BEFORE googlecontrolplane): attempts to
	// disable internal metrics via service.telemetry.metrics.level: None.
	prePolicyDisableCfg := `service:
  telemetry:
    metrics:
      level: None
`
	prePolicyCfgPath := filepath.Join(tempDir, "01_pre_policy_disable_metrics.yaml")
	if err := os.WriteFile(prePolicyCfgPath, []byte(prePolicyDisableCfg), 0644); err != nil {
		t.Fatalf("failed to write 01_pre_policy_disable_metrics.yaml: %v", err)
	}

	// Post-policy endpoint rewiring config: rewires endpoints for test servers
	// without setting service.telemetry.metrics.level, so whatever level won
	// between 01_pre_policy_disable_metrics.yaml and googlecontrolplaneprovider
	// remains in effect.
	rewireCfgPath, _, _, _, selfMockSrv := stageTestPipelineConfig(t, tempDir)

	col := startCollectorWithURIs(
		t,
		otelcolBin,
		fmt.Sprintf("file:%s", filepath.ToSlash(independentCfgPath)),
		fmt.Sprintf("file:%s", filepath.ToSlash(prePolicyCfgPath)),
		fmt.Sprintf("googlecontrolplane:file:%s", filepath.ToSlash(policiesDir)),
		fmt.Sprintf("file:%s", filepath.ToSlash(rewireCfgPath)),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verify the independent pipeline from the earlier file config survived the merge.
	inputLogs := readInputLogs(t, filepath.Join("testdata", "drop_by_body_equals", "input_logs.yaml"))
	if _, err := indClient.Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
		t.Fatalf("failed to export logs to independent pipeline: %v\nstderr:\n%s", err, col.logs())
	}

	// Verify that googlecontrolplaneprovider (evaluated after 01_pre_policy_disable_metrics.yaml)
	// overwrote metrics.level: None back to Normal, so gcp.policy.set.active is reported.
	value, _, found := waitForPolicySetActive(selfMockSrv, 10*time.Second)

	col.shutdown(t)

	gotIndependentBodies := extractAllLogBodies(independentMockSrv.CollectedLogs())
	if diff := cmp.Diff([]string{"KEEP_THIS_LOG_LINE", "DROP_THIS_LOG_LINE"}, gotIndependentBodies); diff != "" {
		t.Errorf("independent pipeline bodies mismatch (-want +got):\n%s\nlogs:\n%s", diff, col.logs())
	}
	if !found || value <= 0 {
		t.Errorf("expected googlecontrolplaneprovider to override earlier file config's level: None and report positive gcp.policy.set.active revision, got found=%v value=%d\nlogs:\n%s", found, value, col.logs())
	}
}

// TestPolicyHostMetricsSource verifies that multiple valid HostMetricsSourcePolicy instances
// are loaded by googlecontrolplaneprovider, consolidate cleanly into a single receiver without
// evaluation errors, and result in an active policy set.
func TestPolicyHostMetricsSource(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir := t.TempDir()

	policiesDir := filepath.Join(tempDir, "policies")
	if err := os.MkdirAll(policiesDir, 0755); err != nil {
		t.Fatalf("failed to create policies directory: %v", err)
	}

	const wantPolicySetID = "projects/my-project/locations/us-central1/policySets/ps-1"
	const p1ID = wantPolicySetID + "/policies/hm-30s"
	const p2ID = wantPolicySetID + "/policies/hm-15s"

	// Two policies with different intervals: 30s and 15s.
	// Minimum effective interval is 15s.
	p1 := fmt.Sprintf(`{
  "type": "host_metrics_source",
  "id": %q,
  "collection_interval_seconds": 30
}`, p1ID)
	p2 := fmt.Sprintf(`{
  "type": "host_metrics_source",
  "id": %q,
  "collection_interval_seconds": 15
}`, p2ID)
	if err := os.WriteFile(filepath.Join(policiesDir, "01-hm-30s.json"), []byte(p1), 0644); err != nil {
		t.Fatalf("failed to write 01-hm-30s.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(policiesDir, "02-hm-15s.json"), []byte(p2), 0644); err != nil {
		t.Fatalf("failed to write 02-hm-15s.json: %v", err)
	}

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir)

	// Wait for googlecontrolplane extension to report positive gcp.policy.set.active revision.
	value, _, found := waitForPolicySetActive(h.selfMockSrv, 10*time.Second)

	// Scan selfMockSrv logs to ensure no evaluation error was emitted for either policy.
	var evaluateErrors []string
	for _, batch := range h.selfMockSrv.CollectedLogs() {
		for i := 0; i < batch.ResourceLogs().Len(); i++ {
			sl := batch.ResourceLogs().At(i).ScopeLogs()
			for j := 0; j < sl.Len(); j++ {
				lr := sl.At(j).LogRecords()
				for k := 0; k < lr.Len(); k++ {
					record := lr.At(k)
					attrs := record.Attributes()
					eventName, hasName := attrs.Get("event.name")
					if hasName && eventName.AsString() == "gcp.policy.evaluate.error" {
						policyID, _ := attrs.Get("gcp.policy.id")
						evaluateErrors = append(evaluateErrors, policyID.AsString())
					}
				}
			}
		}
	}

	h.col.shutdown(t)

	if !found || value <= 0 {
		t.Errorf("expected googlecontrolplane extension to report positive gcp.policy.set.active revision, got found=%v value=%d\nlogs:\n%s", found, value, h.col.logs())
	}
	if len(evaluateErrors) > 0 {
		t.Errorf("expected zero gcp.policy.evaluate.error events, got errors for policies: %v\nlogs:\n%s", evaluateErrors, h.col.logs())
	}
}

// TestPolicyHostMetricsSource_FaultIsolation verifies that if an invalid HostMetricsSourcePolicy
// (interval < 10) is deployed alongside a valid one, the invalid policy emits a gcp.policy.evaluate.error
// event, while the valid policy compiles cleanly without error events and the policy set is applied.
func TestPolicyHostMetricsSource_FaultIsolation(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir := t.TempDir()

	policiesDir := filepath.Join(tempDir, "policies")
	if err := os.MkdirAll(policiesDir, 0755); err != nil {
		t.Fatalf("failed to create policies directory: %v", err)
	}

	const wantPolicySetID = "projects/my-project/locations/us-central1/policySets/my-policy-set"
	const validPolicyID = wantPolicySetID + "/policies/valid-hm"
	const invalidPolicyID = wantPolicySetID + "/policies/invalid-hm"

	validPolicy := fmt.Sprintf(`{
  "type": "host_metrics_source",
  "id": %q,
  "collection_interval_seconds": 25
}`, validPolicyID)
	invalidPolicy := fmt.Sprintf(`{
  "type": "host_metrics_source",
  "id": %q,
  "collection_interval_seconds": 5
}`, invalidPolicyID)

	if err := os.WriteFile(filepath.Join(policiesDir, "01-invalid-hm.json"), []byte(invalidPolicy), 0644); err != nil {
		t.Fatalf("failed to write invalid policy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(policiesDir, "02-valid-hm.json"), []byte(validPolicy), 0644); err != nil {
		t.Fatalf("failed to write valid policy: %v", err)
	}

	_, mockExporterAddr := startMockOTLPServer(t)
	selfMockSrv, selfExporterAddr := startMockOTLPServer(t)
	ports := allocateEphemeralPorts(t, 2)
	otlpReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[0])
	selfReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[1])

	pipelineCfg := fmt.Sprintf(`receivers:
  otlp/test_in:
    protocols:
      grpc:
        endpoint: %[1]s
  otlp/default_self_metrics:
    protocols:
      grpc:
        endpoint: %[3]s
exporters:
  otlp_grpc/test_out:
    endpoint: %[2]s
    sending_queue:
      enabled: false
    tls:
      insecure: true
  otlp_grpc/default_gcp_destination:
    endpoint: %[4]s
    auth:
      enabled: false
    sending_queue:
      enabled: false
    tls:
      insecure: true
service:
  telemetry:
    logs:
      processors:
        - batch:
            schedule_delay: 50
            export_timeout: 200
            exporter:
              otlp:
                endpoint: http://%[3]s
                insecure: true
                protocol: grpc
                timeout: 200
    metrics:
      readers:
        - periodic:
            interval: 100
            timeout: 200
            exporter:
              otlp:
                protocol: grpc
                endpoint: http://%[3]s
                insecure: true
`, otlpReceiverAddr, mockExporterAddr, selfReceiverAddr, selfExporterAddr)
	pipelineCfgPath := filepath.Join(tempDir, "test_pipeline.yaml")
	if err := os.WriteFile(pipelineCfgPath, []byte(pipelineCfg), 0644); err != nil {
		t.Fatalf("failed to write test_pipeline.yaml: %v", err)
	}

	col := startCollector(t, otelcolBin, policiesDir, pipelineCfgPath)

	var sawErrorEventForInvalid bool
	var sawErrorEventForValid bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, batch := range selfMockSrv.CollectedLogs() {
			for i := 0; i < batch.ResourceLogs().Len(); i++ {
				sl := batch.ResourceLogs().At(i).ScopeLogs()
				for j := 0; j < sl.Len(); j++ {
					lr := sl.At(j).LogRecords()
					for k := 0; k < lr.Len(); k++ {
						record := lr.At(k)
						attrs := record.Attributes()
						eventName, hasName := attrs.Get("event.name")
						policyID, hasPolicyID := attrs.Get("gcp.policy.id")
						if hasName && eventName.AsString() == "gcp.policy.evaluate.error" && hasPolicyID {
							if policyID.AsString() == invalidPolicyID {
								sawErrorEventForInvalid = true
							}
							if policyID.AsString() == validPolicyID {
								sawErrorEventForValid = true
							}
						}
					}
				}
			}
		}
		if sawErrorEventForInvalid {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	col.shutdown(t)

	if !sawErrorEventForInvalid {
		t.Fatalf("expected gcp.policy.evaluate.error log event on selfMockSrv for invalid policy.\ncollector logs:\n%s", col.logs())
	}
	if sawErrorEventForValid {
		t.Fatalf("unexpected gcp.policy.evaluate.error log event on selfMockSrv for valid policy %q", validPolicyID)
	}
}

func TestPolicyIntegration_IISSourcePolicy(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir := t.TempDir()

	policiesDir := filepath.Join(tempDir, "policies")
	if err := os.MkdirAll(policiesDir, 0755); err != nil {
		t.Fatalf("failed to create policies directory: %v", err)
	}

	iisPolicy := `{
  "type": "iis_source",
  "id": "enable-iis-metrics",
  "collection_interval_seconds": 30
}`
	if err := os.WriteFile(filepath.Join(policiesDir, "01-iis.json"), []byte(iisPolicy), 0644); err != nil {
		t.Fatalf("failed to write iis policy: %v", err)
	}

	pipelineCfgPath, _, _, _, selfMockSrv := stageTestPipelineConfig(t, tempDir)

	col := startCollector(t, otelcolBin, policiesDir, pipelineCfgPath)
	defer col.shutdown(t)

	// Wait for policy set active self metric to verify collector booted and applied policy set
	value, _, found := waitForPolicySetActive(selfMockSrv, 10*time.Second)
	if !found || value <= 0 {
		t.Errorf("expected collector to report positive gcp.policy.set.active revision, got found=%v value=%d\nlogs:\n%s", found, value, col.logs())
	}

	// Verify no gcp.policy.evaluate.error was emitted for the policy
	for _, batch := range selfMockSrv.CollectedLogs() {
		for i := 0; i < batch.ResourceLogs().Len(); i++ {
			sl := batch.ResourceLogs().At(i).ScopeLogs()
			for j := 0; j < sl.Len(); j++ {
				lr := sl.At(j).LogRecords()
				for k := 0; k < lr.Len(); k++ {
					record := lr.At(k)
					attrs := record.Attributes()
					eventName, hasName := attrs.Get("event.name")
					if hasName && eventName.AsString() == "gcp.policy.evaluate.error" {
						policyID, _ := attrs.Get("gcp.policy.id")
						t.Errorf("unexpected gcp.policy.evaluate.error event for policy %s\nlogs:\n%s", policyID.AsString(), col.logs())
					}
				}
			}
		}
	}
}

// TestSelfObservabilityBypassesTelemetryPolicies verifies that customer log_filter
// and metric_filter policies (even catch-all ACTION_DROP rules) do not drop collector
// self-observability logs (e.g. gcp.policy.evaluate.error) or metrics (e.g.
// gcp.policy.set.active and otelcol_* metrics) on logs/default_self_metrics and
// metrics/default_self_metrics.
func TestSelfObservabilityBypassesTelemetryPolicies(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_all_logs")
	copyDir(t, filepath.Join("testdata", "drop_all_metrics", "policies"), policiesDir)
	copyDir(t, filepath.Join("testdata", "unsupported_policy", "policies"), policiesDir)

	h := startTestHarnessWithSelfLogs(t, otelcolBin, tempDir, policiesDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	inputLogs := readInputLogs(t, filepath.Join("testdata", "drop_all_logs", "input_logs.yaml"))
	if _, err := plogotlp.NewGRPCClient(h.conn).Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
		h.col.shutdown(t)
		t.Fatalf("failed to export workload logs: %v\nstderr:\n%s", err, h.col.logs())
	}

	inputMetrics := readInputMetrics(t, filepath.Join("testdata", "drop_all_metrics", "input_metrics.yaml"))
	if _, err := pmetricotlp.NewGRPCClient(h.conn).Export(ctx, pmetricotlp.NewExportRequestFromMetrics(inputMetrics)); err != nil {
		h.col.shutdown(t)
		t.Fatalf("failed to export workload metrics: %v\nstderr:\n%s", err, h.col.logs())
	}

	value, _, foundMetric := waitForPolicySetActive(h.selfMockSrv, 10*time.Second)
	foundErrorEvent := waitForLog(h.selfMockSrv, 5*time.Second, "no driver found for policy type: unsupported_policy_type")

	h.col.shutdown(t)

	if !foundMetric || value <= 0 {
		t.Errorf("expected gcp.policy.set.active self-metric with positive revision on selfMockSrv, got found=%v value=%d\nlogs:\n%s", foundMetric, value, h.col.logs())
	}
	if !foundErrorEvent {
		t.Errorf("expected gcp.policy.evaluate.error self-log event on selfMockSrv, but it was not received\nlogs:\n%s", h.col.logs())
	}
	if gotWorkloadLogs := extractAllLogBodies(h.mockSrv.CollectedLogs()); len(gotWorkloadLogs) != 0 {
		t.Errorf("expected catch-all log_filter policy to drop workload logs, got: %v", gotWorkloadLogs)
	}
	if gotWorkloadMetrics := collectMetricNames(h.mockSrv.CollectedMetrics()); len(gotWorkloadMetrics) != 0 {
		t.Errorf("expected catch-all metric_filter policy to drop workload metrics, got: %v", gotWorkloadMetrics)
	}
}

func TestFileLogSourcePolicy_Integration(t *testing.T) {
	otelcolBin := resolveOtelcolBinary(t)
	tempDir := t.TempDir()

	logsDir := filepath.Join(tempDir, "logs")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create logs directory: %v", err)
	}

	logFilePath := filepath.Join(logsDir, "app.log")

	policiesDir := filepath.Join(tempDir, "policies")
	if err := os.MkdirAll(policiesDir, 0755); err != nil {
		t.Fatalf("failed to create policies directory: %v", err)
	}

	policyContent := fmt.Sprintf(`{
  "type": "file_log_source",
  "id": "app-filelog-source",
  "included_paths": [%q]
}`, filepath.ToSlash(logFilePath))
	if err := os.WriteFile(filepath.Join(policiesDir, "01-filelog.json"), []byte(policyContent), 0644); err != nil {
		t.Fatalf("failed to write filelog source policy: %v", err)
	}

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir)

	// Allow fileconsumer to discover and start watching the file.
	time.Sleep(500 * time.Millisecond)

	// Append a log line to logFilePath now that the collector is running and watching.
	const testLogMessage = "integration test log message from filelog source"
	writeLogLine := func() {
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			t.Fatalf("failed to open log file %s: %v", logFilePath, err)
		}
		if _, err := f.WriteString(testLogMessage + "\n"); err != nil {
			_ = f.Close()
			t.Fatalf("failed to write to log file: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("failed to close log file: %v", err)
		}
	}
	writeLogLine()

	// Wait for the filelog receiver to poll, process, and export to selfMockSrv (otlp_grpc/default_gcp_destination).
	deadline := time.Now().Add(10 * time.Second)
	var capturedBodies []string
	var foundFilePath string
	for time.Now().Before(deadline) {
		logs := h.selfMockSrv.CollectedLogs()
		for _, ld := range logs {
			for i := 0; i < ld.ResourceLogs().Len(); i++ {
				rl := ld.ResourceLogs().At(i)
				for j := 0; j < rl.ScopeLogs().Len(); j++ {
					sl := rl.ScopeLogs().At(j)
					for k := 0; k < sl.LogRecords().Len(); k++ {
						lr := sl.LogRecords().At(k)
						capturedBodies = append(capturedBodies, lr.Body().Str())
						if fp, ok := lr.Attributes().Get("log.file.path"); ok {
							foundFilePath = fp.Str()
						}
					}
				}
			}
		}
		if slices.Contains(capturedBodies, testLogMessage) {
			break
		}
		capturedBodies = nil
		foundFilePath = ""
		// Re-append in case the initial write occurred before the fileconsumer watcher attached.
		writeLogLine()
		time.Sleep(200 * time.Millisecond)
	}

	h.col.shutdown(t)

	if !slices.Contains(capturedBodies, testLogMessage) {
		t.Fatalf("expected log %q not received.\ncollector logs:\n%s", testLogMessage, h.col.logs())
	}

	// Verify log.file.path attribute was added because include_file_path is enabled by default.
	if filepath.Clean(foundFilePath) != filepath.Clean(logFilePath) {
		t.Errorf("expected log.file.path attribute %q, got %q", logFilePath, foundFilePath)
	}
}

// TestPolicy_SecOpsPayloadCategorization_EnvResourceDetection verifies that setting
// OTEL_RESOURCE_ATTRIBUTES dynamically attaches resource attributes to payloads
// flowing through the canonical pipeline (logs/googlecontrolplane), enabling downstream
// backends (such as SecOps) to categorize and route payloads based on resource metadata.
func TestPolicy_SecOpsPayloadCategorization_EnvResourceDetection(t *testing.T) {
	// Set the environment variable used by SecOps to categorize and route payloads
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "routing.target=secops,secops.category=audit_events,deployment.environment=production")

	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

	ports := allocateEphemeralPorts(t, 1)
	customReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[0])

	appendReceiverCfg := fmt.Sprintf(`receivers:
  otlp/secops_in:
    protocols:
      grpc:
        endpoint: %s
service:
  pipelines:
    logs/googlecontrolplane:
      receivers: [otlp/secops_in]
    metrics/googlecontrolplane:
      receivers: [otlp/secops_in]
`, customReceiverAddr)

	appendCfgPath := filepath.Join(tempDir, "append_secops_receiver.yaml")
	if err := os.WriteFile(appendCfgPath, []byte(appendReceiverCfg), 0644); err != nil {
		t.Fatalf("failed to write append_secops_receiver.yaml: %v", err)
	}

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir, appendCfgPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(customReceiverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect to custom receiver %s: %v", customReceiverAddr, err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	// 1. Export log payload to custom receiver on canonical pipeline
	customLogsClient := plogotlp.NewGRPCClient(conn)
	logs := plog.NewLogs()
	rl := logs.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("host.id", "vm-secops-01")
	sl := rl.ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr("KEEP_THIS_LOG_LINE")

	if _, err := customLogsClient.Export(ctx, plogotlp.NewExportRequestFromLogs(logs)); err != nil {
		t.Fatalf("failed to export logs to custom receiver on canonical pipeline: %v\nstderr:\n%s", err, h.col.logs())
	}

	// 2. Export metric payload to custom receiver on canonical pipeline
	customMetricsClient := pmetricotlp.NewGRPCClient(conn)
	metrics := pmetric.NewMetrics()
	rm := metrics.ResourceMetrics().AppendEmpty()
	rm.Resource().Attributes().PutStr("service.name", "secops-workload")
	sm := rm.ScopeMetrics().AppendEmpty()
	m := sm.Metrics().AppendEmpty()
	m.SetName("secops.threat.events")
	dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.SetIntValue(1)
	dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	if _, err := customMetricsClient.Export(ctx, pmetricotlp.NewExportRequestFromMetrics(metrics)); err != nil {
		t.Fatalf("failed to export metrics to custom receiver on canonical pipeline: %v\nstderr:\n%s", err, h.col.logs())
	}

	// Wait for exported logs to arrive on selfMockSrv (otlp_grpc/default_gcp_destination).
	deadline := time.Now().Add(10 * time.Second)
	var foundLogResourceAttrs map[string]string
	var foundMetricResourceAttrs map[string]string

	for time.Now().Before(deadline) {
		if foundLogResourceAttrs == nil {
			collected := h.selfMockSrv.CollectedLogs()
			for _, ld := range collected {
				for i := 0; i < ld.ResourceLogs().Len(); i++ {
					r := ld.ResourceLogs().At(i)
					for j := 0; j < r.ScopeLogs().Len(); j++ {
						for k := 0; k < r.ScopeLogs().At(j).LogRecords().Len(); k++ {
							rec := r.ScopeLogs().At(j).LogRecords().At(k)
							if rec.Body().Str() == "KEEP_THIS_LOG_LINE" {
								attrs := make(map[string]string)
								r.Resource().Attributes().Range(func(key string, val pcommon.Value) bool {
									attrs[key] = val.AsString()
									return true
								})
								foundLogResourceAttrs = attrs
								break
							}
						}
						if foundLogResourceAttrs != nil {
							break
						}
					}
					if foundLogResourceAttrs != nil {
						break
					}
				}
				if foundLogResourceAttrs != nil {
					break
				}
			}
		}

		if foundMetricResourceAttrs == nil {
			collected := h.selfMockSrv.CollectedMetrics()
			for _, md := range collected {
				for i := 0; i < md.ResourceMetrics().Len(); i++ {
					r := md.ResourceMetrics().At(i)
					for j := 0; j < r.ScopeMetrics().Len(); j++ {
						for k := 0; k < r.ScopeMetrics().At(j).Metrics().Len(); k++ {
							met := r.ScopeMetrics().At(j).Metrics().At(k)
							if met.Name() == "secops.threat.events" {
								attrs := make(map[string]string)
								r.Resource().Attributes().Range(func(key string, val pcommon.Value) bool {
									attrs[key] = val.AsString()
									return true
								})
								foundMetricResourceAttrs = attrs
								break
							}
						}
						if foundMetricResourceAttrs != nil {
							break
						}
					}
					if foundMetricResourceAttrs != nil {
						break
					}
				}
				if foundMetricResourceAttrs != nil {
					break
				}
			}
		}

		if foundLogResourceAttrs != nil && foundMetricResourceAttrs != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	h.col.shutdown(t)

	if foundLogResourceAttrs == nil {
		t.Fatalf("expected log record not found at destination.\ncollector logs:\n%s", h.col.logs())
	}
	if foundMetricResourceAttrs == nil {
		t.Fatalf("expected metric record not found at destination.\ncollector logs:\n%s", h.col.logs())
	}

	// Verify SecOps routing and categorization resource attributes populated on logs
	if got := foundLogResourceAttrs["routing.target"]; got != "secops" {
		t.Errorf("expected log routing.target to be 'secops', got %q", got)
	}
	if got := foundLogResourceAttrs["secops.category"]; got != "audit_events" {
		t.Errorf("expected log secops.category to be 'audit_events', got %q", got)
	}
	if got := foundLogResourceAttrs["deployment.environment"]; got != "production" {
		t.Errorf("expected log deployment.environment to be 'production', got %q", got)
	}
	if got := foundLogResourceAttrs["host.id"]; got != "vm-secops-01" {
		t.Errorf("expected log host.id to be 'vm-secops-01', got %q", got)
	}

	// Verify SecOps routing and categorization resource attributes populated on metrics
	if got := foundMetricResourceAttrs["routing.target"]; got != "secops" {
		t.Errorf("expected metric routing.target to be 'secops', got %q", got)
	}
	if got := foundMetricResourceAttrs["secops.category"]; got != "audit_events" {
		t.Errorf("expected metric secops.category to be 'audit_events', got %q", got)
	}
	if got := foundMetricResourceAttrs["deployment.environment"]; got != "production" {
		t.Errorf("expected metric deployment.environment to be 'production', got %q", got)
	}
	if got := foundMetricResourceAttrs["service.name"]; got != "secops-workload" {
		t.Errorf("expected metric service.name to be 'secops-workload', got %q", got)
	}
}

// TestPolicy_EnvResourceDetection_ExcludesCollectorPodAttributes verifies that
// collector process and Kubernetes pod/controller identity attributes present in
// OTEL_RESOURCE_ATTRIBUTES (describing the collector pod itself) do not leak onto
// canonical workload pipelines, while node/cluster and custom routing attributes
// still apply.
func TestPolicy_EnvResourceDetection_ExcludesCollectorPodAttributes(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", strings.Join([]string{
		"service.name=collector-pod",
		"service.instance.id=collector-pod-instance",
		"k8s.cluster.name=prod-cluster",
		"k8s.namespace.name=opentelemetry-system",
		"k8s.pod.name=otelcol-ds-abc",
		"k8s.pod.uid=pod-uid-123",
		"k8s.container.name=otelcol",
		"k8s.daemonset.name=otelcol-ds",
		"k8s.node.name=worker-node-1",
		"routing.target=secops",
	}, ","))

	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")

	ports := allocateEphemeralPorts(t, 1)
	customReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[0])

	appendReceiverCfg := fmt.Sprintf(`receivers:
  otlp/k8s_in:
    protocols:
      grpc:
        endpoint: %s
service:
  pipelines:
    logs/googlecontrolplane:
      receivers: [otlp/k8s_in]
`, customReceiverAddr)

	appendCfgPath := filepath.Join(tempDir, "append_k8s_receiver.yaml")
	if err := os.WriteFile(appendCfgPath, []byte(appendReceiverCfg), 0644); err != nil {
		t.Fatalf("failed to write append_k8s_receiver.yaml: %v", err)
	}

	h := startTestHarness(t, otelcolBin, tempDir, policiesDir, appendCfgPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(customReceiverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect to custom receiver %s: %v", customReceiverAddr, err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	customLogsClient := plogotlp.NewGRPCClient(conn)
	logs := plog.NewLogs()
	rl := logs.ResourceLogs().AppendEmpty()
	lr := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	lr.Body().SetStr("KEEP_THIS_LOG_LINE")

	if _, err := customLogsClient.Export(ctx, plogotlp.NewExportRequestFromLogs(logs)); err != nil {
		t.Fatalf("failed to export logs to custom receiver: %v\nstderr:\n%s", err, h.col.logs())
	}

	deadline := time.Now().Add(10 * time.Second)
	var foundLogResourceAttrs map[string]string
	for time.Now().Before(deadline) {
		for _, ld := range h.selfMockSrv.CollectedLogs() {
			for i := 0; i < ld.ResourceLogs().Len(); i++ {
				r := ld.ResourceLogs().At(i)
				for j := 0; j < r.ScopeLogs().Len(); j++ {
					for k := 0; k < r.ScopeLogs().At(j).LogRecords().Len(); k++ {
						if r.ScopeLogs().At(j).LogRecords().At(k).Body().Str() == "KEEP_THIS_LOG_LINE" {
							attrs := make(map[string]string)
							r.Resource().Attributes().Range(func(key string, val pcommon.Value) bool {
								attrs[key] = val.AsString()
								return true
							})
							foundLogResourceAttrs = attrs
						}
					}
				}
			}
		}
		if foundLogResourceAttrs != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	h.col.shutdown(t)

	if foundLogResourceAttrs == nil {
		t.Fatalf("expected log record not found at destination.\ncollector logs:\n%s", h.col.logs())
	}

	if got := foundLogResourceAttrs["k8s.cluster.name"]; got != "prod-cluster" {
		t.Errorf("expected k8s.cluster.name 'prod-cluster', got %q", got)
	}
	if got := foundLogResourceAttrs["k8s.node.name"]; got != "worker-node-1" {
		t.Errorf("expected k8s.node.name 'worker-node-1', got %q", got)
	}
	if got := foundLogResourceAttrs["routing.target"]; got != "secops" {
		t.Errorf("expected routing.target 'secops', got %q", got)
	}

	for _, excludedKey := range []string{
		"service.name",
		"service.instance.id",
		"k8s.namespace.name",
		"k8s.pod.name",
		"k8s.pod.uid",
		"k8s.container.name",
		"k8s.daemonset.name",
	} {
		if val, exists := foundLogResourceAttrs[excludedKey]; exists {
			t.Errorf("collector pod attribute %q=%q must not leak onto workload logs", excludedKey, val)
		}
	}
}

// TestPolicy_EnvResourceDetection_TelemetryTypeScopedToWorkloadLogs verifies that
// when gcp.telemetry_type is supplied via OTEL_RESOURCE_ATTRIBUTES, it is preserved
// on canonical workload logs (logs/googlecontrolplane) while being stripped from
// collector self-logs, collector self-metrics, canonical workload metrics, and
// canonical workload traces.
func TestPolicy_EnvResourceDetection_TelemetryTypeScopedToWorkloadLogs(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "gcp.telemetry_type=custom_logs,routing.target=secops")

	otelcolBin := resolveOtelcolBinary(t)
	tempDir, policiesDir := stagePoliciesDir(t, "drop_by_body_equals")
	copyDir(t, filepath.Join("testdata", "unsupported_policy", "policies"), policiesDir)

	ports := allocateEphemeralPorts(t, 1)
	customReceiverAddr := fmt.Sprintf("127.0.0.1:%d", ports[0])

	appendReceiverCfg := fmt.Sprintf(`receivers:
  otlp/telemetry_type_in:
    protocols:
      grpc:
        endpoint: %s
service:
  pipelines:
    logs/googlecontrolplane:
      receivers: [otlp/telemetry_type_in]
    metrics/googlecontrolplane:
      receivers: [otlp/telemetry_type_in]
    traces/googlecontrolplane:
      receivers: [otlp/telemetry_type_in]
`, customReceiverAddr)

	appendCfgPath := filepath.Join(tempDir, "append_telemetry_type_receiver.yaml")
	if err := os.WriteFile(appendCfgPath, []byte(appendReceiverCfg), 0644); err != nil {
		t.Fatalf("failed to write append_telemetry_type_receiver.yaml: %v", err)
	}

	h := startTestHarnessWithSelfLogs(t, otelcolBin, tempDir, policiesDir, appendCfgPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(customReceiverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect to custom receiver %s: %v", customReceiverAddr, err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	// 1. Send workload log
	logs := plog.NewLogs()
	logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty().Body().SetStr("WORKLOAD_LOG_TELEMETRY_TYPE")
	if _, err := plogotlp.NewGRPCClient(conn).Export(ctx, plogotlp.NewExportRequestFromLogs(logs)); err != nil {
		t.Fatalf("failed to export workload logs: %v\nstderr:\n%s", err, h.col.logs())
	}

	// 2. Send workload metric
	metrics := pmetric.NewMetrics()
	m := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	m.SetName("workload.telemetry_type.check")
	dp := m.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.SetIntValue(1)
	dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	if _, err := pmetricotlp.NewGRPCClient(conn).Export(ctx, pmetricotlp.NewExportRequestFromMetrics(metrics)); err != nil {
		t.Fatalf("failed to export workload metrics: %v\nstderr:\n%s", err, h.col.logs())
	}

	// 3. Send workload trace
	traces := ptrace.NewTraces()
	sp := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	sp.SetName("workload.telemetry_type.span")
	sp.SetTraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	sp.SetSpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	if _, err := ptraceotlp.NewGRPCClient(conn).Export(ctx, ptraceotlp.NewExportRequestFromTraces(traces)); err != nil {
		t.Fatalf("failed to export workload traces: %v\nstderr:\n%s", err, h.col.logs())
	}

	attrsToMap := func(am pcommon.Map) map[string]string {
		out := make(map[string]string, am.Len())
		am.Range(func(k string, v pcommon.Value) bool {
			out[k] = v.AsString()
			return true
		})
		return out
	}

	deadline := time.Now().Add(10 * time.Second)
	var workloadLogAttrs, selfLogAttrs, workloadMetricAttrs, selfMetricAttrs, workloadTraceAttrs map[string]string

	for time.Now().Before(deadline) {
		for _, ld := range h.selfMockSrv.CollectedLogs() {
			for i := 0; i < ld.ResourceLogs().Len(); i++ {
				rl := ld.ResourceLogs().At(i)
				for j := 0; j < rl.ScopeLogs().Len(); j++ {
					for k := 0; k < rl.ScopeLogs().At(j).LogRecords().Len(); k++ {
						body := rl.ScopeLogs().At(j).LogRecords().At(k).Body().Str()
						if body == "WORKLOAD_LOG_TELEMETRY_TYPE" && workloadLogAttrs == nil {
							workloadLogAttrs = attrsToMap(rl.Resource().Attributes())
						}
						if strings.Contains(body, "no driver found for policy type: unsupported_policy_type") && selfLogAttrs == nil {
							selfLogAttrs = attrsToMap(rl.Resource().Attributes())
						}
					}
				}
			}
		}
		for _, md := range h.selfMockSrv.CollectedMetrics() {
			for i := 0; i < md.ResourceMetrics().Len(); i++ {
				rm := md.ResourceMetrics().At(i)
				for j := 0; j < rm.ScopeMetrics().Len(); j++ {
					for k := 0; k < rm.ScopeMetrics().At(j).Metrics().Len(); k++ {
						name := rm.ScopeMetrics().At(j).Metrics().At(k).Name()
						if name == "workload.telemetry_type.check" && workloadMetricAttrs == nil {
							workloadMetricAttrs = attrsToMap(rm.Resource().Attributes())
						}
						if name == "gcp.policy.set.active" && selfMetricAttrs == nil {
							selfMetricAttrs = attrsToMap(rm.Resource().Attributes())
						}
					}
				}
			}
		}
		for _, td := range h.selfMockSrv.CollectedTraces() {
			for i := 0; i < td.ResourceSpans().Len(); i++ {
				rs := td.ResourceSpans().At(i)
				for j := 0; j < rs.ScopeSpans().Len(); j++ {
					for k := 0; k < rs.ScopeSpans().At(j).Spans().Len(); k++ {
						if rs.ScopeSpans().At(j).Spans().At(k).Name() == "workload.telemetry_type.span" && workloadTraceAttrs == nil {
							workloadTraceAttrs = attrsToMap(rs.Resource().Attributes())
						}
					}
				}
			}
		}
		if workloadLogAttrs != nil && selfLogAttrs != nil && workloadMetricAttrs != nil && selfMetricAttrs != nil && workloadTraceAttrs != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	h.col.shutdown(t)

	if workloadLogAttrs == nil || selfLogAttrs == nil || workloadMetricAttrs == nil || selfMetricAttrs == nil || workloadTraceAttrs == nil {
		t.Fatalf("timed out waiting for all signals (workloadLog=%v selfLog=%v workloadMetric=%v selfMetric=%v workloadTrace=%v)\nlogs:\n%s",
			workloadLogAttrs != nil, selfLogAttrs != nil, workloadMetricAttrs != nil, selfMetricAttrs != nil, workloadTraceAttrs != nil, h.col.logs())
	}

	// Workload logs MUST preserve gcp.telemetry_type.
	if got := workloadLogAttrs["gcp.telemetry_type"]; got != "custom_logs" {
		t.Errorf("expected workload log gcp.telemetry_type='custom_logs', got %q (attrs=%v)", got, workloadLogAttrs)
	}
	if got := workloadLogAttrs["routing.target"]; got != "secops" {
		t.Errorf("expected workload log routing.target='secops', got %q", got)
	}

	// Self-logs, self-metrics, workload metrics, and workload traces MUST NOT carry gcp.telemetry_type.
	for name, attrs := range map[string]map[string]string{
		"self-log":        selfLogAttrs,
		"self-metric":     selfMetricAttrs,
		"workload-metric": workloadMetricAttrs,
		"workload-trace":  workloadTraceAttrs,
	} {
		if val, exists := attrs["gcp.telemetry_type"]; exists {
			t.Errorf("expected gcp.telemetry_type to be stripped from %s, got %q (attrs=%v)", name, val, attrs)
		}
	}
	if got := workloadMetricAttrs["routing.target"]; got != "secops" {
		t.Errorf("expected workload metric routing.target='secops', got %q", got)
	}
	if got := workloadTraceAttrs["routing.target"]; got != "secops" {
		t.Errorf("expected workload trace routing.target='secops', got %q", got)
	}
}
