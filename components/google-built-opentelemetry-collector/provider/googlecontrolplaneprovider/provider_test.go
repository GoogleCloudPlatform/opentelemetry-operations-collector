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

package googlecontrolplaneprovider

import (
	"context"
	"errors"
	"testing"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider/policies/selfmetrics"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/event"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/confmaptest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func createProvider() confmap.Provider {
	return NewFactory().Create(confmaptest.NewNopProviderSettings())
}

func createProviderWithLogger(logger *zap.Logger) confmap.Provider {
	settings := confmaptest.NewNopProviderSettings()
	settings.Logger = logger
	return NewFactory().Create(settings)
}

func TestValidateProviderScheme(t *testing.T) {
	assert.NoError(t, confmaptest.ValidateProviderScheme(createProvider()))
}

func TestScheme(t *testing.T) {
	p := createProvider()
	assert.Equal(t, "googlecontrolplane", p.Scheme())
}

func TestUnsupportedScheme(t *testing.T) {
	p := createProvider()
	_, err := p.Retrieve(context.Background(), "file:/path/to/conf", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrURINotSupported)
	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestEmptyURI(t *testing.T) {
	p := createProvider()
	_, err := p.Retrieve(context.Background(), "", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrURINotSupported)
	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestEmptyTarget(t *testing.T) {
	p := createProvider()
	_, err := p.Retrieve(context.Background(), "googlecontrolplane:", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyURI)
	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()
	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.NoError(t, err)
	require.NotNil(t, ret)

	conf, err := ret.AsConf()
	require.NoError(t, err)
	assert.NotNil(t, conf)

	// Verify both destination policy and selfmetrics policy configurations are merged
	assert.True(t, conf.IsSet("extensions::googleclientauth/default_gcp_destination"))
	assert.True(t, conf.IsSet("exporters::otlp_grpc/default_gcp_destination"))
	assert.True(t, conf.IsSet("receivers::otlp/default_self_metrics"))
	assert.True(t, conf.IsSet("service::telemetry::resource::attributes"))
	assert.True(t, conf.IsSet("service::pipelines::logs/default_self_metrics"))
	assert.True(t, conf.IsSet("service::pipelines::metrics/default_self_metrics"))

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestShutdown(t *testing.T) {
	p := createProvider()
	assert.NoError(t, p.Shutdown(context.Background()))
}

type mockDestinationPolicy struct {
	name string
}

func (m *mockDestinationPolicy) PolicyName() string { return m.name }
func (m *mockDestinationPolicy) PolicyType() string { return "mock_destination" }
func (m *mockDestinationPolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassDestination
}
func (m *mockDestinationPolicy) Validate() error { return nil }
func (m *mockDestinationPolicy) Evaluate(context.Context) (*confmap.Conf, error) {
	return confmap.NewFromStringMap(map[string]any{
		"exporters": map[string]any{
			"otlp/" + m.name: map[string]any{
				"endpoint": "example.com:4317",
			},
		},
	}), nil
}
func (m *mockDestinationPolicy) ExporterIDs() []component.ID {
	otlpType, _ := component.NewType("otlp")
	return []component.ID{component.NewIDWithName(otlpType, m.name)}
}
func (m *mockDestinationPolicy) PreProcessMetricIDs() []component.ID { return nil }
func (m *mockDestinationPolicy) PreProcessLogIDs() []component.ID    { return nil }
func (m *mockDestinationPolicy) PreProcessTraceIDs() []component.ID  { return nil }
func (m *mockDestinationPolicy) ExtensionIDs() []component.ID        { return nil }

var _ googlepolicy.DestinationPolicy = (*mockDestinationPolicy)(nil)

type mockSourcePolicy struct {
	name    string
	evalErr error
}

func (m *mockSourcePolicy) PolicyName() string { return m.name }
func (m *mockSourcePolicy) PolicyType() string { return "mock_source" }
func (m *mockSourcePolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}
func (m *mockSourcePolicy) Validate() error { return nil }
func (m *mockSourcePolicy) Evaluate(context.Context) (*confmap.Conf, error) {
	if m.evalErr != nil {
		return nil, m.evalErr
	}
	return confmap.NewFromStringMap(map[string]any{
		"receivers": map[string]any{
			"otlp/" + m.name: map[string]any{
				"protocols": map[string]any{
					"grpc": map[string]any{"endpoint": "localhost:4317"},
				},
			},
		},
	}), nil
}
func (m *mockSourcePolicy) LogsPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Conf, error) {
	procStrs := make([]any, 0, len(preExportProcessors))
	for _, id := range preExportProcessors {
		procStrs = append(procStrs, id.String())
	}
	return confmap.NewFromStringMap(map[string]any{
		"service": map[string]any{
			"pipelines": map[string]any{
				"logs/" + m.name: map[string]any{
					"receivers":  []any{"otlp/" + m.name},
					"processors": procStrs,
					"exporters":  []any{"otlp_grpc/default_gcp_destination"},
				},
			},
		},
	}), nil
}
func (m *mockSourcePolicy) MetricsPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Conf, error) {
	return nil, nil
}
func (m *mockSourcePolicy) TracesPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Conf, error) {
	return nil, nil
}

var _ googlepolicy.SourcePolicy = (*mockSourcePolicy)(nil)

func TestRetrieve_NoFleetID(t *testing.T) {
	t.Setenv("FLEET_ID", "")
	p := createProvider()
	_, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, selfmetrics.ErrNoFleetID)
	assert.NoError(t, p.Shutdown(context.Background()))
}

// TestRetrieve_XDSServerUnreachable asserts that a control plane that is down
// does not stop the collector from starting: Start's initial-sync wait expires,
// Retrieve still succeeds on the built-in policies, and the xDS connection keeps
// retrying in the background.
//
// This is the degraded path that matters most in production -- the control plane
// being unavailable must never be the reason a collector fails to come up.
func TestRetrieve_XDSServerUnreachable(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	// Port 1 on loopback: nothing is listening, so the stream can never be
	// established and the manager is left retrying with backoff.
	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:xds://127.0.0.1:1?insecure=true", nil)
	require.NoError(t, err, "an unreachable xDS control plane must not block collector startup")
	require.NotNil(t, ret)

	conf, err := ret.AsConf()
	require.NoError(t, err)

	// The built-in destination and self metrics policies are still applied...
	assert.True(t, conf.IsSet("exporters::otlp_grpc/default_gcp_destination"))
	assert.True(t, conf.IsSet("receivers::otlp/default_self_metrics"))

	// ...so the collector always has at least one pipeline to stand up with,
	// even though no policies were ever received from the control plane.
	assert.True(t, conf.IsSet("service::pipelines::metrics/default_self_metrics"))
	assert.True(t, conf.IsSet("service::pipelines::logs/default_self_metrics"))

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_MultipleDestinationPolicies(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	p := createProviderWithLogger(logger)

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-mult-dest",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"dest1": {PolicyObj: &mockDestinationPolicy{name: "dest1"}},
			"dest2": {PolicyObj: &mockDestinationPolicy{name: "dest2"}},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.NoError(t, err)
	require.NotNil(t, ret)

	// Invalid policy set should be rolled back to nil (built-in fallback)
	assert.Nil(t, googlepolicy.ActivePolicySet())

	conf, err := ret.AsConf()
	require.NoError(t, err)
	assert.True(t, conf.IsSet("exporters::otlp_grpc/default_gcp_destination"))
	assert.True(t, conf.IsSet("receivers::otlp/default_self_metrics"))

	entries := recorded.All()
	require.Len(t, entries, 1)
	entry := entries[0]
	assert.Equal(t, zapcore.ErrorLevel, entry.Level)
	assert.Contains(t, entry.Message, "more than one destination policy found")
	assert.Equal(t, map[string]any{
		"event.name":             event.PolicySetInvalidEventName,
		"policy.set.revision.id": "rev-mult-dest",
		"context":                "context.Background.WithValue(FLEET_ID, 1234)",
	}, entry.ContextMap())

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_ActivePolicySetWithCustomDestination(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-custom-dest",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"custom_dest": {PolicyObj: &mockDestinationPolicy{name: "custom_dest"}},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.NoError(t, err)
	require.NotNil(t, ret)

	conf, err := ret.AsConf()
	require.NoError(t, err)
	assert.True(t, conf.IsSet("exporters::otlp/custom_dest"))
	assert.False(t, conf.IsSet("exporters::otlp_grpc/default_gcp_destination"))
	// Built-in self metrics should still be added
	assert.True(t, conf.IsSet("receivers::otlp/default_self_metrics"))

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_ActivePolicySetWithCustomSelfMetrics(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	customSelf := &selfmetrics.SelfMetricsPolicy{
		Name: "custom_self_metrics",
		Port: 9999,
	}
	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-custom-self",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"custom_self_metrics": {PolicyObj: customSelf},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.NoError(t, err)
	require.NotNil(t, ret)

	conf, err := ret.AsConf()
	require.NoError(t, err)
	assert.True(t, conf.IsSet("receivers::otlp/custom_self_metrics"))
	assert.False(t, conf.IsSet("receivers::otlp/default_self_metrics"))
	assert.NotContains(t, conf.Get("service::pipelines::logs/custom_self_metrics::processors"), "googlepolicy")
	assert.NotContains(t, conf.Get("service::pipelines::metrics/custom_self_metrics::processors"), "googlepolicy")

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_ActivePolicySetWithOtherSource(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-other-source",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"other_source": {PolicyObj: &mockSourcePolicy{name: "other_source"}},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.NoError(t, err)
	require.NotNil(t, ret)

	conf, err := ret.AsConf()
	require.NoError(t, err)
	assert.True(t, conf.IsSet("receivers::otlp/other_source"))
	// Customer source pipelines MUST include googlepolicy processor
	assert.Contains(t, conf.Get("service::pipelines::logs/other_source::processors"), "googlepolicy")
	// Built-in self metrics should also be added, but MUST bypass googlepolicy processor
	assert.True(t, conf.IsSet("receivers::otlp/default_self_metrics"))
	assert.NotContains(t, conf.Get("service::pipelines::logs/default_self_metrics::processors"), "googlepolicy")
	assert.NotContains(t, conf.Get("service::pipelines::metrics/default_self_metrics::processors"), "googlepolicy")

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestFilterPolicyDriversRegistered(t *testing.T) {
	// Verify that importing googlecontrolplaneprovider registers log_filter,
	// metric_filter, and trace_filter policy drivers in googlepolicy.
	for _, tc := range []struct {
		policyType string
		raw        map[string]any
	}{
		{
			policyType: "log_filter",
			raw: map[string]any{
				"type":   "log_filter",
				"id":     "test-log",
				"action": "ACTION_DROP",
				"matches": []any{
					map[string]any{
						"target": map[string]any{"record_field": "LOG_RECORD_FIELD_BODY"},
						"exists": map[string]any{},
					},
				},
			},
		},
		{
			policyType: "metric_filter",
			raw: map[string]any{
				"type":   "metric_filter",
				"id":     "test-metric",
				"action": "ACTION_DROP",
				"matches": []any{
					map[string]any{
						"target": map[string]any{"descriptor_field": "METRIC_DESCRIPTOR_FIELD_NAME"},
						"exists": map[string]any{},
					},
				},
			},
		},
		{
			policyType: "trace_filter",
			raw: map[string]any{
				"type":   "trace_filter",
				"id":     "test-trace",
				"action": "ACTION_DROP",
				"matches": []any{
					map[string]any{
						"target": map[string]any{"record_field": "SPAN_RECORD_FIELD_NAME"},
						"exists": map[string]any{},
					},
				},
			},
		},
	} {
		t.Run(tc.policyType, func(t *testing.T) {
			pol, err := googlepolicy.LoadPolicy(tc.policyType, tc.raw)
			require.NoError(t, err, "driver for %s must be registered", tc.policyType)
			assert.NotNil(t, pol)
		})
	}
}

func TestRetrieve_RecordsPolicyEvaluateErrorEventOnFailure(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	p := createProviderWithLogger(logger)

	failingSource := &mockSourcePolicy{
		name:    "failing_source",
		evalErr: errors.New("boom"),
	}
	validSource := &mockSourcePolicy{
		name: "valid_source",
	}
	ps := &googlepolicy.PolicySet{
		RevisionID: "886313e1-3b8a-5372-9b90-0c9aee199e5d",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"failing_source": {PolicyObj: failingSource},
			"valid_source":   {PolicyObj: validSource},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.NoError(t, err)
	require.NotNil(t, ret)

	// Active policy set remains active (not rolled back) because individual policy failures are non-fatal
	require.NotNil(t, googlepolicy.ActivePolicySet())
	assert.Equal(t, "886313e1-3b8a-5372-9b90-0c9aee199e5d", googlepolicy.ActivePolicySet().RevisionID)

	conf, err := ret.AsConf()
	require.NoError(t, err)
	assert.True(t, conf.IsSet("receivers::otlp/valid_source"))
	assert.False(t, conf.IsSet("receivers::otlp/failing_source"))
	assert.True(t, conf.IsSet("exporters::otlp_grpc/default_gcp_destination"))
	assert.True(t, conf.IsSet("receivers::otlp/default_self_metrics"))

	entries := recorded.All()
	require.Len(t, entries, 1)
	entry := entries[0]
	assert.Equal(t, zapcore.ErrorLevel, entry.Level)
	assert.Contains(t, entry.Message, "failed to evaluate source policy \"failing_source\": boom")
	assert.Equal(t, map[string]any{
		"event.name":             event.PolicyEvaluateErrorEventName,
		"policy.id":              "failing_source",
		"policy.set.revision.id": "886313e1-3b8a-5372-9b90-0c9aee199e5d",
		"context":                "context.Background.WithValue(FLEET_ID, 1234)",
	}, entry.ContextMap())

	assert.NoError(t, p.Shutdown(context.Background()))
}
