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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/processor/googlepolicyprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/envprovider"
	"go.opentelemetry.io/collector/confmap/provider/fileprovider"
	"go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/nopexporter"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/otelcol"
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

func allocateEphemeralPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate ephemeral port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
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

func newStubExtensionFactory(typeStr string) extension.Factory {
	return extension.NewFactory(
		component.MustNewType(typeStr),
		func() component.Config { return &stubComponentConfig{} },
		func(context.Context, extension.Settings, component.Config) (extension.Extension, error) {
			return nil, nil
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

func newStubProcessorFactory(typeStr string) processor.Factory {
	return processor.NewFactory(
		component.MustNewType(typeStr),
		func() component.Config { return &stubComponentConfig{} },
	)
}

func testCollectorFactories() (otelcol.Factories, error) {
	var err error
	factories := otelcol.Factories{
		Telemetry: otelconftelemetry.NewFactory(),
	}
	factories.Extensions, err = otelcol.MakeFactoryMap[extension.Factory](
		newStubExtensionFactory("googleclientauth"),
	)
	if err != nil {
		return otelcol.Factories{}, err
	}

	factories.Receivers, err = otelcol.MakeFactoryMap[receiver.Factory](
		otlpreceiver.NewFactory(),
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
		newStubProcessorFactory("resourcedetection"),
		newStubProcessorFactory("transform"),
		newStubProcessorFactory("queuebatch"),
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

func startCollector(t *testing.T, otelcolBin, policiesDir, pipelineCfgPath string) *collectorHandle {
	t.Helper()
	if otelcolBin == "" {
		return startInProcessCollector(t, policiesDir, pipelineCfgPath)
	}
	return startSubprocessCollector(t, otelcolBin, policiesDir, pipelineCfgPath)
}

func startInProcessCollector(t *testing.T, policiesDir, pipelineCfgPath string) *collectorHandle {
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
				URIs: []string{
					fmt.Sprintf("googlecontrolplane:file:%s", filepath.ToSlash(policiesDir)),
					fmt.Sprintf("file:%s", filepath.ToSlash(pipelineCfgPath)),
				},
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

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- col.Run(context.Background())
	}()

	var shutdownOnce sync.Once
	shutdownFn := func(t *testing.T) {
		shutdownOnce.Do(func() {
			col.Shutdown()
			select {
			case err := <-runErrCh:
				if err != nil {
					t.Logf("in-process collector exited with: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("timed out waiting for in-process collector shutdown.\nlogs:\n%s", logBuf.String())
			}
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
		case err := <-runErrCh:
			t.Fatalf("in-process collector exited before becoming ready: %v\nlogs:\n%s", err, logBuf.String())
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

func startSubprocessCollector(t *testing.T, otelcolBin, policiesDir, pipelineCfgPath string) *collectorHandle {
	t.Helper()
	cmd := exec.Command(
		otelcolBin,
		fmt.Sprintf("--config=googlecontrolplane:file:%s", filepath.ToSlash(policiesDir)),
		fmt.Sprintf("--config=file:%s", filepath.ToSlash(pipelineCfgPath)),
	)
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

func testPipelineConfig(otlpReceiverAddr, selfReceiverAddr, mockExporterAddr string) string {
	return fmt.Sprintf(`receivers:
  otlp/test_in:
    protocols:
      grpc:
        endpoint: %s
  otlp/self_in:
    protocols:
      grpc:
        endpoint: %s
exporters:
  otlp_grpc/test_out:
    endpoint: %s
    sending_queue:
      enabled: false
    tls:
      insecure: true
service:
  extensions: []
  telemetry:
    logs:
      processors: []
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
    logs/default_self_metrics:
      receivers: [otlp/self_in]
      processors: []
      exporters: [otlp_grpc/test_out]
    metrics/default_self_metrics:
      receivers: [otlp/self_in]
      processors: []
      exporters: [otlp_grpc/test_out]
`, otlpReceiverAddr, selfReceiverAddr, mockExporterAddr)
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
	mockSrv *mockOTLPServer
	col     *collectorHandle
	conn    *grpc.ClientConn
}

func startTestHarness(t *testing.T, otelcolBin, tempDir, policiesDir string) *testHarness {
	t.Helper()
	mockSrv, mockExporterAddr := startMockOTLPServer(t)
	otlpReceiverAddr := fmt.Sprintf("127.0.0.1:%d", allocateEphemeralPort(t))
	selfReceiverAddr := fmt.Sprintf("127.0.0.1:%d", allocateEphemeralPort(t))

	pipelineCfg := testPipelineConfig(otlpReceiverAddr, selfReceiverAddr, mockExporterAddr)
	pipelineCfgPath := filepath.Join(tempDir, "test_pipeline.yaml")
	if err := os.WriteFile(pipelineCfgPath, []byte(pipelineCfg), 0644); err != nil {
		t.Fatalf("failed to write test_pipeline.yaml: %v", err)
	}

	col := startCollector(t, otelcolBin, policiesDir, pipelineCfgPath)

	conn, err := grpc.NewClient(otlpReceiverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create gRPC client to %s: %v", otlpReceiverAddr, err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})

	return &testHarness{
		mockSrv: mockSrv,
		col:     col,
		conn:    conn,
	}
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
  "matches": [
    {
      "target": {
        "record_field": "LOG_RECORD_FIELD_BODY"
      },
      "equals": {
        "string_value": "drop-initial"
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
  "matches": [
    {
      "target": {
        "record_field": "LOG_RECORD_FIELD_BODY"
      },
      "equals": {
        "string_value": "drop-after-reload"
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
