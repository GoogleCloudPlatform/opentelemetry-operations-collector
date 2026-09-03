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

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
)

func TestSelfMetricsPolicy_Evaluate(t *testing.T) {
	policy := &SelfMetricsPolicy{
		Name: "custom_self_obs",
		Port: 9999,
	}

	assert.Equal(t, "custom_self_obs", policy.PolicyName())
	assert.Equal(t, PolicyType, policy.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassSource, policy.PolicyClass())

	collectorID := "custom-collector-id"
	fleetID := "1234"
	ctx := policy.ContextSetup(t.Context(), collectorID, fleetID)

	conf, err := policy.Evaluate(ctx)
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
			"value": collectorID,
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
	policy := &SelfMetricsPolicy{
		Name: "default_self_observability",
		Port: 8888,
	}

	ctx := policy.ContextSetup(t.Context(), "76728b45-8fcf-4cc8-a09e-c67283bfa79c", "1234")
	conf, err := policy.Evaluate(ctx)
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

func TestSelfMetricsPolicy_Evaluate_MissingContext(t *testing.T) {
	policy := &SelfMetricsPolicy{
		Name: "test_missing_context",
		Port: 8888,
	}

	// Missing collector ID
	_, err := policy.Evaluate(t.Context())
	require.ErrorIs(t, err, ErrNoCollectorID)

	// Missing fleet ID
	ctx := policy.ContextSetup(t.Context(), "collector-123", "")
	_, err = policy.Evaluate(ctx)
	require.ErrorIs(t, err, ErrNoFleetID)
}

func TestSelfMetricsPolicy_Pipelines(t *testing.T) {
	policy := &SelfMetricsPolicy{
		Name: "test_policy",
		Port: 8888,
	}

	preProcLog := component.MustNewIDWithName("queue_batch", "batch_logs")
	preProcMetric := component.MustNewIDWithName("queue_batch", "batch_metrics")
	exporter := component.MustNewIDWithName("otlp_grpc", "gcp")
	extension := component.MustNewIDWithName("googleclientauth", "auth")

	// LogsPipelines
	logsConf, err := policy.LogsPipelines([]component.ID{preProcLog}, []component.ID{exporter}, []component.ID{extension})
	require.NoError(t, err)
	require.NotNil(t, logsConf)
	assert.Equal(t, []any{"otlp/test_policy"}, logsConf.Get("service::pipelines::logs/test_policy::receivers"))
	assert.Equal(t, []any{"queue_batch/batch_logs"}, logsConf.Get("service::pipelines::logs/test_policy::processors"))
	assert.Equal(t, []any{"otlp_grpc/gcp"}, logsConf.Get("service::pipelines::logs/test_policy::exporters"))

	// MetricsPipelines
	metricsConf, err := policy.MetricsPipelines([]component.ID{preProcMetric}, []component.ID{exporter}, []component.ID{extension})
	require.NoError(t, err)
	require.NotNil(t, metricsConf)
	assert.Equal(t, []any{"otlp/test_policy"}, metricsConf.Get("service::pipelines::metrics/test_policy::receivers"))
	assert.Equal(t, []any{"queue_batch/batch_metrics"}, metricsConf.Get("service::pipelines::metrics/test_policy::processors"))
	assert.Equal(t, []any{"otlp_grpc/gcp"}, metricsConf.Get("service::pipelines::metrics/test_policy::exporters"))

	// TracesPipelines (should return nil, nil)
	tracesConf, err := policy.TracesPipelines(nil, []component.ID{exporter}, []component.ID{extension})
	require.NoError(t, err)
	assert.Nil(t, tracesConf)
}
