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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/processor/processortest"
	"go.uber.org/zap"
)

func TestMergeLoggingConfig(t *testing.T) {
	t.Run("nil_user_config_returns_linux_defaults", func(t *testing.T) {
		merged := mergeLoggingConfig(nil, false)
		assert.Equal(t, map[string]LoggingReceiver{
			"syslog": {
				Type:         "files",
				IncludePaths: []string{"/var/log/messages", "/var/log/syslog"},
			},
		}, merged.Receivers)
		assert.Empty(t, merged.Processors)
		require.NotNil(t, merged.Service)
		assert.Equal(t, &Pipeline{
			ReceiverIDs: []string{"syslog"},
		}, merged.Service.Pipelines["default_pipeline"])
	})

	t.Run("nil_user_config_returns_windows_defaults", func(t *testing.T) {
		merged := mergeLoggingConfig(nil, true)
		assert.Empty(t, merged.Receivers)
		assert.Empty(t, merged.Processors)
		require.NotNil(t, merged.Service)
		assert.Equal(t, &Pipeline{}, merged.Service.Pipelines["default_pipeline"])
	})

	t.Run("override_builtin_receiver_and_log_level", func(t *testing.T) {
		merged := mergeLoggingConfig(&Logging{
			Receivers: map[string]LoggingReceiver{
				"syslog": {
					Type:         "files",
					IncludePaths: []string{"/var/log/custom.log"},
				},
			},
			Service: &LoggingService{
				LogLevel: "debug",
			},
		}, false)
		assert.Equal(t, []string{"/var/log/custom.log"}, merged.Receivers["syslog"].IncludePaths)
		assert.Equal(t, "debug", merged.Service.LogLevel)
		assert.Equal(t, &Pipeline{
			ReceiverIDs: []string{"syslog"},
		}, merged.Service.Pipelines["default_pipeline"])
	})

	t.Run("disable_default_pipeline", func(t *testing.T) {
		merged := mergeLoggingConfig(&Logging{
			Service: &LoggingService{
				LogLevel: "info",
				Pipelines: map[string]*Pipeline{
					"default_pipeline": {
						ReceiverIDs: []string{},
					},
					"empty_pipeline": nil,
				},
			},
		}, false)
		assert.Empty(t, merged.Service.LogLevel)
		assert.Empty(t, merged.Service.Pipelines["default_pipeline"].ReceiverIDs)
		assert.NotNil(t, merged.Service.Pipelines["empty_pipeline"])
	})
}

func TestLoggingValidate(t *testing.T) {
	require.NoError(t, defaultLoggingConfig(false).validate(nil, false))
	require.NoError(t, defaultLoggingConfig(true).validate(nil, true))

	boolTrue := true
	testCases := []struct {
		name        string
		isWindows   bool
		logging     *Logging
		combined    *Combined
		expectedErr string
	}{
		{
			name: "receiver_id_starts_with_lib",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"lib:syslog": {
						Type:         "files",
						IncludePaths: []string{"/var/log/syslog"},
					},
				},
			},
			expectedErr: `logging receiver ID "lib:syslog" cannot start with "lib:"`,
		},
		{
			name: "unsupported_receiver_type",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"custom": {Type: "unknown"},
				},
			},
			expectedErr: `logging receiver "custom" with type "unknown" is not supported`,
		},
		{
			name: "files_missing_include_paths",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"empty_files": {Type: "files"},
				},
			},
			expectedErr: `logging receiver "empty_files" with type "files" requires non-empty include_paths`,
		},
		{
			name: "files_with_network_field",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"bad_files": {
						Type:         "files",
						IncludePaths: []string{"/var/log/syslog"},
						ListenPort:   5140,
					},
				},
			},
			expectedErr: `logging receiver "bad_files" with type "files" does not support listen_port`,
		},
		{
			name: "files_hostname_variable_in_include_paths",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"hostname_files": {
						Type:         "files",
						IncludePaths: []string{"/var/log/${HOSTNAME}/app.log"},
					},
				},
			},
			expectedErr: `logging receiver "hostname_files" with type "files" does not support ${HOSTNAME} in paths`,
		},
		{
			name: "files_hostname_variable_in_exclude_paths",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"hostname_exclude": {
						Type:         "files",
						IncludePaths: []string{"/var/log/*.log"},
						ExcludePaths: []string{"/var/log/${HOSTNAME}.log"},
					},
				},
			},
			expectedErr: `logging receiver "hostname_exclude" with type "files" does not support ${HOSTNAME} in paths`,
		},
		{
			name: "files_invalid_wildcard_refresh_interval_too_short",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"short_refresh": {
						Type:                    "files",
						IncludePaths:            []string{"/var/log/*.log"},
						WildcardRefreshInterval: "500ms",
					},
				},
			},
			expectedErr: `logging receiver "short_refresh" has invalid wildcard_refresh_interval "500ms": must be a duration >= 1s and a multiple of 1s`,
		},
		{
			name: "files_invalid_wildcard_refresh_interval_not_multiple_of_1s",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"fractional_refresh": {
						Type:                    "files",
						IncludePaths:            []string{"/var/log/*.log"},
						WildcardRefreshInterval: "1500ms",
					},
				},
			},
			expectedErr: `logging receiver "fractional_refresh" has invalid wildcard_refresh_interval "1500ms": must be a duration >= 1s and a multiple of 1s`,
		},
		{
			name: "syslog_invalid_transport_protocol",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"syslog_bad_proto": {
						Type:              "syslog",
						TransportProtocol: "http",
						ListenHost:        "127.0.0.1",
						ListenPort:        5140,
					},
				},
			},
			expectedErr: `logging receiver "syslog_bad_proto" with type "syslog" has invalid transport_protocol "http": must be one of [tcp udp]`,
		},
		{
			name: "syslog_invalid_listen_host",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"syslog_bad_host": {
						Type:              "syslog",
						TransportProtocol: "tcp",
						ListenHost:        "not-an-ip",
						ListenPort:        5140,
					},
				},
			},
			expectedErr: `logging receiver "syslog_bad_host" with type "syslog" has invalid listen_host "not-an-ip": must be a valid IP address`,
		},
		{
			name: "syslog_missing_listen_port",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"syslog_zero_port": {
						Type:              "syslog",
						TransportProtocol: "udp",
						ListenHost:        "127.0.0.1",
					},
				},
			},
			expectedErr: `logging receiver "syslog_zero_port" with type "syslog" requires non-zero listen_port`,
		},
		{
			name: "syslog_with_files_field",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"syslog_with_paths": {
						Type:              "syslog",
						TransportProtocol: "tcp",
						ListenHost:        "127.0.0.1",
						ListenPort:        5140,
						IncludePaths:      []string{"/var/log/syslog"},
					},
				},
			},
			expectedErr: `logging receiver "syslog_with_paths" with type "syslog" does not support include_paths`,
		},
		{
			name: "fluent_forward_invalid_listen_host",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"forward_bad_host": {
						Type:       "fluent_forward",
						ListenHost: "localhost",
					},
				},
			},
			expectedErr: `logging receiver "forward_bad_host" with type "fluent_forward" has invalid listen_host "localhost": must be a valid IP address`,
		},
		{
			name: "fluent_forward_with_transport_protocol",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"forward_proto": {
						Type:              "fluent_forward",
						TransportProtocol: "tcp",
					},
				},
			},
			expectedErr: `logging receiver "forward_proto" with type "fluent_forward" does not support transport_protocol`,
		},
		{
			name:      "systemd_journald_on_windows",
			isWindows: true,
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"journald": {Type: "systemd_journald"},
				},
			},
			expectedErr: `logging receiver "journald" with type "systemd_journald" is not supported`,
		},
		{
			name: "systemd_journald_with_extra_fields",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"journald": {
						Type:              "systemd_journald",
						RecordLogFilePath: &boolTrue,
					},
				},
			},
			expectedErr: `logging receiver "journald" with type "systemd_journald" does not support record_log_file_path`,
		},
		{
			name: "duplicate_receiver_name_with_combined",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"otlp": {
						Type:         "files",
						IncludePaths: []string{"/var/log/syslog"},
					},
				},
			},
			combined: &Combined{
				Receivers: map[string]CombinedReceiver{
					"otlp": {Type: "otlp"},
				},
			},
			expectedErr: `logging receiver "otlp" has the same name as combined receiver "otlp"`,
		},
		{
			name: "processor_id_starts_with_lib",
			logging: &Logging{
				Processors: map[string]LoggingProcessor{
					"lib:apache": {Type: "parse_json"},
				},
			},
			expectedErr: `logging processor ID "lib:apache" cannot start with "lib:"`,
		},
		{
			name: "unsupported_processor_type",
			logging: &Logging{
				Processors: map[string]LoggingProcessor{
					"custom_proc": {Type: "unknown"},
				},
			},
			expectedErr: `logging processor "custom_proc" with type "unknown" is not supported`,
		},
		{
			name: "invalid_log_level",
			logging: &Logging{
				Service: &LoggingService{
					LogLevel: "verbose",
				},
			},
			expectedErr: `logging service has invalid log_level "verbose"`,
		},
		{
			name: "pipeline_id_starts_with_lib",
			logging: &Logging{
				Service: &LoggingService{
					Pipelines: map[string]*Pipeline{
						"lib:pipe": {},
					},
				},
			},
			expectedErr: `logging pipeline ID "lib:pipe" cannot start with "lib:"`,
		},
		{
			name: "undefined_pipeline_receiver",
			logging: &Logging{
				Service: &LoggingService{
					Pipelines: map[string]*Pipeline{
						"default_pipeline": {
							ReceiverIDs: []string{"missing_receiver"},
						},
					},
				},
			},
			expectedErr: `logging receiver "missing_receiver" from pipeline "default_pipeline" is not defined`,
		},
		{
			name: "undefined_pipeline_processor",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"syslog": {
						Type:         "files",
						IncludePaths: []string{"/var/log/syslog"},
					},
				},
				Service: &LoggingService{
					Pipelines: map[string]*Pipeline{
						"default_pipeline": {
							ReceiverIDs:  []string{"syslog"},
							ProcessorIDs: []string{"missing_proc"},
						},
					},
				},
			},
			expectedErr: `logging processor "missing_proc" from pipeline "default_pipeline" is not defined`,
		},
		{
			name: "network_receiver_used_in_two_pipelines",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"forward": {
						Type: "fluent_forward",
					},
				},
				Service: &LoggingService{
					Pipelines: map[string]*Pipeline{
						"pipe_a": {ReceiverIDs: []string{"forward"}},
						"pipe_b": {ReceiverIDs: []string{"forward"}},
					},
				},
			},
			expectedErr: `logging receiver "forward" listening on port 24224 cannot be used in two pipelines`,
		},
		{
			name: "two_network_receivers_on_same_port",
			logging: &Logging{
				Receivers: map[string]LoggingReceiver{
					"syslog_1": {
						Type:              "syslog",
						TransportProtocol: "tcp",
						ListenHost:        "127.0.0.1",
						ListenPort:        24224,
					},
					"forward_1": {
						Type: "fluent_forward",
					},
				},
				Service: &LoggingService{
					Pipelines: map[string]*Pipeline{
						"pipe_a": {ReceiverIDs: []string{"syslog_1", "forward_1"}},
					},
				},
			},
			expectedErr: `two logging receivers "syslog_1" and "forward_1" cannot listen on the same port 24224`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.logging.validate(tc.combined, tc.isWindows)
			require.ErrorContains(t, err, tc.expectedErr)
		})
	}
}

func TestRetrieveFilesLoggingPipelines(t *testing.T) {
	configYAML := `logging:
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
        receivers: [syslog]
      app_pipeline:
        receivers: [app_logs]
`
	configFile, _ := writeTestConfig(t, configYAML)

	p := &provider{
		logger:   zap.NewNop(),
		hostInfo: hostInfo{OS: "linux", Platform: "debian", PlatformVersion: "12", Hostname: "test-vm"},
	}
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"include":                       []string{"/var/log/messages", "/var/log/syslog"},
		"exclude":                       []string{},
		"start_at":                      "beginning",
		"include_file_name":             false,
		"preserve_leading_whitespaces":  true,
		"preserve_trailing_whitespaces": true,
		"fingerprint_size":              "5kb",
		"storage":                       "file_storage",
		"operators": []map[string]any{
			{
				"id":   "body",
				"type": "move",
				"from": "body",
				"to":   "body.message",
			},
		},
	}, conf.Get("receivers::file_log/logging_syslog"))

	assert.Equal(t, map[string]any{
		"include":                       []string{"/var/log/app/*.log"},
		"exclude":                       []string{"/var/log/app/debug.log"},
		"poll_interval":                 "30s",
		"start_at":                      "beginning",
		"include_file_name":             false,
		"include_file_path":             true,
		"preserve_leading_whitespaces":  true,
		"preserve_trailing_whitespaces": true,
		"fingerprint_size":              "5kb",
		"storage":                       "file_storage",
		"operators": []map[string]any{
			{
				"id":   "body",
				"type": "move",
				"from": "body",
				"to":   "body.message",
			},
			{
				"id":   "record_log_file_path",
				"type": "move",
				"from": `attributes["log.file.path"]`,
				"to":   `attributes["agent.googleapis.com/log_file_path"]`,
			},
		},
	}, conf.Get("receivers::file_log/logging_app__logs"))

	assert.Equal(t, []string{
		"transform/logging_app__logs_0",
		"resourcedetection/_global_0",
		"resource/otlp_grpc/otlp_logs_logs_1",
		"transform/otlp_grpc/otlp_logs_logs_2",
		"transform/otlp_grpc/otlp_logs_logs_3",
	}, conf.Get("service::pipelines::logs/logs_app__pipeline_logging_app__logs::processors"))
}

func TestRetrieveNetworkAndJournaldLoggingPipelines(t *testing.T) {
	configYAML := `combined:
  receivers:
    otlp_in:
      type: otlp
      grpc_endpoint: 127.0.0.1:4317
logging:
  receivers:
    syslog_tcp:
      type: syslog
      transport_protocol: tcp
      listen_host: 0.0.0.0
      listen_port: 5141
    fluent_default:
      type: fluent_forward
    fluent_custom:
      type: fluent_forward
      listen_host: 127.0.0.2
      listen_port: 24225
    systemd_logs:
      type: systemd_journald
  service:
    pipelines:
      custom_pipe:
        receivers: [syslog_tcp, fluent_default, fluent_custom, systemd_logs, otlp_in]
traces:
  service:
    pipelines: {}
`
	configFile, _ := writeTestConfig(t, configYAML)

	p := &provider{
		logger:   zap.NewNop(),
		hostInfo: hostInfo{OS: "linux", Platform: "debian", PlatformVersion: "12", Hostname: "test-vm"},
	}
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"protocol": "rfc5424",
		"tcp": map[string]any{
			"listen_address": "0.0.0.0:5141",
		},
	}, conf.Get("receivers::syslog/logging_syslog__tcp"))
	assert.Equal(t, []string{
		"transform/logging_syslog__tcp_0",
		"transform/logging_syslog__tcp_1",
		"resourcedetection/_global_0",
		"resource/otlp_grpc/otlp_logs_logs_1",
		"transform/otlp_grpc/otlp_logs_logs_2",
		"transform/otlp_grpc/otlp_logs_logs_3",
	}, conf.Get("service::pipelines::logs/logs_custom__pipe_logging_syslog__tcp::processors"))

	assert.Equal(t, map[string]any{
		"endpoint": "127.0.0.1:24224",
	}, conf.Get("receivers::fluentforward/logging_fluent__default"))
	assert.Equal(t, map[string]any{
		"endpoint": "127.0.0.2:24225",
	}, conf.Get("receivers::fluentforward/logging_fluent__custom"))
	assert.Equal(t, []string{
		"transform/logging_fluent__default_0",
		"transform/logging_fluent__default_1",
		"transform/logging_fluent__default_2",
		"resourcedetection/_global_0",
		"resource/otlp_grpc/otlp_logs_logs_1",
		"transform/otlp_grpc/otlp_logs_logs_2",
		"transform/otlp_grpc/otlp_logs_logs_3",
	}, conf.Get("service::pipelines::logs/logs_custom__pipe_logging_fluent__default::processors"))

	assert.Equal(t, map[string]any{
		"priority": "debug",
		"start_at": "beginning",
		"storage":  "file_storage",
	}, conf.Get("receivers::journald/logging_systemd__logs"))
	assert.Equal(t, []string{
		"transform/logging_systemd__logs_0",
		"transform/logging_systemd__logs_1",
		"resourcedetection/_global_0",
		"resource/otlp_grpc/otlp_logs_logs_1",
		"transform/otlp_grpc/otlp_logs_logs_2",
		"transform/otlp_grpc/otlp_logs_logs_3",
	}, conf.Get("service::pipelines::logs/logs_custom__pipe_logging_systemd__logs::processors"))

	assert.Equal(t, map[string]any{
		"protocols": map[string]any{
			"grpc": map[string]any{
				"endpoint": "127.0.0.1:4317",
			},
		},
	}, conf.Get("receivers::otlp/otlp__in"))
	assert.Equal(t, []string{
		"transform/otlp__in_0",
		"resourcedetection/_global_1",
		"resource/otlp_grpc/otlp_logs_logs_1",
		"transform/otlp_grpc/otlp_logs_logs_2",
		"transform/otlp_grpc/otlp_logs_logs_3",
	}, conf.Get("service::pipelines::logs/logs_custom__pipe_otlp__in::processors"))
}

func buildLogsProcessorChainForHost(t *testing.T, configYAML string, info hostInfo, procIDs []string) (consumer.Logs, *consumertest.LogsSink) {
	t.Helper()
	ctx := context.Background()
	cfg, factories := getTestCollectorConfig(t, configYAML, info)

	sink := new(consumertest.LogsSink)
	var next consumer.Logs = sink
	for i := len(procIDs) - 1; i >= 0; i-- {
		var id component.ID
		require.NoError(t, id.UnmarshalText([]byte(procIDs[i])))
		factory := factories.Processors[id.Type()]
		require.NotNil(t, factory, "missing processor factory for %s", id)
		procCfg := cfg.Processors[id]
		require.NotNil(t, procCfg, "missing processor config for %s", id)

		set := processortest.NewNopSettings(id.Type())
		set.ID = id
		proc, err := factory.CreateLogs(ctx, set, procCfg, next)
		require.NoError(t, err)
		require.NoError(t, proc.Start(ctx, componenttest.NewNopHost()))
		t.Cleanup(func() {
			require.NoError(t, proc.Shutdown(ctx))
		})
		next = proc
	}
	return next, sink
}

func assertLogAttrStr(t *testing.T, attrs pcommon.Map, key, expected string) {
	t.Helper()
	val, ok := attrs.Get(key)
	require.True(t, ok, "expected attribute %q to exist", key)
	assert.Equal(t, expected, val.Str())
}

func TestLoggingExporterAndSetLogNameTransformation(t *testing.T) {
	info := hostInfo{OS: "linux", Platform: "debian", PlatformVersion: "12", Hostname: "my-gce-instance"}
	chain, sink := buildLogsProcessorChainForHost(t, "", info, []string{
		"transform/logging_syslog_0",
		"resource/otlp_grpc/otlp_logs_logs_1",
		"transform/otlp_grpc/otlp_logs_logs_2",
		"transform/otlp_grpc/otlp_logs_logs_3",
	})

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "checkout-service")
	rl.Resource().Attributes().PutStr("service.namespace", "shop")
	rl.Resource().Attributes().PutStr("service.instance.id", "instance-42")

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName("my.instrumentation.scope")
	sl.Scope().SetVersion("1.2.3")

	// Record 1: no existing gcp.log_name or compute.googleapis.com/resource_name.
	lr1 := sl.LogRecords().AppendEmpty()
	lr1.Body().SetStr("first log line")

	// Record 2: pre-existing gcp.log_name and compute.googleapis.com/resource_name should be preserved.
	lr2 := sl.LogRecords().AppendEmpty()
	lr2.Body().SetStr("second log line")
	lr2.Attributes().PutStr("gcp.log_name", "custom_override")
	lr2.Attributes().PutStr("compute.googleapis.com/resource_name", "custom-host")

	require.NoError(t, chain.ConsumeLogs(context.Background(), ld))

	allLogs := sink.AllLogs()
	require.Len(t, allLogs, 1)
	outRL := allLogs[0].ResourceLogs().At(0)

	assertLogAttrStr(t, outRL.Resource().Attributes(), "gcp.use_legacy_mapping", "true")

	outRecords := outRL.ScopeLogs().At(0).LogRecords()
	require.Equal(t, 2, outRecords.Len())

	rec1Attrs := outRecords.At(0).Attributes()
	assertLogAttrStr(t, rec1Attrs, "gcp.log_name", "syslog")
	assertLogAttrStr(t, rec1Attrs, "compute.googleapis.com/resource_name", "my-gce-instance")
	assertLogAttrStr(t, rec1Attrs, "instrumentation_source", "my.instrumentation.scope")
	assertLogAttrStr(t, rec1Attrs, "instrumentation_version", "1.2.3")
	assertLogAttrStr(t, rec1Attrs, "service.name", "checkout-service")
	assertLogAttrStr(t, rec1Attrs, "service.namespace", "shop")
	assertLogAttrStr(t, rec1Attrs, "service.instance.id", "instance-42")

	rec2Attrs := outRecords.At(1).Attributes()
	assertLogAttrStr(t, rec2Attrs, "gcp.log_name", "custom_override")
	assertLogAttrStr(t, rec2Attrs, "compute.googleapis.com/resource_name", "custom-host")
}

func TestSyslogTransformation(t *testing.T) {
	configYAML := `logging:
  receivers:
    syslog_tcp:
      type: syslog
      transport_protocol: tcp
      listen_host: 127.0.0.1
      listen_port: 5140
  service:
    pipelines:
      default_pipeline:
        receivers: [syslog_tcp]
`
	info := hostInfo{OS: "linux", Platform: "debian", PlatformVersion: "12", Hostname: "syslog-vm"}
	chain, sink := buildLogsProcessorChainForHost(t, configYAML, info, []string{
		"transform/logging_syslog__tcp_0",
		"transform/logging_syslog__tcp_1",
	})

	ld := plog.NewLogs()
	sl := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr("<34>1 2026-10-05T12:00:00Z mymachine su - ID47 - 'su root' failed")
	lr.Attributes().PutStr("appname", "su")
	lr.Attributes().PutStr("proc_id", "ID47")

	require.NoError(t, chain.ConsumeLogs(context.Background(), ld))

	allLogs := sink.AllLogs()
	require.Len(t, allLogs, 1)
	outRec := allLogs[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)

	require.Equal(t, pcommon.ValueTypeMap, outRec.Body().Type())
	msgVal, ok := outRec.Body().Map().Get("message")
	require.True(t, ok)
	assert.Equal(t, "<34>1 2026-10-05T12:00:00Z mymachine su - ID47 - 'su root' failed", msgVal.Str())

	_, hasAppname := outRec.Attributes().Get("appname")
	assert.False(t, hasAppname, "parsed syslog attributes should be cleared before setLogNameProcessor")
	assertLogAttrStr(t, outRec.Attributes(), "gcp.log_name", "syslog_tcp")
	assertLogAttrStr(t, outRec.Attributes(), "compute.googleapis.com/resource_name", "syslog-vm")
}

func TestFluentForwardTransformation(t *testing.T) {
	configYAML := `logging:
  receivers:
    fluent_logs:
      type: fluent_forward
  service:
    pipelines:
      default_pipeline:
        receivers: [fluent_logs]
`
	info := hostInfo{OS: "linux", Platform: "debian", PlatformVersion: "12", Hostname: "fluent-vm"}
	chain, sink := buildLogsProcessorChainForHost(t, configYAML, info, []string{
		"transform/logging_fluent__logs_0",
		"transform/logging_fluent__logs_1",
		"transform/logging_fluent__logs_2",
	})

	ld := plog.NewLogs()
	sl := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()
	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr("some message")
	lr.Attributes().PutStr("fluent.tag", "forwarder_tag")
	lr.Attributes().PutStr("field1", "value1")

	require.NoError(t, chain.ConsumeLogs(context.Background(), ld))

	allLogs := sink.AllLogs()
	require.Len(t, allLogs, 1)
	outRec := allLogs[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)

	require.Equal(t, pcommon.ValueTypeMap, outRec.Body().Type())
	bodyMap := outRec.Body().Map()
	assertLogAttrStr(t, bodyMap, "message", "some message")
	assertLogAttrStr(t, bodyMap, "field1", "value1")
	_, hasFluentTag := bodyMap.Get("fluent.tag")
	assert.False(t, hasFluentTag, "fluent.tag should be deleted from body after appending to gcp.log_name")

	assertLogAttrStr(t, outRec.Attributes(), "gcp.log_name", "fluent_logs.forwarder_tag")
	assertLogAttrStr(t, outRec.Attributes(), "compute.googleapis.com/resource_name", "fluent-vm")
}

func TestJournaldTransformation(t *testing.T) {
	configYAML := `logging:
  receivers:
    systemd_logs:
      type: systemd_journald
  service:
    pipelines:
      default_pipeline:
        receivers: [systemd_logs]
`
	info := hostInfo{OS: "linux", Platform: "debian", PlatformVersion: "12", Hostname: "journald-vm"}
	chain, sink := buildLogsProcessorChainForHost(t, configYAML, info, []string{
		"transform/logging_systemd__logs_0",
		"transform/logging_systemd__logs_1",
	})

	ld := plog.NewLogs()
	sl := ld.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()

	lr1 := sl.LogRecords().AppendEmpty()
	body1 := lr1.Body().SetEmptyMap()
	body1.PutStr("MESSAGE", "unit failed")
	body1.PutStr("PRIORITY", "3")
	body1.PutStr("CODE_FILE", "src/core/unit.c")
	body1.PutStr("CODE_FUNC", "unit_start")
	body1.PutStr("CODE_LINE", "42")

	lr2 := sl.LogRecords().AppendEmpty()
	body2 := lr2.Body().SetEmptyMap()
	body2.PutStr("MESSAGE", "unmapped priority")
	body2.PutStr("PRIORITY", "99")
	lr2.SetSeverityText("ORIGINAL")
	lr2.SetSeverityNumber(plog.SeverityNumberInfo)

	require.NoError(t, chain.ConsumeLogs(context.Background(), ld))

	allLogs := sink.AllLogs()
	require.Len(t, allLogs, 1)
	outRecords := allLogs[0].ResourceLogs().At(0).ScopeLogs().At(0).LogRecords()
	require.Equal(t, 2, outRecords.Len())

	out1 := outRecords.At(0)
	assert.Equal(t, "ERROR", out1.SeverityText())
	assert.Equal(t, plog.SeverityNumberUnspecified, out1.SeverityNumber())
	assertLogAttrStr(t, out1.Attributes(), "gcp.log_name", "systemd_logs")
	assertLogAttrStr(t, out1.Attributes(), "compute.googleapis.com/resource_name", "journald-vm")

	srcLocVal, ok := out1.Attributes().Get("gcp.source_location")
	require.True(t, ok, "expected gcp.source_location attribute map")
	require.Equal(t, pcommon.ValueTypeMap, srcLocVal.Type())
	assertLogAttrStr(t, srcLocVal.Map(), "file", "src/core/unit.c")
	assertLogAttrStr(t, srcLocVal.Map(), "func", "unit_start")
	lineVal, ok := srcLocVal.Map().Get("line")
	require.True(t, ok)
	assert.Equal(t, int64(42), lineVal.Int())

	out2 := outRecords.At(1)
	assert.Equal(t, "ORIGINAL", out2.SeverityText())
	assert.Equal(t, plog.SeverityNumberInfo, out2.SeverityNumber())
}
