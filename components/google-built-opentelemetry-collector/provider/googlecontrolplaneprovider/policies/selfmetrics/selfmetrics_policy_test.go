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

package selfmetrics

import (
	"fmt"
	"testing"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider/internal/collectorid"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelfMetricsPolicy_Evaluate(t *testing.T) {
	require.NoError(t, collectorid.GenerateCollectorID())

	policy := &SelfMetricsPolicy{
		Name: "custom_self_obs",
		Port: 9999,
	}

	assert.Equal(t, "custom_self_obs", policy.PolicyName())
	assert.Equal(t, PolicyType, policy.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassSource, policy.PolicyClass())

	ret, err := policy.Evaluate(t.Context())
	require.NoError(t, err)
	require.NotNil(t, ret)

	conf, err := ret.AsConf()
	require.NoError(t, err)
	require.NotNil(t, conf)

	// Verify receiver configuration
	expectedReceiverKey := "receivers::otlp/custom_self_obs::protocols::grpc::endpoint"
	assert.Equal(t, "localhost:9999", conf.Get(expectedReceiverKey))
	assert.Nil(t, conf.Get("receivers::otlp/custom_self_obs::protocols::http"))

	// Verify service telemetry resource attributes
	assert.Equal(t, []any{
		map[string]any{
			"name":  "service.instance.id",
			"value": collectorid.CollectorID,
		},
		map[string]any{
			"name":  "gcp.fleet_id",
			"value": "1234",
		},
	}, conf.Get("service::telemetry::resource::attributes"))

	// Verify service telemetry logs
	assert.Equal(t, "info", conf.Get("service::telemetry::logs::level"))
	assert.Equal(t, []any{
		map[string]any{
			"batch": map[string]any{
				"exporter": map[string]any{
					"otlp": map[string]any{
						"endpoint": fmt.Sprintf("http://localhost:%d", 9999),
						"insecure": true,
						"protocol": "grpc",
					},
				},
			},
		},
	}, conf.Get("service::telemetry::logs::processors"))

	// Verify service telemetry metrics
	assert.Equal(t, "Normal", conf.Get("service::telemetry::metrics::level"))
	assert.Equal(t, []any{
		map[string]any{
			"periodic": map[string]any{
				"interval": 5000,
				"timeout":  30000,
				"exporter": map[string]any{
					"otlp": map[string]any{
						"endpoint": fmt.Sprintf("http://localhost:%d", 9999),
						"insecure": true,
						"protocol": "grpc",
					},
				},
			},
		},
	}, conf.Get("service::telemetry::metrics::readers"))
}

func TestSelfMetricsPolicy_Evaluate_ExampleYamlMatch(t *testing.T) {
	collectorid.CollectorID = "76728b45-8fcf-4cc8-a09e-c67283bfa79c"
	defer func() {
		collectorid.CollectorID = ""
	}()

	policy := &SelfMetricsPolicy{
		Name: "default_self_observability",
		Port: 8888,
	}

	ret, err := policy.Evaluate(t.Context())
	require.NoError(t, err)

	conf, err := ret.AsConf()
	require.NoError(t, err)

	assert.Equal(t, "localhost:8888", conf.Get("receivers::otlp/default_self_observability::protocols::grpc::endpoint"))

	attrs := conf.Get("service::telemetry::resource::attributes").([]any)
	require.Len(t, attrs, 2)
	assert.Equal(t, map[string]any{"name": "service.instance.id", "value": "76728b45-8fcf-4cc8-a09e-c67283bfa79c"}, attrs[0])
	assert.Equal(t, map[string]any{"name": "gcp.fleet_id", "value": "1234"}, attrs[1])

	assert.Equal(t, "info", conf.Get("service::telemetry::logs::level"))

	procs := conf.Get("service::telemetry::logs::processors").([]any)
	require.Len(t, procs, 1)
	assert.Equal(t, map[string]any{
		"batch": map[string]any{
			"exporter": map[string]any{
				"otlp": map[string]any{
					"endpoint": "http://localhost:8888",
					"insecure": true,
					"protocol": "grpc",
				},
			},
		},
	}, procs[0])

	assert.Equal(t, "Normal", conf.Get("service::telemetry::metrics::level"))

	readers := conf.Get("service::telemetry::metrics::readers").([]any)
	require.Len(t, readers, 1)
	assert.Equal(t, map[string]any{
		"periodic": map[string]any{
			"interval": 5000,
			"timeout":  30000,
			"exporter": map[string]any{
				"otlp": map[string]any{
					"endpoint": "http://localhost:8888",
					"insecure": true,
					"protocol": "grpc",
				},
			},
		},
	}, readers[0])
}
