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

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/processor/agentmetricsprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/processor/filterprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/processor/normalizesumsprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/processor/transformprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/receiver/dcgmreceiver"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/otelopscol/receiver/nvmlreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/googleclientauthextension"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/storage/filestorage"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/cumulativetodeltaprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatorateprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/groupbyattrsprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/intervalprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstarttimeprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourceprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/filelogreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/fluentforwardreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/iisreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/journaldreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/sqlserverreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/syslogreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowseventlogreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"
	"go.uber.org/zap"
)

func testFactories(t *testing.T) otelcol.Factories {
	t.Helper()

	var err error
	factories := otelcol.Factories{
		Telemetry: otelconftelemetry.NewFactory(),
	}

	factories.Receivers, err = otelcol.MakeFactoryMap[receiver.Factory](
		dcgmreceiver.NewFactory(),
		filelogreceiver.NewFactory(),
		fluentforwardreceiver.NewFactory(),
		hostmetricsreceiver.NewFactory(),
		iisreceiver.NewFactory(),
		journaldreceiver.NewFactory(),
		nvmlreceiver.NewFactory(),
		otlpreceiver.NewFactory(),
		prometheusreceiver.NewFactory(),
		sqlserverreceiver.NewFactory(),
		syslogreceiver.NewFactory(),
		windowseventlogreceiver.NewFactory(),
		windowsperfcountersreceiver.NewFactory(),
	)
	require.NoError(t, err)

	factories.Processors, err = otelcol.MakeFactoryMap[processor.Factory](
		agentmetricsprocessor.NewFactory(),
		batchprocessor.NewFactory(),
		cumulativetodeltaprocessor.NewFactory(),
		deltatorateprocessor.NewFactory(),
		filterprocessor.NewFactory(),
		groupbyattrsprocessor.NewFactory(),
		intervalprocessor.NewFactory(),
		metricstarttimeprocessor.NewFactory(),
		metricstransformprocessor.NewFactory(),
		normalizesumsprocessor.NewFactory(),
		resourcedetectionprocessor.NewFactory(),
		resourceprocessor.NewFactory(),
		transformprocessor.NewFactory(),
	)
	require.NoError(t, err)

	factories.Exporters, err = otelcol.MakeFactoryMap[exporter.Factory](
		otlpexporter.NewFactory(),
	)
	require.NoError(t, err)

	factories.Extensions, err = otelcol.MakeFactoryMap[extension.Factory](
		filestorage.NewFactory(),
		googleclientauthextension.NewFactory(),
	)
	require.NoError(t, err)

	return factories
}

func writeTestConfig(t *testing.T, content string) (configFile string, stateDir string) {
	t.Helper()
	tmpDir := t.TempDir()
	configFile = filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0600))

	stateDir = filepath.Join(tmpDir, "state")
	t.Setenv("STATE_DIRECTORY", stateDir)
	return configFile, stateDir
}

func TestRetrieveEmptyConfig(t *testing.T) {
	configFile, stateDir := writeTestConfig(t, "")

	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	assert.Equal(t, "opsagentconf", p.Scheme())

	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)

	rawMap := conf.ToStringMap()
	assert.Contains(t, rawMap, "receivers")
	assert.Contains(t, rawMap, "processors")
	assert.Contains(t, rawMap, "exporters")
	assert.Contains(t, rawMap, "extensions")
	assert.Contains(t, rawMap, "service")

	assert.True(t, conf.IsSet("receivers::prometheus/agent_prometheus"))
	assert.True(t, conf.IsSet("receivers::hostmetrics/hostmetrics"))
	assert.True(t, conf.IsSet("receivers::file_log/logging_syslog"))
	assert.True(t, conf.IsSet("processors::resourcedetection/_global_0"))
	assert.True(t, conf.IsSet("exporters::otlp_grpc/otlp_metrics"))
	assert.True(t, conf.IsSet("exporters::otlp_grpc/otlp_logs"))
	expectedUserAgent := detectHostInfo().userAgent()
	assert.Equal(t, expectedUserAgent, conf.Get("exporters::otlp_grpc/otlp_metrics::user_agent"))
	assert.Equal(t, expectedUserAgent, conf.Get("exporters::otlp_grpc/otlp_logs::user_agent"))
	assert.Equal(t, filepath.Join(stateDir, "file_storage"), conf.Get("extensions::file_storage::directory"))
	assert.True(t, conf.IsSet("service::telemetry::metrics"))

	assert.Equal(t, []string{
		"transform/logging_syslog_0",
		"resourcedetection/_global_0",
		"resource/otlp_grpc/otlp_logs_logs_1",
		"transform/otlp_grpc/otlp_logs_logs_2",
		"transform/otlp_grpc/otlp_logs_logs_3",
	}, conf.Get("service::pipelines::logs/logs_default__pipeline_logging_syslog::processors"))

	assert.Equal(t, []string{
		"transform/agent_prometheus_0",
		"transform/agent_prometheus_1",
		"transform/agent_prometheus_2",
		"transform/otel_0",
		"filter/otel_1",
		"filter/otel_2",
		"metricstransform/otel_3",
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}, conf.Get("service::pipelines::metrics/otel::processors"))

	assert.Equal(t, []string{
		"transform/agent_prometheus_0",
		"transform/agent_prometheus_1",
		"transform/agent_prometheus_2",
		"transform/loggingmetrics_0",
		"filter/loggingmetrics_1",
		"filter/loggingmetrics_2",
		"metricstransform/loggingmetrics_3",
		"transform/loggingmetrics_4",
		"interval/loggingmetrics_5",
		"metricstransform/loggingmetrics_6",
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}, conf.Get("service::pipelines::metrics/loggingmetrics::processors"))

	assert.Equal(t, []string{
		"agentmetrics/hostmetrics_0",
		"filter/hostmetrics_1",
		"metricstransform/hostmetrics_2",
		"transform/hostmetrics_3",
		"transform/hostmetrics_4",
		"filter/default__pipeline_hostmetrics_0",
		"resourcedetection/_global_0",
		"metric_start_time/otlp_grpc/otlp_metrics_metrics_1",
		"batch/otlp_grpc/otlp_metrics_metrics_2",
	}, conf.Get("service::pipelines::metrics/default__pipeline_hostmetrics::processors"))

	require.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieveNonExistentConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	missingConfig := filepath.Join(tmpDir, "does_not_exist.yaml")
	t.Setenv("STATE_DIRECTORY", filepath.Join(tmpDir, "state"))

	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+missingConfig, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)
	assert.True(t, conf.IsSet("service::pipelines::logs/logs_default__pipeline_logging_syslog"))
	assert.True(t, conf.IsSet("service::pipelines::metrics/otel"))
	assert.True(t, conf.IsSet("service::pipelines::metrics/loggingmetrics"))
	assert.True(t, conf.IsSet("service::pipelines::metrics/default__pipeline_hostmetrics"))
}

func TestRetrieveInvalidYAML(t *testing.T) {
	configFile, _ := writeTestConfig(t, "logging:\n  - invalid: [unclosed\n")

	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	_, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.ErrorContains(t, err, "failed to read config file")
}

func TestRetrieveDefaultStateDir(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(""), 0600))
	t.Setenv("STATE_DIRECTORY", "")

	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(defaultStateDir(), "file_storage"), conf.Get("extensions::file_storage::directory"))
}

func TestValidateCollectorConfig(t *testing.T) {
	testCases := []struct {
		name      string
		isWindows bool
		hasGPU    bool
		config    string
	}{
		{
			name:   "empty_config",
			config: "",
		},
		{
			name: "empty_sections",
			config: `combined: {}
logging: {}
metrics: {}
traces: {}
`,
		},
		{
			name: "custom_hostmetrics_and_exclude_metrics",
			config: `metrics:
  receivers:
    hostmetrics:
      type: hostmetrics
      collection_interval: 30s
  processors:
    metrics_filter:
      type: exclude_metrics
      metrics_pattern:
        - agent.googleapis.com/processes/*
        - agent.googleapis.com/cpu/*
  service:
    log_level: debug
    pipelines:
      default_pipeline:
        receivers: [hostmetrics]
        processors: [metrics_filter]
`,
		},
		{
			name: "disable_default_metrics_pipeline",
			config: `metrics:
  service:
    pipelines:
      default_pipeline:
        receivers: []
`,
		},
		{
			name: "combined_otlp_gmp_and_traces",
			config: `combined:
  receivers:
    otlp:
      type: otlp
      grpc_endpoint: 127.0.0.1:4317
      metrics_mode: googlemanagedprometheus
metrics:
  service:
    pipelines:
      otlp_pipe:
        receivers: [otlp]
traces:
  service:
    pipelines:
      otlp_pipe:
        receivers: [otlp]
`,
		},
		{
			name: "combined_otlp_gcm_with_exclude_metrics",
			config: `combined:
  receivers:
    otlp:
      type: otlp
      metrics_mode: googlecloudmonitoring
metrics:
  processors:
    filter_custom:
      type: exclude_metrics
      metrics_pattern:
        - workload.googleapis.com/secret/*
  service:
    pipelines:
      otlp_pipe:
        receivers: [otlp]
        processors: [filter_custom]
traces:
  service:
    pipelines: {}
`,
		},
		{
			name: "prometheus_metrics_receiver",
			config: `metrics:
  receivers:
    prometheus:
      type: prometheus
      config:
        scrape_configs:
          - job_name: node
            scrape_interval: 10s
            static_configs:
              - targets: ['localhost:1234']
            relabel_configs:
              - source_labels: [__address__]
                regex: '(.+)'
                replacement: '${1}'
                target_label: instance
            metric_relabel_configs:
              - source_labels: [source]
                regex: '(.*)@(.*)'
                replacement: '${2}/${1}'
                target_label: destination
  service:
    pipelines:
      prometheus_pipeline:
        receivers: [prometheus]
`,
		},
		{
			name:      "windows_default_metrics",
			isWindows: true,
			config:    "",
		},
		{
			name:      "windows_iis_and_mssql_v1_and_v2",
			isWindows: true,
			config: `metrics:
  receivers:
    iis_v2:
      type: iis
      receiver_version: 2
    mssql_v2:
      type: mssql
      receiver_version: 2
  service:
    pipelines:
      iispipeline:
        receivers: [iis_v2]
      mssql_v2:
        receivers: [mssql_v2]
`,
		},
		{
			name:   "linux_gpu_default_metrics",
			hasGPU: true,
			config: "",
		},
		{
			name: "linux_dcgm_v1_and_v2",
			config: `metrics:
  receivers:
    dcgm:
      type: dcgm
    dcgm_v2:
      type: dcgm
      receiver_version: 2
      endpoint: 127.0.0.1:5556
  service:
    pipelines:
      dcgm:
        receivers: [dcgm, dcgm_v2]
`,
		},
		{
			name: "custom_files_logging_pipeline",
			config: `logging:
  receivers:
    app_logs:
      type: files
      include_paths:
        - /var/log/app/*.log
      exclude_paths:
        - /var/log/app/debug.log
      wildcard_refresh_interval: 30s
      record_log_file_path: true
  service:
    log_level: trace
    pipelines:
      default_pipeline:
        receivers: [syslog, app_logs]
`,
		},
		{
			name: "linux_network_and_journald_logging_and_otlp_logs",
			config: `combined:
  receivers:
    otlp:
      type: otlp
logging:
  receivers:
    syslog_tcp:
      type: syslog
      transport_protocol: tcp
      listen_host: 127.0.0.1
      listen_port: 5140
    syslog_udp:
      type: syslog
      transport_protocol: udp
      listen_host: 0.0.0.0
      listen_port: 5141
    fluent_default:
      type: fluent_forward
    systemd_logs:
      type: systemd_journald
  service:
    pipelines:
      default_pipeline:
        receivers: [syslog, syslog_tcp, syslog_udp, fluent_default, systemd_logs, otlp]
traces:
  service:
    pipelines:
      otlp_traces:
        receivers: [otlp]
`,
		},
		{
			name: "logging_processors_and_builtin_libs",
			config: `logging:
  processors:
    json_proc:
      type: parse_json
      field: message
      time_key: timestamp
      time_format: "%Y-%m-%dT%H:%M:%S.%LZ"
    regex_proc:
      type: parse_regex
      field: jsonPayload.message
      regex: "^(?<time>[^ ]+) (?<severity>[^ ]+) (?<msg>.*)$"
      time_key: time
      time_format: "%Y-%m-%dT%H:%M:%SZ"
    exclude_proc:
      type: exclude_logs
      match_any:
        - severity = "DEBUG"
        - jsonPayload.msg =~ "^(?<ignore>healthcheck)$"
    modify_proc:
      type: modify_fields
      fields:
        severity:
          copy_from: jsonPayload.level
          map_values:
            err: ERROR
            warn: WARNING
          map_values_exclusive: true
        jsonPayload.port:
          move_from: jsonPayload.raw_port
          type: integer
          omit_if: jsonPayload.port = 0
        labels.env:
          static_value: prod
  service:
    pipelines:
      default_pipeline:
        receivers: [syslog]
        processors:
          - json_proc
          - regex_proc
          - exclude_proc
          - modify_proc
          - lib:apache
          - lib:apache2
          - lib:apache_error
          - lib:mongodb
          - lib:nginx
          - lib:syslog-rfc3164
          - lib:syslog-rfc5424
`,
		},
		{
			name:      "windows_event_log_v1_v2_and_xml",
			isWindows: true,
			config: `logging:
  receivers:
    winlog2:
      type: windows_event_log
      receiver_version: 2
      channels:
        - System
        - Application
        - Microsoft-Windows-Windows Defender/Operational
    winlog2_xml:
      type: windows_event_log
      receiver_version: 2
      render_as_xml: true
      channels:
        - Security
  service:
    pipelines:
      winlog2:
        receivers: [winlog2, winlog2_xml]
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			configFile, _ := writeTestConfig(t, tc.config)

			factories := testFactories(t)
			info := detectHostInfo()
			info.HasNvidiaGPU = tc.hasGPU
			if tc.isWindows {
				info = hostInfo{OS: "windows", Platform: "Microsoft Windows Server 2022 Datacenter", PlatformVersion: "10.0.20348 Build 20348"}
			}
			providerFactory := confmap.NewProviderFactory(func(set confmap.ProviderSettings) confmap.Provider {
				return &provider{
					logger:   set.Logger,
					hostInfo: info,
				}
			})

			configProvider, err := otelcol.NewConfigProvider(otelcol.ConfigProviderSettings{
				ResolverSettings: confmap.ResolverSettings{
					URIs: []string{"opsagentconf:" + configFile},
					ProviderFactories: []confmap.ProviderFactory{
						providerFactory,
					},
				},
			})
			require.NoError(t, err)

			cfg, err := configProvider.Get(context.Background(), factories)
			require.NoError(t, err)
			require.NotNil(t, cfg)
			require.NoError(t, cfg.Validate())
			require.NoError(t, configProvider.Shutdown(context.Background()))
		})
	}
}

func TestRetrieveUnsupportedScheme(t *testing.T) {
	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	_, err := p.Retrieve(context.Background(), "file:/etc/config.yaml", nil)
	require.ErrorContains(t, err, "is not supported")
}
