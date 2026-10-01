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

//go:build integration_test

package smoke

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/gce"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/integration_test/gce-testing-internal/logging"
)

// PKIArtifacts holds PEM-encoded certificates and private keys generated for testing mTLS.
type PKIArtifacts struct {
	CACertPEM          []byte
	ServerCertPEM      []byte
	ServerKeyPEM       []byte
	ClientCertPEM      []byte
	ClientKeyPEM       []byte
	RogueCACertPEM     []byte
	RogueClientCertPEM []byte
	RogueClientKeyPEM  []byte
}

// generateTestPKI creates a self-contained Root CA, signed Server certificate,
// signed Client certificate, and an untrusted Rogue client certificate for negative testing.
func generateTestPKI() (*PKIArtifacts, error) {
	// 1. Generate Root CA
	caPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA key: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(202601),
		Subject: pkix.Name{
			Organization: []string{"GBOC Gateway Test Root CA"},
			CommonName:   "GBOC Test Root CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	caCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caBytes})
	caKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caPrivKey)})

	// 2. Generate Server Certificate signed by Root CA
	serverPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server key: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(202602),
		Subject: pkix.Name{
			Organization: []string{"GBOC Gateway Server"},
			CommonName:   "localhost",
		},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("0.0.0.0"), net.ParseIP("::1")},
		DNSNames:     []string{"localhost", "gateway.internal", "localhost.localdomain"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}

	serverBytes, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create server certificate: %w", err)
	}

	serverCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverBytes})
	serverKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(serverPrivKey)})

	// 3. Generate Authorized Client Certificate signed by Root CA
	clientPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate client key: %w", err)
	}

	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(202603),
		Subject: pkix.Name{
			Organization: []string{"GBOC Upstream Node"},
			CommonName:   "upstream-worker-client",
		},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		SubjectKeyId: []byte{1, 2, 3, 4, 7},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}

	clientBytes, err := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create client certificate: %w", err)
	}

	clientCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientBytes})
	clientKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(clientPrivKey)})

	// 4. Generate Rogue Root CA and Rogue Client Certificate (untrusted CA)
	rogueCAPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate rogue CA key: %w", err)
	}
	rogueCATemplate := &x509.Certificate{
		SerialNumber: big.NewInt(202699),
		Subject: pkix.Name{
			Organization: []string{"Untrusted Rogue CA"},
			CommonName:   "Rogue Untrusted CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	rogueCABytes, err := x509.CreateCertificate(rand.Reader, rogueCATemplate, rogueCATemplate, &rogueCAPrivKey.PublicKey, rogueCAPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create rogue CA certificate: %w", err)
	}

	rogueClientPrivKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate rogue client key: %w", err)
	}
	rogueClientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(202698),
		Subject: pkix.Name{
			Organization: []string{"Untrusted Attacker"},
			CommonName:   "rogue-attacker-client",
		},
		NotBefore:   time.Now().Add(-1 * time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	rogueClientBytes, err := x509.CreateCertificate(rand.Reader, rogueClientTemplate, rogueCATemplate, &rogueClientPrivKey.PublicKey, rogueCAPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create rogue client certificate: %w", err)
	}

	return &PKIArtifacts{
		CACertPEM:          caCertPEM,
		ServerCertPEM:      serverCertPEM,
		ServerKeyPEM:       serverKeyPEM,
		ClientCertPEM:      clientCertPEM,
		ClientKeyPEM:       clientKeyPEM,
		RogueCACertPEM:     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rogueCABytes}),
		RogueClientCertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rogueClientBytes}),
		RogueClientKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rogueClientPrivKey)}),
	}, nil
}

// getGatewayOtelcolConfig reads gateway.yaml from OTELCOL_CONFIGS_DIR and renders it with TestRunID and CertDir.
func getGatewayOtelcolConfig(t *testing.T, certDir string) string {
	configDir := os.Getenv("OTELCOL_CONFIGS_DIR")
	if configDir == "" {
		t.Fatal("Must pass nonempty value for OTELCOL_CONFIGS_DIR")
	}
	configPath := path.Join(configDir, "gateway.yaml")
	t.Logf("Reading gateway otelcol config from %q", configPath)

	temp, err := template.New("gateway.yaml").ParseFiles(configPath)
	if err != nil {
		t.Fatal(err)
	}

	type Data struct {
		TestRunID string
		CertDir   string
	}
	data := Data{
		TestRunID: os.Getenv("KOKORO_BUILD_ID"),
		CertDir:   certDir,
	}
	if data.TestRunID == "" {
		t.Fatal("This test does not support being run outside of Kokoro.")
	}
	var builder strings.Builder
	if err := temp.Execute(&builder, data); err != nil {
		t.Fatal(err)
	}

	return builder.String()
}

func uploadPKICertificates(ctx context.Context, logger *log.Logger, vm *gce.VM, pki *PKIArtifacts, certDir string) error {
	certFiles := map[string][]byte{
		"ca.crt":           pki.CACertPEM,
		"server.crt":       pki.ServerCertPEM,
		"server.key":       pki.ServerKeyPEM,
		"client.crt":       pki.ClientCertPEM,
		"client.key":       pki.ClientKeyPEM,
		"rogue_client.crt": pki.RogueClientCertPEM,
		"rogue_client.key": pki.RogueClientKeyPEM,
	}

	for name, content := range certFiles {
		remotePath := path.Join(certDir, name)
		if gce.IsWindows(vm.ImageSpec) {
			remotePath = fmt.Sprintf(`%s\%s`, certDir, name)
		}
		if err := gce.UploadContent(ctx, logger, vm, bytes.NewReader(content), remotePath); err != nil {
			return fmt.Errorf("failed uploading cert %s to %s: %w", name, remotePath, err)
		}
	}

	// On Linux, secure permissions so the otelcol service user can read certificates and keys
	if !gce.IsWindows(vm.ImageSpec) {
		if _, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("sudo chmod -R 755 %s && sudo chmod 644 %s/*.key %s/*.crt", certDir, certDir, certDir)); err != nil {
			return fmt.Errorf("failed setting permissions on %s: %w", certDir, err)
		}
	}
	return nil
}

// sendAuthenticatedUpstreamData sends OTLP metrics, logs, and traces over HTTPS with valid mTLS client certificates.
func sendAuthenticatedUpstreamData(ctx context.Context, logger *log.Logger, vm *gce.VM, certDir string) error {
	nowNano := time.Now().UnixNano()
	traceID := randomHex(16)
	spanID := randomHex(8)
	startNano := nowNano - int64(100*time.Millisecond)
	endNano := nowNano

	// 1. Upstream VM simulated OTLP logs payload (preserves host.id and host.name)
	upstreamVMLogJSON := fmt.Sprintf(`{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "cloud.provider", "value": {"stringValue": "gcp"}},
					{"key": "cloud.platform", "value": {"stringValue": "gcp_compute_engine"}},
					{"key": "cloud.account.id", "value": {"stringValue": "%s"}},
					{"key": "cloud.zone", "value": {"stringValue": "us-central1-a"}},
					{"key": "host.id", "value": {"stringValue": "987654321012345678"}},
					{"key": "host.name", "value": {"stringValue": "upstream-edge-vm-01"}}
				]
			},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "%d",
					"body": {"stringValue": "mTLS authenticated log from upstream GCE VM 01 via Gateway"},
					"attributes": [
						{"key": "source_host", "value": {"stringValue": "upstream-edge-vm-01"}},
						{"key": "upstream_flavor", "value": {"stringValue": "simulated_gce_vm"}}
					]
				}]
			}]
		}]
	}`, vm.Project, nowNano)

	// 2. Upstream Kubernetes Node/Pod simulated OTLP logs payload (preserves k8s attributes)
	upstreamK8sLogJSON := fmt.Sprintf(`{
		"resourceLogs": [{
			"resource": {
				"attributes": [
					{"key": "cloud.provider", "value": {"stringValue": "gcp"}},
					{"key": "cloud.platform", "value": {"stringValue": "gcp_kubernetes_engine"}},
					{"key": "k8s.cluster.name", "value": {"stringValue": "production-k8s-cluster"}},
					{"key": "k8s.namespace.name", "value": {"stringValue": "billing-system"}},
					{"key": "k8s.pod.name", "value": {"stringValue": "payment-worker-pod-42"}},
					{"key": "k8s.node.name", "value": {"stringValue": "gke-node-pool-1-worker-99"}}
				]
			},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "%d",
					"body": {"stringValue": "mTLS authenticated log from Kubernetes pod payment-worker-pod-42 via Gateway"},
					"attributes": [
						{"key": "source_k8s_pod", "value": {"stringValue": "payment-worker-pod-42"}},
						{"key": "upstream_flavor", "value": {"stringValue": "simulated_k8s_pod"}}
					]
				}]
			}]
		}]
	}`, nowNano)

	// 3. Upstream simulated OTLP Trace payload
	upstreamTraceJSON := fmt.Sprintf(`{
		"resourceSpans": [{
			"resource": {
				"attributes": [
					{"key": "cloud.provider", "value": {"stringValue": "gcp"}},
					{"key": "cloud.platform", "value": {"stringValue": "gcp_kubernetes_engine"}},
					{"key": "k8s.pod.name", "value": {"stringValue": "payment-worker-pod-42"}},
					{"key": "service.name", "value": {"stringValue": "payment-service"}}
				]
			},
			"scopeSpans": [{
				"spans": [{
					"traceId": "%s",
					"spanId": "%s",
					"name": "gateway-mtls-payment-process",
					"kind": 1,
					"startTimeUnixNano": "%d",
					"endTimeUnixNano": "%d",
					"attributes": [
						{"key": "http.status_code", "value": {"intValue": 200}},
						{"key": "upstream_flavor", "value": {"stringValue": "simulated_k8s_trace"}}
					]
				}]
			}]
		}]
	}`, traceID, spanID, startNano, endNano)

	// 4. Upstream simulated OTLP Metrics payload
	upstreamMetricJSON := fmt.Sprintf(`{
		"resourceMetrics": [{
			"resource": {
				"attributes": [
					{"key": "cloud.provider", "value": {"stringValue": "gcp"}},
					{"key": "cloud.platform", "value": {"stringValue": "gcp_compute_engine"}},
					{"key": "cloud.account.id", "value": {"stringValue": "%s"}},
					{"key": "cloud.zone", "value": {"stringValue": "us-central1-a"}},
					{"key": "host.id", "value": {"stringValue": "987654321012345678"}},
					{"key": "host.name", "value": {"stringValue": "upstream-edge-vm-01"}}
				]
			},
			"scopeMetrics": [{
				"metrics": [{
					"name": "workload.googleapis.com/gateway_forwarded_requests",
					"gauge": {
						"dataPoints": [{
							"timeUnixNano": "%d",
							"asInt": "42",
							"attributes": [
								{"key": "client_env", "value": {"stringValue": "upstream_edge_node"}}
							]
						}]
					}
				}]
			}]
		}]
	}`, vm.Project, nowNano)

	payloads := map[string]struct {
		endpoint string
		jsonBody string
	}{
		"upstream_vm_log.json":  {endpoint: "https://localhost:4318/v1/logs", jsonBody: upstreamVMLogJSON},
		"upstream_k8s_log.json": {endpoint: "https://localhost:4318/v1/logs", jsonBody: upstreamK8sLogJSON},
		"upstream_trace.json":   {endpoint: "https://localhost:4318/v1/traces", jsonBody: upstreamTraceJSON},
		"upstream_metric.json":  {endpoint: "https://localhost:4318/v1/metrics", jsonBody: upstreamMetricJSON},
	}

	for filename, p := range payloads {
		remoteFilePath := path.Join("/tmp", filename)
		caPath := path.Join(certDir, "ca.crt")
		certPath := path.Join(certDir, "client.crt")
		keyPath := path.Join(certDir, "client.key")

		if gce.IsWindows(vm.ImageSpec) {
			remoteFilePath = fmt.Sprintf(`%s\%s`, certDir, filename)
			caPath = fmt.Sprintf(`%s\ca.crt`, certDir)
			certPath = fmt.Sprintf(`%s\client.crt`, certDir)
			keyPath = fmt.Sprintf(`%s\client.key`, certDir)
		}

		if err := gce.UploadContent(ctx, logger, vm, strings.NewReader(p.jsonBody), remoteFilePath); err != nil {
			return fmt.Errorf("sendAuthenticatedUpstreamData() failed uploading %s: %w", filename, err)
		}

		// Use mTLS with curl: --cacert verifies gateway server cert, --cert and --key authenticate client to gateway
		var cmd string
		if gce.IsWindows(vm.ImageSpec) {
			cmd = fmt.Sprintf(`curl.exe -s -f -X POST "%s" --cacert "%s" --cert "%s" --key "%s" -H "Content-Type: application/json" -d @%s`,
				p.endpoint, caPath, certPath, keyPath, remoteFilePath)
		} else {
			cmd = fmt.Sprintf(`curl -s -f -X POST "%s" --cacert "%s" --cert "%s" --key "%s" -H "Content-Type: application/json" -d @%s`,
				p.endpoint, caPath, certPath, keyPath, remoteFilePath)
		}

		if _, err := gce.RunRemotely(ctx, logger, vm, cmd); err != nil {
			return fmt.Errorf("sendAuthenticatedUpstreamData() failed posting %s over mTLS: %w", filename, err)
		}
		logger.Printf("Successfully sent mTLS authenticated payload %s to %s", filename, p.endpoint)
	}

	return nil
}

// verifyUnauthorizedRejections verifies that requests without a client cert or with an untrusted client cert are rejected.
func verifyUnauthorizedRejections(ctx context.Context, logger *log.Logger, vm *gce.VM, certDir string) error {
	caPath := path.Join(certDir, "ca.crt")
	rogueCertPath := path.Join(certDir, "rogue_client.crt")
	rogueKeyPath := path.Join(certDir, "rogue_client.key")

	if gce.IsWindows(vm.ImageSpec) {
		caPath = fmt.Sprintf(`%s\ca.crt`, certDir)
		rogueCertPath = fmt.Sprintf(`%s\rogue_client.crt`, certDir)
		rogueKeyPath = fmt.Sprintf(`%s\rogue_client.key`, certDir)
	}

	testPayload := `{"resourceLogs": []}`

	// Negative Test 1: No client certificate presented
	noCertCmd := fmt.Sprintf(`curl -s -f -X POST "https://localhost:4318/v1/logs" --cacert "%s" -H "Content-Type: application/json" -d '%s'`, caPath, testPayload)
	if _, err := gce.RunRemotely(ctx, logger, vm, noCertCmd); err == nil {
		return fmt.Errorf("SECURITY FAILURE: Gateway accepted unauthenticated request with no client certificate")
	}
	logger.Println("Successfully verified: Gateway rejected request without client certificate.")

	// Negative Test 2: Client certificate signed by untrusted Rogue CA
	rogueCertCmd := fmt.Sprintf(`curl -s -f -X POST "https://localhost:4318/v1/logs" --cacert "%s" --cert "%s" --key "%s" -H "Content-Type: application/json" -d '%s'`,
		caPath, rogueCertPath, rogueKeyPath, testPayload)
	if _, err := gce.RunRemotely(ctx, logger, vm, rogueCertCmd); err == nil {
		return fmt.Errorf("SECURITY FAILURE: Gateway accepted untrusted client certificate signed by rogue CA")
	}
	logger.Println("Successfully verified: Gateway rejected untrusted rogue client certificate.")

	return nil
}

func TestGBOCGateway(t *testing.T) {
	t.Parallel()

	testRunID := os.Getenv("KOKORO_BUILD_ID")
	pki, err := generateTestPKI()
	if err != nil {
		t.Fatalf("Failed to generate test PKI certificates: %v", err)
	}

	gce.RunForEachImage(t, func(t *testing.T, imageSpec string) {
		t.Parallel()
		ctx, dirLog, vm := commonSetupWithExtraCreateArgumentsAndMetadata(t, imageSpec, nil, nil)
		logger := dirLog.ToMainLog()

		certDir := "/etc/otelcol-google/certs"
		if gce.IsWindows(imageSpec) {
			certDir = `C:\collectorUpload\certs`
		}

		if _, err := gce.RunRemotely(ctx, logger, vm, fmt.Sprintf("sudo mkdir -p %s", certDir)); err != nil {
			t.Fatalf("Failed creating cert dir %s: %v", certDir, err)
		}

		// 1. Provision certificates onto the Gateway VM
		if err := uploadPKICertificates(ctx, logger, vm, pki, certDir); err != nil {
			t.Fatalf("Failed uploading mTLS PKI to VM: %v", err)
		}

		// 2. Render and install mTLS Gateway Collector configuration
		config := getGatewayOtelcolConfig(t, certDir)
		logger.Printf("Installing GBOC in mTLS Gateway mode with config:\n%s", config)
		if err := setupOtelCollector(ctx, logger, vm, config); err != nil {
			t.Fatal(err)
		}

		// Allow service time to bind ports 4317/4318/4320
		time.Sleep(10 * time.Second)

		// 3. Verify mTLS authorization enforcement (reject unauthenticated & untrusted certs)
		t.Run("security_mtls_rejections", func(t *testing.T) {
			if err := verifyUnauthorizedRejections(ctx, logger, vm, certDir); err != nil {
				t.Fatalf("mTLS security validation failed: %v", err)
			}
		})

		// 4. Send authenticated upstream OTLP traffic over mTLS
		if err := sendAuthenticatedUpstreamData(ctx, logger, vm, certDir); err != nil {
			t.Fatalf("Failed sending authenticated OTLP payloads to Gateway: %v", err)
		}

		// 5. Verify upstream forwarded telemetry preservation in Google Cloud
		t.Run("forwarded_upstream_vm_logs", func(t *testing.T) {
			t.Parallel()
			GatewayForwardedVMTest(ctx, t, logger, vm)
		})
		t.Run("forwarded_k8s_pod_logs", func(t *testing.T) {
			t.Parallel()
			GatewayForwardedK8sTest(ctx, t, logger, vm)
		})
		t.Run("forwarded_upstream_traces", func(t *testing.T) {
			t.Parallel()
			GatewayForwardedTraceTest(ctx, t, logger, vm)
		})

		// 6. Verify gateway self-observability bound to the Gateway VM itself
		t.Run("gateway_self_observability_metrics", func(t *testing.T) {
			t.Parallel()
			GatewaySelfObservabilityMetricTest(ctx, t, logger, vm)
		})
		t.Run("gateway_self_observability_logs", func(t *testing.T) {
			t.Parallel()
			GatewaySelfObservabilityLogTest(ctx, t, logger, vm)
		})
	})
}
