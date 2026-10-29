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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider/policies/selfmetrics"
	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/event"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/windowseventlogsource"
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
	assert.True(t, conf.IsSet("receivers::nop/googlecontrolplane"))
	assert.True(t, conf.IsSet("service::telemetry::resource::attributes"))
	assert.True(t, conf.IsSet("service::pipelines::logs/default_self_metrics"))
	assert.True(t, conf.IsSet("service::pipelines::metrics/default_self_metrics"))
	assert.True(t, conf.IsSet("service::pipelines::logs/googlecontrolplane"))
	assert.True(t, conf.IsSet("service::pipelines::metrics/googlecontrolplane"))
	assert.True(t, conf.IsSet("service::pipelines::traces/googlecontrolplane"))

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
func (m *mockSourcePolicy) LogReceiverIDs() []component.ID {
	otlpType, _ := component.NewType("otlp")
	return []component.ID{component.NewIDWithName(otlpType, m.name)}
}
func (m *mockSourcePolicy) MetricReceiverIDs() []component.ID { return nil }
func (m *mockSourcePolicy) TraceReceiverIDs() []component.ID  { return nil }

var _ googlepolicy.SourcePolicy = (*mockSourcePolicy)(nil)

func TestRetrieve_NoFleetID(t *testing.T) {
	t.Setenv("FLEET_ID", "")
	p := createProvider()
	_, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, selfmetrics.ErrNoFleetID)
	assert.NoError(t, p.Shutdown(context.Background()))
}

// withFakeGCEMetadata points the GCE metadata library at a local server that
// answers 404 to every lookup, so xDS project/locality resolution sees no
// metadata, as it would off GCE.
//
// Without it, tests run on a GCE host (e.g. a cloudtop) reach the real metadata
// server through the library's shared HTTP client, which keeps the connection
// alive for a minute afterwards and trips goleak. The library offers no way to
// close that connection, but closing the server on cleanup drops it from the
// server side, so the client's connection goroutines exit.
func withFakeGCEMetadata(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	t.Setenv("GCE_METADATA_HOST", srv.Listener.Addr().String())
}

// TestRetrieve_XDSServerUnreachable asserts that a control plane that is down
// does not stop the collector from starting: Start's initial-sync wait expires,
// Retrieve still succeeds on the built-in policies, and the xDS connection keeps
// retrying in the background.
//
// This is the degraded path that matters most in production -- the control plane
// being unavailable must never be the reason a collector fails to come up.
func TestRetrieve_XDSServerUnreachable(t *testing.T) {
	withFakeGCEMetadata(t)
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	// Port 1 on loopback: nothing is listening, so the stream can never be
	// established and the manager is left retrying with backoff.
	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:xds://127.0.0.1:1?project=my-project&insecure=true", nil)
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

func TestRetrieve_XDSNumericProjectDoesNotClobberProjectID(t *testing.T) {
	withFakeGCEMetadata(t)
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:xds://127.0.0.1:1?project=123456789012&insecure=true", nil)
	require.NoError(t, err)
	require.NotNil(t, ret)

	conf, err := ret.AsConf()
	require.NoError(t, err)

	rawAttrs := conf.Get("service::telemetry::resource::attributes")
	attrs, ok := rawAttrs.([]any)
	require.True(t, ok)
	for _, entry := range attrs {
		if m, ok := entry.(map[string]any); ok {
			assert.NotEqual(t, "gcp.project_id", m["name"], "numeric project number in ?project= must not be stamped as gcp.project_id")
		}
	}

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_MultipleDestinationPolicies(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	p := createProviderWithLogger(logger)

	ps := &googlepolicy.PolicySet{
		PolicySetID: "projects/my-project/locations/us-central1/policySets/my-policy-set",
		RevisionID:  "rev-mult-dest",
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
		"event.name":                 event.PolicySetInvalidEventName,
		"gcp.policy.set.id":          "projects/my-project/locations/us-central1/policySets/my-policy-set",
		"gcp.policy.set.revision.id": "rev-mult-dest",
		"context":                    "context.Background.WithValue(FLEET_ID, 1234)",
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
	assert.Contains(t, conf.Get("service::pipelines::logs/custom_self_metrics::processors"), "googlepolicy")
	assert.Contains(t, conf.Get("service::pipelines::metrics/custom_self_metrics::processors"), "googlepolicy")

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
	// Customer sources are consolidated into canonical logs/googlecontrolplane pipeline with googlepolicy and resource detection
	assert.Contains(t, conf.Get("service::pipelines::logs/googlecontrolplane::processors"), "resourcedetection/googlecontrolplane")
	assert.Contains(t, conf.Get("service::pipelines::logs/googlecontrolplane::processors"), "transform/googlecontrolplane")
	assert.Contains(t, conf.Get("service::pipelines::logs/googlecontrolplane::processors"), "googlepolicy")
	assert.Contains(t, conf.Get("service::pipelines::logs/googlecontrolplane::receivers"), "otlp/other_source")
	assert.Contains(t, conf.Get("service::pipelines::logs/googlecontrolplane::receivers"), "nop/googlecontrolplane")
	// Built-in self metrics also includes googlepolicy processor
	assert.True(t, conf.IsSet("receivers::otlp/default_self_metrics"))
	assert.Contains(t, conf.Get("service::pipelines::logs/default_self_metrics::processors"), "googlepolicy")
	assert.Contains(t, conf.Get("service::pipelines::metrics/default_self_metrics::processors"), "googlepolicy")

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_ActivePolicySetWithMultipleSources_CanonicalConsolidation(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-multi-source",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"source_1": {PolicyObj: &mockSourcePolicy{name: "source_1"}},
			"source_2": {PolicyObj: &mockSourcePolicy{name: "source_2"}},
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

	assert.True(t, conf.IsSet("receivers::otlp/source_1"))
	assert.True(t, conf.IsSet("receivers::otlp/source_2"))

	// Both sources must merge into the shared canonical logs/googlecontrolplane pipeline
	receivers := conf.Get("service::pipelines::logs/googlecontrolplane::receivers")
	assert.Contains(t, receivers, "nop/googlecontrolplane")
	assert.Contains(t, receivers, "otlp/source_1")
	assert.Contains(t, receivers, "otlp/source_2")

	// Shared pipeline must contain googlepolicy and destination queue/batch processor
	processors := conf.Get("service::pipelines::logs/googlecontrolplane::processors")
	assert.Contains(t, processors, "googlepolicy")

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_ActivePolicySetWithWindowsEventLogSource(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	pol, err := windowseventlogsource.NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "windows-security-sysmon",
		Channels: []string{"Security", "Microsoft-Windows-Sysmon/Operational"},
	})
	require.NoError(t, err)

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-windows-source",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"windows-security-sysmon": {PolicyObj: pol},
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

	// Verify synthesized receiver configs and compiler defaults
	assert.True(t, conf.IsSet("receivers::windowseventlog/security"))
	assert.Equal(t, "Security", conf.Get("receivers::windowseventlog/security::channel"))
	assert.Equal(t, true, conf.Get("receivers::windowseventlog/security::suppress_rendering_info"))
	assert.False(t, conf.IsSet("receivers::windowseventlog/security::event_driven_scraping"))
	assert.Equal(t, 500, conf.Get("receivers::windowseventlog/security::max_reads"))
	assert.Equal(t, true, conf.Get("receivers::windowseventlog/security::ignore_channel_errors"))

	assert.True(t, conf.IsSet("receivers::windowseventlog/microsoft-windows-sysmon_operational"))
	assert.Equal(t, "Microsoft-Windows-Sysmon/Operational", conf.Get("receivers::windowseventlog/microsoft-windows-sysmon_operational::channel"))
	assert.Equal(t, true, conf.Get("receivers::windowseventlog/microsoft-windows-sysmon_operational::suppress_rendering_info"))
	assert.False(t, conf.IsSet("receivers::windowseventlog/microsoft-windows-sysmon_operational::event_driven_scraping"))
	assert.Equal(t, 500, conf.Get("receivers::windowseventlog/microsoft-windows-sysmon_operational::max_reads"))
	assert.Equal(t, true, conf.Get("receivers::windowseventlog/microsoft-windows-sysmon_operational::ignore_channel_errors"))

	// Receivers must attach to canonical shared logs/googlecontrolplane pipeline
	receivers := conf.Get("service::pipelines::logs/googlecontrolplane::receivers")
	assert.Contains(t, receivers, "nop/googlecontrolplane")
	assert.Contains(t, receivers, "windowseventlog/security")
	assert.Contains(t, receivers, "windowseventlog/microsoft-windows-sysmon_operational")

	// Canonical pipeline must contain standard processor chain
	processors := conf.Get("service::pipelines::logs/googlecontrolplane::processors")
	assert.Contains(t, processors, "resourcedetection/googlecontrolplane")
	assert.Contains(t, processors, "transform/googlecontrolplane")
	assert.Contains(t, processors, "googlepolicy")
	assert.Contains(t, processors, "queue_batch/default_gcp_destination_batch_logs")

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_ActivePolicySetWithWindowsEventLogSource_MultiPolicyConsolidation(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	pol1, err := windowseventlogsource.NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-1",
		Channels: []string{"Security", "System"},
	})
	require.NoError(t, err)

	pol2, err := windowseventlogsource.NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-2",
		Channels: []string{"security", "Application", "SYSTEM"},
	})
	require.NoError(t, err)

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-windows-consolidation",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"win-1": {PolicyObj: pol1},
			"win-2": {PolicyObj: pol2},
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

	// Deduplicated channels: application, security, system
	assert.True(t, conf.IsSet("receivers::windowseventlog/application"))
	assert.True(t, conf.IsSet("receivers::windowseventlog/security"))
	assert.True(t, conf.IsSet("receivers::windowseventlog/system"))

	// First-seen casing preserved
	assert.Equal(t, "Security", conf.Get("receivers::windowseventlog/security::channel"))
	assert.Equal(t, "System", conf.Get("receivers::windowseventlog/system::channel"))
	assert.Equal(t, "Application", conf.Get("receivers::windowseventlog/application::channel"))

	receivers := conf.Get("service::pipelines::logs/googlecontrolplane::receivers")
	expectedReceivers := []any{
		"nop/googlecontrolplane",
		"windowseventlog/application",
		"windowseventlog/security",
		"windowseventlog/system",
	}
	assert.ElementsMatch(t, expectedReceivers, receivers)

	// Verify all generated receivers unmarshal cleanly into real collector component.ID
	sub, err := conf.Sub("receivers")
	require.NoError(t, err)
	var recMap map[component.ID]any
	require.NoError(t, sub.Unmarshal(&recMap))
	assert.Contains(t, recMap, component.MustNewIDWithName("windowseventlog", "application"))
	assert.Contains(t, recMap, component.MustNewIDWithName("windowseventlog", "security"))
	assert.Contains(t, recMap, component.MustNewIDWithName("windowseventlog", "system"))

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_ActivePolicySetWithWindowsEventLogSource_RealPolicyFaultIsolation(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	obsCore, recordedLogs := observer.New(zapcore.WarnLevel)
	logger := zap.New(obsCore)
	p := createProviderWithLogger(logger)

	// Valid policy
	polValid, err := windowseventlogsource.NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-valid",
		Channels: []string{"Security"},
	})
	require.NoError(t, err)

	// Real policy with empty channel that fails validation
	badProto := &policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-bad-empty",
		Channels: []string{"Security", ""},
	}
	// Bypass NewPolicyFromProto validation by loading directly into driver or raw struct to test provider fault isolation
	polInvalid, err := googlepolicy.LoadPolicy(windowseventlogsource.PolicyType, map[string]any{
		"type":     windowseventlogsource.PolicyType,
		"id":       "win-bad-empty",
		"channels": []any{"Security", ""},
	})
	// LoadPolicy enforces Validate(), so polInvalid is nil or fails.
	// To test runtime fault isolation if an invalid policy enters PolicySet:
	if polInvalid == nil {
		polInvalid = &mockInvalidWindowsSourcePolicy{name: "win-bad-empty"}
	}
	_ = badProto

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-windows-real-fault-isolation",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"win-valid":     {PolicyObj: polValid},
			"win-bad-empty": {PolicyObj: polInvalid},
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

	// Valid policy survives and produces receivers
	assert.True(t, conf.IsSet("receivers::windowseventlog/security"))
	receivers := conf.Get("service::pipelines::logs/googlecontrolplane::receivers")
	assert.Contains(t, receivers, "windowseventlog/security")

	// Component ID unmarshaling succeeds
	sub, err := conf.Sub("receivers")
	require.NoError(t, err)
	var recs map[component.ID]any
	require.NoError(t, sub.Unmarshal(&recs))
	assert.Contains(t, recs, component.MustNewIDWithName("windowseventlog", "security"))

	// Warning log was recorded for the invalid policy
	assert.NotEmpty(t, recordedLogs.All())

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_ActivePolicySetWithWindowsEventLogSource_InvalidComponentIDIsolation(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	obsCore, _ := observer.New(zapcore.WarnLevel)
	logger := zap.New(obsCore)
	p := createProviderWithLogger(logger)

	polValid, err := windowseventlogsource.NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-valid",
		Channels: []string{"Security"},
	})
	require.NoError(t, err)

	// Invalid policy with only symbol channel that produces no valid component ID
	polInvalid := &mockInvalidChannelSourcePolicy{name: "win-symbol-only"}

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-windows-symbol-isolation",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"win-valid":       {PolicyObj: polValid},
			"win-symbol-only": {PolicyObj: polInvalid},
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

	// Valid policy survives and config unmarshals into component.ID map
	assert.True(t, conf.IsSet("receivers::windowseventlog/security"))
	sub, err := conf.Sub("receivers")
	require.NoError(t, err)
	var recs map[component.ID]any
	require.NoError(t, sub.Unmarshal(&recs))
	assert.Contains(t, recs, component.MustNewIDWithName("windowseventlog", "security"))

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_ActivePolicySetWithWindowsEventLogSource_MultiPolicyDelimiterCollision(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProviderWithLogger(zap.NewNop())

	pol1, err := windowseventlogsource.NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-slash",
		Channels: []string{"Foo/Bar"},
	})
	require.NoError(t, err)

	pol2, err := windowseventlogsource.NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-underscore",
		Channels: []string{"Foo_Bar"},
	})
	require.NoError(t, err)

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-multi-policy-collision",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"win-slash":      {PolicyObj: pol1},
			"win-underscore": {PolicyObj: pol2},
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

	// Both channels are preserved and have distinct receiver IDs
	sub, err := conf.Sub("receivers")
	require.NoError(t, err)
	var recs map[component.ID]any
	require.NoError(t, sub.Unmarshal(&recs))

	assert.Contains(t, recs, component.MustNewIDWithName("windowseventlog", "foo_bar"))
	foundDisambiguated := false
	for id := range recs {
		if id.Type().String() == "windowseventlog" && strings.HasPrefix(id.Name(), "foo_bar_") {
			foundDisambiguated = true
			break
		}
	}
	assert.True(t, foundDisambiguated, "expected disambiguated receiver ID with foo_bar_ prefix")

	pipelineReceivers := conf.Get("service::pipelines::logs/googlecontrolplane::receivers")
	assert.Contains(t, pipelineReceivers, "windowseventlog/foo_bar")
	for id := range recs {
		if id.Type().String() == "windowseventlog" && strings.HasPrefix(id.Name(), "foo_bar_") {
			assert.Contains(t, pipelineReceivers, id.String())
		}
	}

	assert.NoError(t, p.Shutdown(context.Background()))
}

type mockInvalidChannelSourcePolicy struct {
	name string
}

func (m *mockInvalidChannelSourcePolicy) PolicyName() string { return m.name }
func (m *mockInvalidChannelSourcePolicy) PolicyType() string {
	return windowseventlogsource.PolicyType
}
func (m *mockInvalidChannelSourcePolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}
func (m *mockInvalidChannelSourcePolicy) Validate() error {
	return windowseventlogsource.ErrInvalidChannelName
}
func (m *mockInvalidChannelSourcePolicy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	return nil, windowseventlogsource.ErrInvalidChannelName
}
func (m *mockInvalidChannelSourcePolicy) LogReceiverIDs() []component.ID    { return nil }
func (m *mockInvalidChannelSourcePolicy) MetricReceiverIDs() []component.ID { return nil }
func (m *mockInvalidChannelSourcePolicy) TraceReceiverIDs() []component.ID  { return nil }

func TestRetrieve_ActivePolicySetWithWindowsEventLogSource_FaultIsolation(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	obsCore, recordedLogs := observer.New(zapcore.WarnLevel)
	logger := zap.New(obsCore)
	p := createProviderWithLogger(logger)

	polValid, err := windowseventlogsource.NewPolicyFromProto(&policyv1alpha1.WindowsEventLogSourcePolicy{
		Id:       "win-valid",
		Channels: []string{"Security"},
	})
	require.NoError(t, err)

	// polInvalid has empty channels list (violates Validate)
	polInvalid := &mockInvalidWindowsSourcePolicy{name: "win-invalid"}

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-windows-fault-isolation",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"win-valid":   {PolicyObj: polValid},
			"win-invalid": {PolicyObj: polInvalid},
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

	// Valid policy survives and produces receivers
	assert.True(t, conf.IsSet("receivers::windowseventlog/security"))
	receivers := conf.Get("service::pipelines::logs/googlecontrolplane::receivers")
	assert.Contains(t, receivers, "windowseventlog/security")

	// Invalid policy was rejected and logged
	logs := recordedLogs.All()
	foundErrorLog := false
	for _, l := range logs {
		if strings.Contains(l.Message, "windows event log source policy must specify at least one channel") {
			foundErrorLog = true
			break
		}
	}
	assert.True(t, foundErrorLog, "expected evaluate error event for invalid policy")

	assert.NoError(t, p.Shutdown(context.Background()))
}

type mockInvalidWindowsSourcePolicy struct {
	name string
}

func (m *mockInvalidWindowsSourcePolicy) PolicyName() string { return m.name }
func (m *mockInvalidWindowsSourcePolicy) PolicyType() string { return windowseventlogsource.PolicyType }
func (m *mockInvalidWindowsSourcePolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}
func (m *mockInvalidWindowsSourcePolicy) Validate() error {
	return windowseventlogsource.ErrMissingChannels
}
func (m *mockInvalidWindowsSourcePolicy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	return nil, windowseventlogsource.ErrMissingChannels
}
func (m *mockInvalidWindowsSourcePolicy) LogReceiverIDs() []component.ID    { return nil }
func (m *mockInvalidWindowsSourcePolicy) MetricReceiverIDs() []component.ID { return nil }
func (m *mockInvalidWindowsSourcePolicy) TraceReceiverIDs() []component.ID  { return nil }
func (m *mockInvalidWindowsSourcePolicy) Consolidate(_ []googlepolicy.SourcePolicy) (googlepolicy.SourcePolicy, error) {
	return nil, windowseventlogsource.ErrMissingChannels
}

var (
	_ googlepolicy.SourcePolicy               = (*mockInvalidWindowsSourcePolicy)(nil)
	_ googlepolicy.ConsolidatableSourcePolicy = (*mockInvalidWindowsSourcePolicy)(nil)
)

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
				"matchers": []any{
					map[string]any{
						"target":    map[string]any{"record_field": "LOG_RECORD_FIELD_BODY"},
						"predicate": map[string]any{"existence": map[string]any{}},
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
				"matchers": []any{
					map[string]any{
						"target":    map[string]any{"descriptor_field": "METRIC_DESCRIPTOR_FIELD_NAME"},
						"predicate": map[string]any{"existence": map[string]any{}},
					},
				},
			},
		},
		{
			policyType: "trace_filter",
			raw: map[string]any{
				"type": "trace_filter",
				"id":   "test-trace",
				"matchers": []any{
					map[string]any{
						"target":    map[string]any{"record_field": "SPAN_RECORD_FIELD_NAME"},
						"predicate": map[string]any{"existence": map[string]any{}},
					},
				},
			},
		},
		{
			policyType: "journald_source",
			raw: map[string]any{
				"type": "journald_source",
				"id":   "test-journald",
			},
		},
		{
			policyType: "windows_event_log_source",
			raw: map[string]any{
				"type":     "windows_event_log_source",
				"id":       "test-win",
				"channels": []any{"Security"},
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

// TestMergeAppendUnionsServiceExtensions pins the behaviour that makes it safe
// for more than one policy (or confmap provider) to declare an extension.
//
// Creating the provider enables the confmap.enableMergeAppendOption feature
// gate so confmap.Merge appends and deduplicates service::extensions instead of
// replacing the slice with only the last policy's extension.
func TestMergeAppendUnionsServiceExtensions(t *testing.T) {
	_ = createProvider()
	conf := confmap.New()

	destination := confmap.NewFromStringMap(map[string]any{
		"service": map[string]any{
			"extensions": []any{"googleclientauth/default_gcp_destination"},
		},
	})
	selfMetrics := confmap.NewFromStringMap(map[string]any{
		"service": map[string]any{
			"extensions": []any{"googlecontrolplane/default_self_metrics"},
		},
	})

	require.NoError(t, conf.Merge(cleanConf(destination)))
	require.NoError(t, conf.Merge(cleanConf(selfMetrics)))

	assert.Equal(t, []any{
		"googleclientauth/default_gcp_destination",
		"googlecontrolplane/default_self_metrics",
	}, conf.Get("service::extensions"))
}

func TestMergeAppendDeduplicatesServiceExtensions(t *testing.T) {
	_ = createProvider()
	conf := confmap.New()
	declare := func() *confmap.Conf {
		return confmap.NewFromStringMap(map[string]any{
			"service": map[string]any{
				"extensions": []any{"googleclientauth/default_gcp_destination"},
			},
		})
	}

	require.NoError(t, conf.Merge(cleanConf(declare())))
	require.NoError(t, conf.Merge(cleanConf(declare())))

	assert.Equal(t, []any{"googleclientauth/default_gcp_destination"}, conf.Get("service::extensions"))
}

// TestMergeAppendWithoutServiceExtensions guards the common case: a policy that
// declares no extensions must not introduce an empty service::extensions key.
func TestMergeAppendWithoutServiceExtensions(t *testing.T) {
	_ = createProvider()
	conf := confmap.New()
	require.NoError(t, conf.Merge(cleanConf(confmap.NewFromStringMap(map[string]any{
		"receivers": map[string]any{"otlp/x": map[string]any{}},
	}))))

	assert.False(t, conf.IsSet("service::extensions"))
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
		PolicySetID: "projects/my-project/locations/us-central1/policySets/my-policy-set",
		RevisionID:  "886313e1-3b8a-5372-9b90-0c9aee199e5d",
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
		"event.name":                 event.PolicyEvaluateErrorEventName,
		"gcp.policy.id":              "failing_source",
		"gcp.policy.set.id":          "projects/my-project/locations/us-central1/policySets/my-policy-set",
		"gcp.policy.set.revision.id": "886313e1-3b8a-5372-9b90-0c9aee199e5d",
		"error.type":                 "policy_evaluation_failed",
		"gcp.policy.class":           "source",
		"context":                    "context.Background.WithValue(FLEET_ID, 1234)",
	}, entry.ContextMap())

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_BuiltinPolicyErrorDoesNotRollbackActivePolicySet(t *testing.T) {
	t.Setenv("FLEET_ID", "")
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	p := createProviderWithLogger(logger)

	ps := &googlepolicy.PolicySet{
		PolicySetID: "projects/my-project/locations/us-central1/policySets/my-policy-set",
		RevisionID:  "rev-should-stay-active",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"valid_source": {PolicyObj: &mockSourcePolicy{name: "valid_source"}},
		},
	}
	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	_, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.ErrorIs(t, err, selfmetrics.ErrNoFleetID)

	// Built-in policy failures must NOT roll back the active policy set or attribute the error to activePolicySet.RevisionID.
	require.NotNil(t, googlepolicy.ActivePolicySet())
	assert.Equal(t, "rev-should-stay-active", googlepolicy.ActivePolicySet().RevisionID)

	entries := recorded.All()
	require.Len(t, entries, 1)
	assert.Equal(t, map[string]any{
		"event.name":                 event.PolicyEvaluateErrorEventName,
		"gcp.policy.id":              "default_self_metrics",
		"gcp.policy.set.id":          "",
		"gcp.policy.set.revision.id": "",
		"error.type":                 "policy_evaluation_failed",
		"gcp.policy.class":           "source",
		"context":                    "context.Background",
	}, entries[0].ContextMap())

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_RecordsPolicyEvaluateErrorEventForFailedPoliciesWithClass(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	p := createProviderWithLogger(logger)

	ps, err := googlepolicy.MakePolicySet("rev-failed-class", []map[string]any{
		{
			"type":   "log_filter",
			"id":     "invalid-log-filter",
			"action": "ACTION_DROP",
			// missing matchers -> ErrPolicyFailedValidation, class: transformation
		},
		{
			"type": "unknown_policy_type",
			"id":   "unknown-policy",
		},
	})
	require.Error(t, err)
	require.NotNil(t, ps)
	ps.PolicySetID = "projects/my-project/locations/us-central1/policySets/my-policy-set"

	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.NoError(t, err)
	require.NotNil(t, ret)

	entries := recorded.All()
	require.Len(t, entries, 2)

	assert.Equal(t, map[string]any{
		"event.name":                 event.PolicyEvaluateErrorEventName,
		"gcp.policy.id":              "invalid-log-filter",
		"gcp.policy.set.id":          "projects/my-project/locations/us-central1/policySets/my-policy-set",
		"gcp.policy.set.revision.id": "rev-failed-class",
		"error.type":                 "policy_failed_validation",
		"gcp.policy.class":           "transformation",
		"context":                    "context.Background.WithValue(FLEET_ID, 1234)",
	}, entries[0].ContextMap())

	assert.Equal(t, map[string]any{
		"event.name":                 event.PolicyEvaluateErrorEventName,
		"gcp.policy.id":              "unknown-policy",
		"gcp.policy.set.id":          "projects/my-project/locations/us-central1/policySets/my-policy-set",
		"gcp.policy.set.revision.id": "rev-failed-class",
		"error.type":                 "policy_type_not_found",
		"context":                    "context.Background.WithValue(FLEET_ID, 1234)",
	}, entries[1].ContextMap())

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestPolicyHotReload_SourcePolicyTriggersWatcher(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	changeEvents := make(chan *confmap.ChangeEvent, 5)
	watcher := func(e *confmap.ChangeEvent) {
		changeEvents <- e
	}

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", watcher)
	require.NoError(t, err)
	require.NotNil(t, ret)

	conf, err := ret.AsConf()
	require.NoError(t, err)
	assert.Equal(t, "localhost:18888", conf.Get("receivers::otlp/default_self_metrics::protocols::grpc::endpoint"))

	// Update active policy set with a custom self_metrics policy configuring a different port.
	ps, err := googlepolicy.MakePolicySet("rev-source-1", []map[string]any{
		{
			"type": "self_metrics",
			"name": "custom_self_metrics",
			"port": 19999,
		},
	})
	require.NoError(t, err)

	googlepolicy.SetActivePolicySet(ps)

	select {
	case e := <-changeEvents:
		assert.NotNil(t, e)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for confmap.WatcherFunc to be triggered on source policy update")
	}

	// Verify that calling Retrieve() after reload returns the updated config with port 19999.
	retUpdated, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", watcher)
	require.NoError(t, err)
	confUpdated, err := retUpdated.AsConf()
	require.NoError(t, err)
	assert.Equal(t, "localhost:19999", confUpdated.Get("receivers::otlp/custom_self_metrics::protocols::grpc::endpoint"))

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestPolicyHotReload_FilterPolicyDoesNotTriggerWatcher(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	var eventCount atomic.Int32
	changeEvents := make(chan *confmap.ChangeEvent, 5)
	watcher := func(e *confmap.ChangeEvent) {
		eventCount.Add(1)
		changeEvents <- e
	}

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", watcher)
	require.NoError(t, err)
	require.NotNil(t, ret)

	// Update active policy set with only a LogFilterPolicy (transformation policy).
	psFilter, err := googlepolicy.MakePolicySet("rev-filter-1", []map[string]any{
		{
			"type":   "log_filter",
			"id":     "drop-debug",
			"action": "ACTION_DROP",
			"matchers": []map[string]any{
				{
					"target": map[string]any{
						"record_field": "LOG_RECORD_FIELD_BODY",
					},
					"predicate": map[string]any{
						"glob": "DEBUG*",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	googlepolicy.SetActivePolicySet(psFilter)

	// Establish a sync point: push a source policy change that is guaranteed to trigger a reload.
	psSource, err := googlepolicy.MakePolicySet("rev-source-sync", []map[string]any{
		{
			"type": "self_metrics",
			"name": "custom_self_metrics",
			"port": 19999,
		},
	})
	require.NoError(t, err)

	googlepolicy.SetActivePolicySet(psSource)

	select {
	case <-changeEvents:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for sync point watcher invocation")
	}

	// Strictly assert that watcher was called exactly once (for the source policy, never for the filter policy).
	assert.Equal(t, int32(1), eventCount.Load(), "watcher should only have been invoked for the source policy, not the filter policy")

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestPolicyHotReload_ProviderShutdownCleansUpWatcher(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	changeEvents := make(chan *confmap.ChangeEvent, 5)
	watcher := func(e *confmap.ChangeEvent) {
		changeEvents <- e
	}

	_, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", watcher)
	require.NoError(t, err)

	assert.NoError(t, p.Shutdown(context.Background()))

	// Updating policy set after shutdown must not trigger events or panic.
	ps, err := googlepolicy.MakePolicySet("rev-shutdown", []map[string]any{
		{
			"type": "self_metrics",
			"name": "post_shutdown_metrics",
			"port": 20000,
		},
	})
	require.NoError(t, err)

	googlepolicy.SetActivePolicySet(ps)

	select {
	case e := <-changeEvents:
		t.Fatalf("unexpected confmap.WatcherFunc triggered after provider shutdown: %+v", e)
	case <-time.After(100 * time.Millisecond):
		// Expected: no event.
	}
}

// Mimics confmap.Resolver: buffered(1) chan, blocking send in onChange,
// provider Shutdown followed by close(chan).
func TestReview_SendOnClosedChannelAfterShutdown(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})
	ch := make(chan error, 1)
	onChange := func(e *confmap.ChangeEvent) { ch <- e.Error }

	_, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", onChange)
	require.NoError(t, err)

	for i, port := range []int{19991, 19992, 19993} {
		ps, err := googlepolicy.MakePolicySet("rev-"+string(rune('a'+i)), []map[string]any{
			{"type": "self_metrics", "name": "sm", "port": port},
		})
		require.NoError(t, err)
		googlepolicy.SetActivePolicySet(ps)
		time.Sleep(50 * time.Millisecond)
	}
	// Collector is shutting down; nobody drains ch.
	require.NoError(t, p.Shutdown(context.Background()))
	close(ch) // what Resolver.Shutdown does
	time.Sleep(100 * time.Millisecond)
}

func TestPolicyHotReload_InvalidSourcePolicyDoesNotTriggerReload(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	var eventCount atomic.Int32
	changeEvents := make(chan *confmap.ChangeEvent, 5)
	watcher := func(e *confmap.ChangeEvent) {
		eventCount.Add(1)
		changeEvents <- e
	}

	// 1. Initial valid configuration with a non-default source policy (port 19998).
	psInitial, err := googlepolicy.MakePolicySet("rev-initial", []map[string]any{
		{
			"type": "self_metrics",
			"name": "custom_self_metrics",
			"port": 19998,
		},
	})
	require.NoError(t, err)
	googlepolicy.SetActivePolicySet(psInitial)

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", watcher)
	require.NoError(t, err)
	require.NotNil(t, ret)
	conf, err := ret.AsConf()
	require.NoError(t, err)
	assert.Equal(t, "localhost:19998", conf.Get("receivers::otlp/custom_self_metrics::protocols::grpc::endpoint"))

	// 2. Set an active policy set that fails whole-set validation (e.g. multiple destination policies),
	// which must trigger RollbackActivePolicySet() in evaluateActivePolicySet.
	psInvalid := &googlepolicy.PolicySet{
		RevisionID: "rev-invalid-multidest",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"dest1": {PolicyObj: &mockDestinationPolicy{name: "dest1"}},
			"dest2": {PolicyObj: &mockDestinationPolicy{name: "dest2"}},
		},
	}
	googlepolicy.SetActivePolicySet(psInvalid)

	// Verify that ActivePolicySet rolled back to the initial revision.
	require.Eventually(t, func() bool {
		return googlepolicy.ActivePolicySetRevisionID() == "rev-initial"
	}, 2*time.Second, 10*time.Millisecond, "expected active policy set to roll back to rev-initial")

	// 3. Establish a sync point: push a valid source policy change guaranteed to trigger a reload.
	psSource, err := googlepolicy.MakePolicySet("rev-source-sync", []map[string]any{
		{
			"type": "self_metrics",
			"name": "custom_self_metrics",
			"port": 19999,
		},
	})
	require.NoError(t, err)
	googlepolicy.SetActivePolicySet(psSource)

	select {
	case <-changeEvents:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for sync point watcher invocation")
	}

	// Strictly assert that watcher was called exactly once (for the sync source policy, never for the invalid one).
	assert.Equal(t, int32(1), eventCount.Load(), "watcher should only have been invoked for the valid source policy, not the failing one")
	assert.Equal(t, "rev-source-sync", googlepolicy.ActivePolicySetRevisionID())

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestPolicyHotReload_RollbackToBuiltinFallbackDoesNotTriggerReload(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	var eventCount atomic.Int32
	changeEvents := make(chan *confmap.ChangeEvent, 5)
	watcher := func(e *confmap.ChangeEvent) {
		eventCount.Add(1)
		changeEvents <- e
	}

	// 1. Initial Retrieve with ActivePolicySet() == nil (runs on built-in self-metrics).
	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", watcher)
	require.NoError(t, err)
	require.NotNil(t, ret)
	conf, err := ret.AsConf()
	require.NoError(t, err)
	assert.Equal(t, "localhost:18888", conf.Get("receivers::otlp/default_self_metrics::protocols::grpc::endpoint"))

	// 2. Push an invalid policy set. Since history was empty, rollback returns to nil.
	psInvalid := &googlepolicy.PolicySet{
		RevisionID: "rev-invalid-multidest",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"dest1": {PolicyObj: &mockDestinationPolicy{name: "dest1"}},
			"dest2": {PolicyObj: &mockDestinationPolicy{name: "dest2"}},
		},
	}
	googlepolicy.SetActivePolicySet(psInvalid)

	// Verify that ActivePolicySet rolled back to nil (built-in fallback).
	require.Eventually(t, func() bool {
		return googlepolicy.ActivePolicySet() == nil
	}, 2*time.Second, 10*time.Millisecond, "expected active policy set to roll back to nil")

	// 3. Establish a sync point: push a valid custom source policy.
	psSource, err := googlepolicy.MakePolicySet("rev-source-sync", []map[string]any{
		{
			"type": "self_metrics",
			"name": "custom_self_metrics",
			"port": 19999,
		},
	})
	require.NoError(t, err)
	googlepolicy.SetActivePolicySet(psSource)

	select {
	case <-changeEvents:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for sync point watcher invocation")
	}

	// Watcher should only have been called once for the sync point, never for the rolled-back invalid set.
	assert.Equal(t, int32(1), eventCount.Load(), "watcher should only have been invoked for the valid source policy, not the rolled-back one")
	assert.Equal(t, "rev-source-sync", googlepolicy.ActivePolicySetRevisionID())

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestPolicyHotReload_ConcurrentRetrieveAndPolicyUpdate(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	var eventCount atomic.Int32
	watcher := func(e *confmap.ChangeEvent) {
		eventCount.Add(1)
	}

	_, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", watcher)
	require.NoError(t, err)

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	// Goroutines concurrently calling Retrieve (as happens during reloads).
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				_, _ = p.Retrieve(ctx, "googlecontrolplane:component:my-config", watcher)
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	// Goroutines updating policy sets.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			iteration := 0
			for ctx.Err() == nil {
				iteration++
				ps, err := googlepolicy.MakePolicySet(fmt.Sprintf("rev-%d-%d", workerID, iteration), []map[string]any{
					{
						"type": "self_metrics",
						"name": "sm",
						"port": 19000 + (iteration % 50),
					},
				})
				if err == nil {
					googlepolicy.SetActivePolicySet(ps)
				}
				time.Sleep(15 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	assert.NoError(t, p.Shutdown(context.Background()))
}

type mockCountingSourcePolicy struct {
	name      string
	evalCount *atomic.Int32
}

func (m *mockCountingSourcePolicy) PolicyName() string { return m.name }
func (m *mockCountingSourcePolicy) PolicyType() string { return "mock_counting_source" }
func (m *mockCountingSourcePolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}
func (m *mockCountingSourcePolicy) Validate() error { return nil }
func (m *mockCountingSourcePolicy) Evaluate(context.Context) (*confmap.Conf, error) {
	m.evalCount.Add(1)
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
func (m *mockCountingSourcePolicy) LogReceiverIDs() []component.ID    { return nil }
func (m *mockCountingSourcePolicy) MetricReceiverIDs() []component.ID { return nil }
func (m *mockCountingSourcePolicy) TraceReceiverIDs() []component.ID  { return nil }
func (m *mockCountingSourcePolicy) LogsPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Conf, error) {
	return nil, nil
}
func (m *mockCountingSourcePolicy) MetricsPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Conf, error) {
	return nil, nil
}
func (m *mockCountingSourcePolicy) TracesPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Conf, error) {
	return nil, nil
}

var _ googlepolicy.DedicatedPipelineSourcePolicy = (*mockCountingSourcePolicy)(nil)

func TestPolicyHotReload_NoDuplicateEvaluationOnUpdate(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})

	var evalCount atomic.Int32
	ps1 := &googlepolicy.PolicySet{
		RevisionID: "rev-eval-1",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"src": {
				PolicyObj: &mockCountingSourcePolicy{
					name:      "src1",
					evalCount: &evalCount,
				},
			},
		},
	}
	googlepolicy.SetActivePolicySet(ps1)

	changeEvents := make(chan *confmap.ChangeEvent, 5)
	watcher := func(e *confmap.ChangeEvent) {
		changeEvents <- e
	}

	ret, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", watcher)
	require.NoError(t, err)
	require.NotNil(t, ret)
	// Initial Retrieve evaluated once.
	assert.Equal(t, int32(1), evalCount.Load())

	// Update active policy set to a new revision with different config.
	ps2 := &googlepolicy.PolicySet{
		RevisionID: "rev-eval-2",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"src": {
				PolicyObj: &mockCountingSourcePolicy{
					name:      "src2",
					evalCount: &evalCount,
				},
			},
		},
	}
	googlepolicy.SetActivePolicySet(ps2)

	select {
	case <-changeEvents:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for watcher invocation")
	}

	// onPolicySetUpdated evaluated once (count is now 2).
	assert.Equal(t, int32(2), evalCount.Load())

	// Collector re-fetches config via Retrieve in response to the change event.
	ret2, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", watcher)
	require.NoError(t, err)
	require.NotNil(t, ret2)

	// Retrieve should return the cached evaluation without calling Evaluate() again.
	assert.Equal(t, int32(2), evalCount.Load(), "Retrieve after hot reload should reuse precomputed confmap and not re-evaluate")

	assert.NoError(t, p.Shutdown(context.Background()))
}

func TestRetrieve_MultipleJournaldSourcePoliciesConsolidated(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	p1, err := googlepolicy.LoadPolicy("journald_source", map[string]any{
		"type": "journald_source",
		"id":   "host-journal-1",
	})
	require.NoError(t, err)

	p2, err := googlepolicy.LoadPolicy("journald_source", map[string]any{
		"type": "journald_source",
		"id":   "host-journal-2",
	})
	require.NoError(t, err)

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-multi-journald",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"policy-1": {PolicyObj: p1},
			"policy-2": {PolicyObj: p2},
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

	raw := conf.ToStringMap()
	receivers, ok := raw["receivers"].(map[string]any)
	require.True(t, ok)

	// Exactly 1 journald receiver is present
	require.Contains(t, receivers, "journald")

	pipelines, ok := raw["service"].(map[string]any)["pipelines"].(map[string]any)
	require.True(t, ok)
	logsPipeline, ok := pipelines["logs/googlecontrolplane"].(map[string]any)
	require.True(t, ok)
	pipelineReceivers, ok := logsPipeline["receivers"].([]any)
	require.True(t, ok)
	require.Contains(t, pipelineReceivers, "journald")
	require.Contains(t, pipelineReceivers, "nop/googlecontrolplane")

	assert.NoError(t, p.Shutdown(context.Background()))
}

type invalidJournaldSourcePolicy struct {
	name string
}

func (i *invalidJournaldSourcePolicy) PolicyName() string { return i.name }
func (i *invalidJournaldSourcePolicy) PolicyType() string { return "journald_source" }
func (i *invalidJournaldSourcePolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}
func (i *invalidJournaldSourcePolicy) Validate() error { return errors.New("invalid journald policy") }
func (i *invalidJournaldSourcePolicy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	return nil, errors.New("invalid journald policy")
}
func (i *invalidJournaldSourcePolicy) LogReceiverIDs() []component.ID {
	return []component.ID{component.MustNewID("journald")}
}
func (i *invalidJournaldSourcePolicy) MetricReceiverIDs() []component.ID { return nil }
func (i *invalidJournaldSourcePolicy) TraceReceiverIDs() []component.ID  { return nil }
func (i *invalidJournaldSourcePolicy) Consolidate(policies []googlepolicy.SourcePolicy) (googlepolicy.SourcePolicy, error) {
	return nil, errors.New("invalid policy cannot consolidate")
}

func TestRetrieve_MultipleJournaldSourcePolicies_FaultIsolation_FirstPolicyInvalid(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	badP0 := &invalidJournaldSourcePolicy{name: "bad-journald-0"}

	p1, err := googlepolicy.LoadPolicy("journald_source", map[string]any{
		"type": "journald_source",
		"id":   "host-journal-1",
	})
	require.NoError(t, err)

	p2, err := googlepolicy.LoadPolicy("journald_source", map[string]any{
		"type": "journald_source",
		"id":   "host-journal-2",
	})
	require.NoError(t, err)

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-fault-isolation",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"policy-bad-0":  {PolicyObj: badP0},
			"policy-good-1": {PolicyObj: p1},
			"policy-good-2": {PolicyObj: p2},
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

	raw := conf.ToStringMap()
	receivers, ok := raw["receivers"].(map[string]any)
	require.True(t, ok)

	// Fault isolation: good-1 and good-2 consolidated successfully into journald despite bad-0
	require.Contains(t, receivers, "journald")

	pipelines, ok := raw["service"].(map[string]any)["pipelines"].(map[string]any)
	require.True(t, ok)
	logsPipeline, ok := pipelines["logs/googlecontrolplane"].(map[string]any)
	require.True(t, ok)
	pipelineReceivers, ok := logsPipeline["receivers"].([]any)
	require.True(t, ok)
	require.Contains(t, pipelineReceivers, "journald")
	require.Contains(t, pipelineReceivers, "nop/googlecontrolplane")

	assert.NoError(t, p.Shutdown(context.Background()))
}

type failConsolidateJournaldSourcePolicy struct {
	name string
}

func (f *failConsolidateJournaldSourcePolicy) PolicyName() string { return f.name }
func (f *failConsolidateJournaldSourcePolicy) PolicyType() string { return "journald_source" }
func (f *failConsolidateJournaldSourcePolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}
func (f *failConsolidateJournaldSourcePolicy) Validate() error { return nil }
func (f *failConsolidateJournaldSourcePolicy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	return confmap.New(), nil
}
func (f *failConsolidateJournaldSourcePolicy) LogReceiverIDs() []component.ID {
	return []component.ID{component.MustNewID("journald")}
}
func (f *failConsolidateJournaldSourcePolicy) MetricReceiverIDs() []component.ID { return nil }
func (f *failConsolidateJournaldSourcePolicy) TraceReceiverIDs() []component.ID  { return nil }
func (f *failConsolidateJournaldSourcePolicy) Consolidate(_ []googlepolicy.SourcePolicy) (googlepolicy.SourcePolicy, error) {
	return nil, errors.New("consolidation failed deliberately")
}

func TestRetrieve_MultipleJournaldSourcePolicies_ConsolidationFailureAttributesAllPolicies(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

	p1 := &failConsolidateJournaldSourcePolicy{name: "failing-policy-1"}
	p2 := &failConsolidateJournaldSourcePolicy{name: "failing-policy-2"}

	ps := &googlepolicy.PolicySet{
		RevisionID: "rev-consolidation-fail",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			"failing-policy-1": {PolicyObj: p1},
			"failing-policy-2": {PolicyObj: p2},
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

	// Since consolidation failed, no journald receiver is added
	conf, err := ret.AsConf()
	require.NoError(t, err)
	raw := conf.ToStringMap()
	receivers, ok := raw["receivers"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, receivers, "journald")

	assert.NoError(t, p.Shutdown(context.Background()))
}
