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
	"go.opentelemetry.io/collector/confmap"
	"go.uber.org/zap"
)

func TestTracesValidate(t *testing.T) {
	testCases := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name: "traces_pipeline_id_with_lib_prefix",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "otlp"},
					},
				},
				Traces: &Traces{
					Service: &TracesService{
						Pipelines: map[string]*Pipeline{
							"lib:traces": {ReceiverIDs: []string{"otlp"}},
						},
					},
				},
			},
			wantErr: `traces pipeline ID "lib:traces" cannot start with "lib:"`,
		},
		{
			name: "traces_pipeline_undefined_receiver_without_combined",
			config: Config{
				Traces: &Traces{
					Service: &TracesService{
						Pipelines: map[string]*Pipeline{
							"traces_pipe": {ReceiverIDs: []string{"otlp"}},
						},
					},
				},
			},
			wantErr: `traces receiver "otlp" from pipeline "traces_pipe" is not defined`,
		},
		{
			name: "traces_pipeline_undefined_receiver",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "otlp"},
					},
				},
				Traces: &Traces{
					Service: &TracesService{
						Pipelines: map[string]*Pipeline{
							"traces_pipe": {ReceiverIDs: []string{"other"}},
						},
					},
				},
			},
			wantErr: `traces receiver "other" from pipeline "traces_pipe" is not defined`,
		},
		{
			name: "traces_pipeline_does_not_support_processors",
			config: Config{
				Combined: &Combined{
					Receivers: map[string]CombinedReceiver{
						"otlp": {Type: "otlp"},
					},
				},
				Traces: &Traces{
					Service: &TracesService{
						Pipelines: map[string]*Pipeline{
							"otlp": {
								ReceiverIDs:  []string{"otlp"},
								ProcessorIDs: []string{"proc"},
							},
						},
					},
				},
			},
			wantErr: `traces pipeline "otlp" uses processors but traces pipelines do not support processors`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.config.generateOtelConfig(context.Background(), t.TempDir(), hostInfo{OS: "linux"})
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestRetrieveTracesPipelines(t *testing.T) {
	configYAML := `combined:
  receivers:
    otlp_recv:
      type: otlp
      grpc_endpoint: 127.0.0.1:4317
traces:
  service:
    pipelines:
      trace_pipe:
        receivers: [otlp_recv]
      empty_pipe:
`
	configFile, _ := writeTestConfig(t, configYAML)
	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	retrieved, err := p.Retrieve(context.Background(), "opsagentconf:"+configFile, nil)
	require.NoError(t, err)

	conf, err := retrieved.AsConf()
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:4317", conf.Get("receivers::otlp/otlp__recv::protocols::grpc::endpoint"))
	assert.True(t, conf.IsSet("exporters::otlp_grpc/otlp_traces"))
	assert.Equal(t, false, conf.Get("processors::resourcedetection/_global_1::override"))
	assert.Equal(t, []string{
		"resourcedetection/_global_1",
		"batch/otlp_grpc/otlp_traces_traces_1",
	}, conf.Get("service::pipelines::traces/traces_trace__pipe_otlp__recv::processors"))
	assert.Equal(t, []string{"otlp_grpc/otlp_traces"}, conf.Get("service::pipelines::traces/traces_trace__pipe_otlp__recv::exporters"))
}
