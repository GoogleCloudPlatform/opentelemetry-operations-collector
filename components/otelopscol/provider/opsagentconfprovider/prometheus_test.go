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

package opsagentconfprovider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

func TestPrometheusValidate(t *testing.T) {
	tmpDir := t.TempDir()
	certFile := filepath.Join(tmpDir, "cert.pem")
	keyFile := filepath.Join(tmpDir, "key.pem")
	credsFile := filepath.Join(tmpDir, "token")
	require.NoError(t, os.WriteFile(certFile, []byte("cert"), 0600))
	require.NoError(t, os.WriteFile(keyFile, []byte("key"), 0600))
	require.NoError(t, os.WriteFile(credsFile, []byte("secret"), 0600))

	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name: "valid_prometheus_with_tls_and_credentials",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: secure_node
            scrape_interval: 10s
            static_configs:
              - targets: ['localhost:1234']
            authorization:
              credentials_file: ` + credsFile + `
            tls_config:
              cert_file: ` + certFile + `
              key_file: ` + keyFile + `
              insecure_skip_verify: true
  service:
    pipelines:
      prom_pipe:
        receivers: [prometheus]
`,
		},
		{
			name: "hostmetrics_with_config_rejected",
			config: `metrics:
  receivers:
    hostmetrics:
      type: hostmetrics
      config:
        scrape_configs: []
`,
			wantErr: `metrics receiver "hostmetrics" with type "hostmetrics" does not support config`,
		},
		{
			name: "prometheus_with_collection_interval_rejected",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      collection_interval: 30s
      config:
        scrape_configs:
          - job_name: node
            static_configs:
              - targets: ['localhost:1234']
`,
			wantErr: `metrics receiver "prometheus" with type "prometheus" does not support collection_interval`,
		},
		{
			name: "prometheus_missing_config",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
`,
			wantErr: `no Prometheus scrape_configs`,
		},
		{
			name: "prometheus_empty_scrape_configs",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs: []
`,
			wantErr: `no Prometheus scrape_configs`,
		},
		{
			name: "prometheus_unsupported_remote_read_write",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: prometheus
            static_configs:
              - targets: ['localhost:1234']
        remote_write:
          - url: "https://example.com/api/Write"
        remote_read:
          - url: "https://example.com/api/Read"
`,
			wantErr: "unsupported features:\n\tremote_read\n\tremote_write",
		},
		{
			name: "prometheus_unsupported_rule_files",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: prometheus
            static_configs:
              - targets: ['localhost:1234']
        rule_files:
          - "rules.yml"
`,
			wantErr: "unsupported features:\n\trule_files",
		},
		{
			name: "prometheus_honor_labels",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: prometheus
            honor_labels: true
            static_configs:
              - targets: ['localhost:1234']
`,
			wantErr: `error validating scrape_config for job prometheus: honor_labels is not supported`,
		},
		{
			name: "prometheus_invalid_relabel_location",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: prometheus
            static_configs:
              - targets: ['localhost:1234']
            relabel_configs:
              - source_labels: [__address__]
                target_label: location
`,
			wantErr: `error validating scrape_config for job prometheus: relabel_configs cannot rename location, namespace or cluster`,
		},
		{
			name: "prometheus_invalid_metric_relabel_name",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: prometheus
            static_configs:
              - targets: ['localhost:1234']
            metric_relabel_configs:
              - source_labels: [foo]
                target_label: __name__
`,
			wantErr: `error validating scrape_config for job prometheus: metric_relabel_configs cannot rename __name__`,
		},
		{
			name: "prometheus_invalid_metric_relabel_location",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: prometheus
            static_configs:
              - targets: ['localhost:1234']
            metric_relabel_configs:
              - source_labels: [new_location]
                target_label: location
`,
			wantErr: `error validating scrape_config for job prometheus: metric_relabel_configs cannot rename location, namespace or cluster`,
		},
		{
			name: "prometheus_missing_tls_cert",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: node
            static_configs:
              - targets: ['localhost:1234']
            tls_config:
              cert_file: /path/to/nonexistent/cert
              key_file: /path/to/nonexistent/key
`,
			wantErr: `error checking client cert file "/path/to/nonexistent/cert": file "/path/to/nonexistent/cert" does not exist`,
		},
		{
			name: "prometheus_cert_without_key",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: node
            static_configs:
              - targets: ['localhost:1234']
            tls_config:
              cert_file: ` + certFile + `
`,
			wantErr: `exactly one of key or key_file must be configured when a client certificate is configured`,
		},
		{
			name: "prometheus_key_without_cert",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: node
            static_configs:
              - targets: ['localhost:1234']
            tls_config:
              key_file: ` + keyFile + `
`,
			wantErr: `exactly one of cert or cert_file must be configured when a client key is configured`,
		},
		{
			name: "prometheus_missing_credentials_file",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: node
            static_configs:
              - targets: ['localhost:1234']
            authorization:
              credentials_file: /path/to/nonexistent/token
`,
			wantErr: `error checking authorization credentials file "/path/to/nonexistent/token": file "/path/to/nonexistent/token" does not exist`,
		},
		{
			name: "prometheus_unsupported_service_discovery",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: prometheus
            file_sd_configs:
              - files:
                  - nonexistent_file.yml
`,
			wantErr: `unsupported service discovery config *file.SDConfig`,
		},
		{
			name: "prometheus_incompatible_with_ops_agent_processors",
			config: `metrics:
  processors:
    metrics_filter:
      type: exclude_metrics
      metrics_pattern:
        - agent.googleapis.com/gpu/utilization
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: prometheus
            static_configs:
              - targets: ['localhost:1234']
  service:
    pipelines:
      prometheus_pipeline:
        receivers: [prometheus]
        processors: [metrics_filter]
`,
			wantErr: `prometheus receiver is incompatible with Ops Agent processors`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			configFile, _ := writeTestConfig(t, tc.config)
			p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
			_, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

func TestRetrievePrometheusPipeline(t *testing.T) {
	userYAML := `metrics:
  receivers:
    prom_app:
      type: prometheus
      config:
        scrape_configs:
          - job_name: fast_job
            scrape_interval: 1s
            static_configs:
              - targets: ['localhost:1234']
            relabel_configs:
              - source_labels: [__address__]
                regex: '(.+)'
                replacement: '${1}'
                target_label: instance
          - job_name: normal_job
            scrape_interval: 15s
            static_configs:
              - targets: ['localhost:5678']
  service:
    pipelines:
      prom_pipe:
        receivers: [prom_app]
`
	configFile, _ := writeTestConfig(t, userYAML)
	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)

	scrapeConfigs, ok := conf.Get("receivers::prometheus/prom__app::config::scrape_configs").([]any)
	require.True(t, ok)
	require.Len(t, scrapeConfigs, 2)

	fastJob, ok := scrapeConfigs[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "10s", fastJob["scrape_interval"])

	relabelConfigs, ok := fastJob["relabel_configs"].([]any)
	require.True(t, ok)
	require.Len(t, relabelConfigs, 1)
	rcMap, ok := relabelConfigs[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "${1}", rcMap["replacement"])

	normalJob, ok := scrapeConfigs[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "15s", normalJob["scrape_interval"])

	assert.Equal(t, []string{"prometheus/prom__app"}, conf.Get("service::pipelines::metrics/prom__pipe_prom__app::receivers"))
	assert.Equal(t, []string{
		"resourcedetection/prom__app_0",
		"transform/prom__app_1",
		"groupbyattrs/prom__app_2",
		"transform/prom__app_3",
		"metricstransform/prom__app_4",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}, conf.Get("service::pipelines::metrics/prom__pipe_prom__app::processors"))
}

func TestPrometheusMetricsTransformation(t *testing.T) {
	configYAML := `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: test
            static_configs:
              - targets: ['localhost:1234']
  service:
    pipelines:
      prom_pipe:
        receivers: [prometheus]
`
	chain, sink := buildProcessorChainWithConfig(t, configYAML, []string{
		"transform/prometheus_1",
		"groupbyattrs/prometheus_2",
		"transform/prometheus_3",
		"metricstransform/prometheus_4",
	})

	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	resAttrs := rm.Resource().Attributes()
	resAttrs.PutStr("cloud.platform", "gcp_compute_engine")
	resAttrs.PutStr("cloud.availability_zone", "us-east1-b")
	resAttrs.PutStr("host.id", "99887766")
	resAttrs.PutStr("host.name", "prom-vm")
	resAttrs.PutStr("host.type", "n2-standard-2")

	sm := rm.ScopeMetrics().AppendEmpty()

	mUnknown := sm.Metrics().AppendEmpty()
	mUnknown.SetName("untyped_requests_total")
	mUnknown.Metadata().PutStr("prometheus.type", "unknown")
	dp := mUnknown.SetEmptyGauge().DataPoints().AppendEmpty()
	dp.SetDoubleValue(100)

	require.NoError(t, chain.ConsumeMetrics(context.Background(), md))

	allMetrics := sink.AllMetrics()
	require.NotEmpty(t, allMetrics)

	var sawGauge, sawSum bool
	for _, batch := range allMetrics {
		for i := 0; i < batch.ResourceMetrics().Len(); i++ {
			rmOut := batch.ResourceMetrics().At(i)
			rAttrs := rmOut.Resource().Attributes()
			loc, _ := rAttrs.Get("location")
			assert.Equal(t, "us-east1-b", loc.Str())
			ns, _ := rAttrs.Get("namespace")
			assert.Equal(t, "99887766/prom-vm", ns.Str())
			cluster, _ := rAttrs.Get("cluster")
			assert.Equal(t, "__gce__", cluster.Str())

			for j := 0; j < rmOut.ScopeMetrics().Len(); j++ {
				metrics := rmOut.ScopeMetrics().At(j).Metrics()
				for k := 0; k < metrics.Len(); k++ {
					m := metrics.At(k)
					if m.Name() == "prometheus.googleapis.com/untyped_requests_total" {
						switch m.Type() {
						case pmetric.MetricTypeGauge:
							sawGauge = true
						case pmetric.MetricTypeSum:
							sawSum = true
						}
					}
				}
			}
		}
	}

	assert.True(t, sawGauge)
	assert.True(t, sawSum)
}
