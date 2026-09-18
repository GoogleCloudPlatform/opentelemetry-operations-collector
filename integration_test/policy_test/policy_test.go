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

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"go.yaml.in/yaml/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/encoding/gzip"
)

var (
	otelcolFlag = flag.String("otelcol", os.Getenv("OTELCOL"), "path to compiled otelcol-google binary")
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

	defaultRel := filepath.Join("..", "..", "google-built-opentelemetry-collector", "otelcol-google")
	if abs, err := filepath.Abs(defaultRel); err == nil {
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}

	t.Skip("--otelcol flag or OTELCOL env var not supplied and default binary not found")
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
			t.Parallel()
			runPolicyTestCase(t, otelcolBin, caseName)
		})
	}
}

func runPolicyTestCase(t *testing.T, otelcolBin, caseName string) {
	caseDir := filepath.Join("testdata", caseName)
	tempDir := t.TempDir()

	policiesDir := filepath.Join(tempDir, "policies")
	copyDir(t, filepath.Join(caseDir, "policies"), policiesDir)

	mockSrv, mockExporterAddr := startMockOTLPServer(t)
	otlpReceiverAddr := fmt.Sprintf("127.0.0.1:%d", allocateEphemeralPort(t))
	selfReceiverAddr := fmt.Sprintf("127.0.0.1:%d", allocateEphemeralPort(t))

	pipelineCfg := fmt.Sprintf(`receivers:
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

	pipelineCfgPath := filepath.Join(tempDir, "test_pipeline.yaml")
	if err := os.WriteFile(pipelineCfgPath, []byte(pipelineCfg), 0644); err != nil {
		t.Fatalf("failed to write test_pipeline.yaml: %v", err)
	}

	testStartTime := time.Now()

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
	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start otelcol-google: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	readyCh := make(chan bool, 1)
	var readyOnce sync.Once
	var stderrLog bytes.Buffer
	var stderrMu sync.Mutex

	go func() {
		defer readyOnce.Do(func() { readyCh <- false })
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stderrMu.Lock()
			stderrLog.WriteString(line + "\n")
			stderrMu.Unlock()
			if strings.Contains(line, "Everything is ready. Begin running and processing data.") {
				readyOnce.Do(func() { readyCh <- true })
			}
		}
	}()

	select {
	case ready := <-readyCh:
		if !ready {
			stderrMu.Lock()
			logs := stderrLog.String()
			stderrMu.Unlock()
			t.Fatalf("collector exited before becoming ready.\nstderr:\n%s", logs)
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		stderrMu.Lock()
		logs := stderrLog.String()
		stderrMu.Unlock()
		t.Fatalf("timed out waiting for collector readiness.\nstderr:\n%s", logs)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(otlpReceiverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create gRPC client to %s: %v", otlpReceiverAddr, err)
	}
	defer conn.Close()

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
		client := plogotlp.NewGRPCClient(conn)
		if _, err := client.Export(ctx, plogotlp.NewExportRequestFromLogs(inputLogs)); err != nil {
			stderrMu.Lock()
			logs := stderrLog.String()
			stderrMu.Unlock()
			t.Fatalf("failed to export logs to collector: %v\nstderr:\n%s", err, logs)
		}
	}

	if hasMetrics {
		inputMetrics := readInputMetrics(t, inputMetricsPath)
		client := pmetricotlp.NewGRPCClient(conn)
		if _, err := client.Export(ctx, pmetricotlp.NewExportRequestFromMetrics(inputMetrics)); err != nil {
			stderrMu.Lock()
			logs := stderrLog.String()
			stderrMu.Unlock()
			t.Fatalf("failed to export metrics to collector: %v\nstderr:\n%s", err, logs)
		}
	}

	if hasTraces {
		inputTraces := readInputTraces(t, inputTracesPath)
		client := ptraceotlp.NewGRPCClient(conn)
		if _, err := client.Export(ctx, ptraceotlp.NewExportRequestFromTraces(inputTraces)); err != nil {
			stderrMu.Lock()
			logs := stderrLog.String()
			stderrMu.Unlock()
			t.Fatalf("failed to export traces to collector: %v\nstderr:\n%s", err, logs)
		}
	}

	// Allow in-flight OTLP batches to reach mock exporter before triggering graceful drain.
	time.Sleep(250 * time.Millisecond)

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Logf("warning: failed to send SIGTERM to collector: %v", err)
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case err := <-waitDone:
		if err != nil {
			t.Logf("collector exited with: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("timed out waiting for collector graceful shutdown")
	}

	stderrMu.Lock()
	capturedStderr := stderrLog.String()
	stderrMu.Unlock()

	if hasLogs {
		gotLogs := normalizeExportedLogs(t, mockSrv.CollectedLogs(), testStartTime)
		assertGoldenYAML(t, filepath.Join(caseDir, "expected_logs.yaml"), gotLogs, capturedStderr)
	}

	if hasMetrics {
		gotMetrics := normalizeExportedMetrics(t, mockSrv.CollectedMetrics(), testStartTime)
		assertGoldenYAML(t, filepath.Join(caseDir, "expected_metrics.yaml"), gotMetrics, capturedStderr)
	}

	if hasTraces {
		gotTraces := normalizeExportedTraces(t, mockSrv.CollectedTraces(), testStartTime)
		assertGoldenYAML(t, filepath.Join(caseDir, "expected_traces.yaml"), gotTraces, capturedStderr)
	}
}
