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
	"context"
	"fmt"
	"testing"
	"time"

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
				"interval": 60000,
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

	// Verify resourcedetection processor
	assert.Equal(t, []any{"gcp"}, conf.Get("processors::resourcedetection/custom_self_obs::detectors"))
	assert.Equal(t, false, conf.Get("processors::resourcedetection/custom_self_obs::override"))
	assert.Equal(t, 10*time.Second, conf.Get("processors::resourcedetection/custom_self_obs::timeout"))

	// Verify transform processor
	assert.Equal(t, "ignore", conf.Get("processors::transform/custom_self_obs::error_mode"))
	assert.NotNil(t, conf.Get("processors::transform/custom_self_obs::log_statements"))
	assert.Equal(t, []any{
		map[string]any{
			"context": "resource",
			"statements": []any{
				`set(attributes["gcp.project_id"], attributes["cloud.account.id"]) where attributes["gcp.project_id"] == nil and attributes["cloud.account.id"] != nil`,
			},
		},
		map[string]any{
			"context": "datapoint",
			"statements": []any{
				`set(attributes["gcp.fleet_id"], resource.attributes["gcp.fleet_id"]) where resource.attributes["gcp.fleet_id"] != nil`,
				`set(attributes["service.instance.id"], resource.attributes["service.instance.id"]) where resource.attributes["service.instance.id"] != nil`,
				`set(attributes["service.version"], resource.attributes["service.version"]) where resource.attributes["service.version"] != nil`,
			},
		},
	}, conf.Get("processors::transform/custom_self_obs::metric_statements"))
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
			"interval": 60000,
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

	preProcLog := component.MustNewIDWithName("queuebatch", "batch_logs")
	preProcMetric := component.MustNewIDWithName("queuebatch", "batch_metrics")
	exporter := component.MustNewIDWithName("otlp_grpc", "gcp")
	extension := component.MustNewIDWithName("googleclientauth", "auth")

	// LogsPipelines
	logsConf, err := policy.LogsPipelines([]component.ID{preProcLog}, []component.ID{exporter}, []component.ID{extension})
	require.NoError(t, err)
	require.NotNil(t, logsConf)
	assert.Equal(t, []any{"otlp/test_policy"}, logsConf.Get("service::pipelines::logs/test_policy::receivers"))
	assert.Equal(t, []any{"resourcedetection/test_policy", "transform/test_policy", "queuebatch/batch_logs"}, logsConf.Get("service::pipelines::logs/test_policy::processors"))
	assert.Equal(t, []any{"otlp_grpc/gcp"}, logsConf.Get("service::pipelines::logs/test_policy::exporters"))

	// MetricsPipelines
	metricsConf, err := policy.MetricsPipelines([]component.ID{preProcMetric}, []component.ID{exporter}, []component.ID{extension})
	require.NoError(t, err)
	require.NotNil(t, metricsConf)
	assert.Equal(t, []any{"otlp/test_policy"}, metricsConf.Get("service::pipelines::metrics/test_policy::receivers"))
	assert.Equal(t, []any{"resourcedetection/test_policy", "transform/test_policy", "queuebatch/batch_metrics"}, metricsConf.Get("service::pipelines::metrics/test_policy::processors"))
	assert.Equal(t, []any{"otlp_grpc/gcp"}, metricsConf.Get("service::pipelines::metrics/test_policy::exporters"))

	// TracesPipelines (should return nil, nil)
	tracesConf, err := policy.TracesPipelines(nil, []component.ID{exporter}, []component.ID{extension})
	require.NoError(t, err)
	assert.Nil(t, tracesConf)
}

func TestSelfMetricsPolicy_Evaluate_WithProjectID(t *testing.T) {
	policy := &SelfMetricsPolicy{
		Name: "test_project_self_obs",
		Port: 9999,
	}

	collectorID := "custom-collector-id"
	fleetID := "demo-fleet"
	ctx := policy.ContextSetup(t.Context(), collectorID, fleetID)
	ctx = context.WithValue(ctx, contextKeyProjectID, "my-gcp-project")

	conf, err := policy.Evaluate(ctx)
	require.NoError(t, err)
	require.NotNil(t, conf)

	// Verify service telemetry resource attributes include gcp.project_id
	assert.Equal(t, []any{
		map[string]any{
			"name":  "service.instance.id",
			"value": collectorID,
		},
		map[string]any{
			"name":  "gcp.fleet_id",
			"value": "demo-fleet",
		},
		map[string]any{
			"name":  "gcp.project_id",
			"value": "my-gcp-project",
		},
	}, conf.Get("service::telemetry::resource::attributes"))
}

// TestSelfMetricsPolicy_Validate covers the path a control plane can reach.
// This driver is registered, so LoadPolicy calls Validate on any self_metrics
// policy that arrives over xDS; while Validate panicked, a single malformed
// remote policy was enough to take down the collector process.
func TestSelfMetricsPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		policy  *SelfMetricsPolicy
		wantErr string
	}{
		{
			name:   "valid",
			policy: &SelfMetricsPolicy{Name: "self_metrics", Port: 18888},
		},
		{
			name:    "missing name",
			policy:  &SelfMetricsPolicy{Port: 18888},
			wantErr: "policy must be named",
		},
		{
			// The zero value, which is what an omitted port unmarshals to.
			name:    "zero port",
			policy:  &SelfMetricsPolicy{Name: "self_metrics"},
			wantErr: "port must be between 1 and 65535",
		},
		{
			name:    "port out of range",
			policy:  &SelfMetricsPolicy{Name: "self_metrics", Port: 70000},
			wantErr: "port must be between 1 and 65535",
		},
		{
			name:    "negative port",
			policy:  &SelfMetricsPolicy{Name: "self_metrics", Port: -1},
			wantErr: "port must be between 1 and 65535",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Fails the test rather than crashing the run if Validate ever
			// goes back to panicking.
			require.NotPanics(t, func() {
				err := tc.policy.Validate()
				if tc.wantErr == "" {
					assert.NoError(t, err)
					return
				}
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			})
		})
	}
}

// TestSelfMetricsPolicy_Validate_LoadableFromRegistry exercises the whole
// remote path: the registered driver unmarshals a raw config and validates it,
// which is what a policy delivered by a control plane goes through.
func TestSelfMetricsPolicy_Validate_LoadableFromRegistry(t *testing.T) {
	loaded, err := googlepolicy.LoadPolicy(PolicyType, map[string]any{
		"type": PolicyType,
		"name": "remote_self_metrics",
		"port": 19999,
	})
	require.NoError(t, err)
	assert.Equal(t, "remote_self_metrics", loaded.PolicyName())

	_, err = googlepolicy.LoadPolicy(PolicyType, map[string]any{
		"type": PolicyType,
		"name": "no_port",
	})
	require.ErrorIs(t, err, googlepolicy.ErrPolicyFailedValidation)
}

// resourceStatements returns the resource-context statements of the policy's
// transform processor for the given signal key ("log_statements" or
// "metric_statements").
func resourceStatements(t *testing.T, conf interface{ Get(string) any }, policyName, key string) []any {
	t.Helper()
	groups, ok := conf.Get("processors::transform/" + policyName + "::" + key).([]any)
	require.True(t, ok, key)
	for _, g := range groups {
		m, ok := g.(map[string]any)
		require.True(t, ok)
		if m["context"] == "resource" {
			stmts, ok := m["statements"].([]any)
			require.True(t, ok)
			return stmts
		}
	}
	t.Fatalf("no resource statements in %s", key)
	return nil
}

func TestSelfMetricsPolicy_Evaluate_RegionFallback(t *testing.T) {
	policy := &SelfMetricsPolicy{Name: "self", Port: 9999}
	fromAccount := `set(attributes["gcp.project_id"], attributes["cloud.account.id"]) where attributes["gcp.project_id"] == nil and attributes["cloud.account.id"] != nil`

	// With a resolved region, self-metrics and self-logs get the cloud.region
	// fallback after resource detection.
	ctx := context.WithValue(policy.ContextSetup(t.Context(), "collector-id", "1234"), contextKeyRegion, "us-east1")
	conf, err := policy.Evaluate(ctx)
	require.NoError(t, err)
	want := []any{fromAccount, googlepolicy.CloudRegionFallbackStatement("us-east1")}
	assert.Equal(t, want, resourceStatements(t, conf, "self", "metric_statements"))
	assert.Equal(t, want, resourceStatements(t, conf, "self", "log_statements"))

	// The fallback is applied in the transform, never as a telemetry resource
	// attribute, so the gcp detector can still set the host's own region.
	for _, a := range conf.Get("service::telemetry::resource::attributes").([]any) {
		assert.NotEqual(t, "cloud.region", a.(map[string]any)["name"])
	}

	// Without a resolved region, nothing changes.
	conf, err = policy.Evaluate(policy.ContextSetup(t.Context(), "collector-id", "1234"))
	require.NoError(t, err)
	assert.Equal(t, []any{fromAccount}, resourceStatements(t, conf, "self", "metric_statements"))
	assert.Equal(t, []any{fromAccount}, resourceStatements(t, conf, "self", "log_statements"))
}

// withInProcessReader overrides the telemetry.inProcessReader gate lookup for
// the duration of the test.
func withInProcessReader(t *testing.T, enabled bool) {
	t.Helper()
	orig := inProcessReaderEnabled
	inProcessReaderEnabled = func() bool { return enabled }
	t.Cleanup(func() { inProcessReaderEnabled = orig })
}

func TestSelfMetricsPolicy_InProcessReader(t *testing.T) {
	withInProcessReader(t, true)

	policy := &SelfMetricsPolicy{Name: "self", Port: 9999}
	ctx := policy.ContextSetup(t.Context(), "collector-id", "1234")

	conf, err := policy.Evaluate(ctx)
	require.NoError(t, err)

	// No metric reader and no log processor: nothing is exported over OTLP to
	// a local port. Both keys must be present as explicit empty lists so they
	// replace the telemetry factory's defaults (e.g. the :8888 reader).
	require.True(t, conf.IsSet("service::telemetry::metrics::readers"))
	assert.Equal(t, []any{}, conf.Get("service::telemetry::metrics::readers"))
	require.True(t, conf.IsSet("service::telemetry::logs::processors"))
	assert.Equal(t, []any{}, conf.Get("service::telemetry::logs::processors"))

	// No OTLP receiver, so no listening port at all.
	receivers, ok := conf.Get("receivers").(map[string]any)
	require.True(t, ok)
	assert.Len(t, receivers, 2)
	assert.NotContains(t, receivers, "otlp/self")

	// Internal metrics and logs are collected in-process, by separate
	// receiver instances with their own collection intervals.
	assert.Equal(t, 60*time.Second, conf.Get("receivers::selftelemetry/self_metrics::collection_interval"))
	assert.Equal(t, 30*time.Second, conf.Get("receivers::selftelemetry/self_metrics::timeout"))
	assert.Equal(t, selfLogsCollectionInterval, conf.Get("receivers::selftelemetry/self_logs::collection_interval"))
	assert.Equal(t, 30*time.Second, conf.Get("receivers::selftelemetry/self_logs::timeout"))

	exporter := component.MustNewIDWithName("otlp_grpc", "gcp")
	metricsConf, err := policy.MetricsPipelines(nil, []component.ID{exporter}, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{"selftelemetry/self_metrics"}, metricsConf.Get("service::pipelines::metrics/self::receivers"))
	assert.Equal(t, []any{"resourcedetection/self", "transform/self"}, metricsConf.Get("service::pipelines::metrics/self::processors"))

	logsConf, err := policy.LogsPipelines(nil, []component.ID{exporter}, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{"selftelemetry/self_logs"}, logsConf.Get("service::pipelines::logs/self::receivers"))
	assert.Equal(t, []any{"resourcedetection/self", "transform/self"}, logsConf.Get("service::pipelines::logs/self::processors"))
}

func TestSelfMetricsPolicy_InProcessReaderDisabled(t *testing.T) {
	withInProcessReader(t, false)

	policy := &SelfMetricsPolicy{Name: "self", Port: 9999}
	ctx := policy.ContextSetup(t.Context(), "collector-id", "1234")

	conf, err := policy.Evaluate(ctx)
	require.NoError(t, err)

	assert.False(t, conf.IsSet("receivers::selftelemetry/self_metrics"))
	assert.False(t, conf.IsSet("receivers::selftelemetry/self_logs"))
	assert.Equal(t, "localhost:9999", conf.Get("receivers::otlp/self::protocols::grpc::endpoint"))
	readers, ok := conf.Get("service::telemetry::metrics::readers").([]any)
	require.True(t, ok)
	require.Len(t, readers, 1)
	processors, ok := conf.Get("service::telemetry::logs::processors").([]any)
	require.True(t, ok)
	require.Len(t, processors, 1)

	metricsConf, err := policy.MetricsPipelines(nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{"otlp/self"}, metricsConf.Get("service::pipelines::metrics/self::receivers"))
	logsConf, err := policy.LogsPipelines(nil, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{"otlp/self"}, logsConf.Get("service::pipelines::logs/self::receivers"))
}

func TestInProcessReaderEnabled_UnregisteredGate(t *testing.T) {
	// The inprocesstelemetry factory is not linked into this test binary, so
	// the gate is not registered and the loopback path must be used.
	assert.False(t, inProcessReaderEnabled())
}
