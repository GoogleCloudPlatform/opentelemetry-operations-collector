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

package googlecontrolplaneextension

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/extension/extensiontest"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/extension/googlecontrolplaneextension/internal/metadata"
)

// fakeSource is a PolicyStateSource test double. Because the extension depends
// on the narrow interface rather than on pkg/googlepolicy, these tests never
// touch the process wide policy registry.
type fakeSource struct {
	revisionID  string
	policySetID string
}

func (f fakeSource) ActivePolicySet() PolicySetState {
	return PolicySetState{ID: f.policySetID, Revision: f.revisionID}
}

// newTestExtension wires the extension up to a real SDK MeterProvider backed by
// a manual reader, so tests can assert on the actual emitted metrics.
//
// There is no Config parameter because the extension has nothing to configure:
// everything it reports comes from the source.
func newTestExtension(t *testing.T, source PolicyStateSource) (*controlPlaneExtension, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	set := extensiontest.NewNopSettings(metadata.Type)
	set.TelemetrySettings.MeterProvider = mp

	return newControlPlaneExtension(&Config{}, set, source), reader
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string][]metricdata.DataPoint[int64] {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	out := map[string][]metricdata.DataPoint[int64]{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			g, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				continue
			}
			out[m.Name] = g.DataPoints
		}
	}
	return out
}

func attrsOf(dp metricdata.DataPoint[int64]) map[string]string {
	out := map[string]string{}
	for _, kv := range dp.Attributes.ToSlice() {
		out[string(kv.Key)] = kv.Value.String()
	}
	return out
}

const testPolicySetID = "projects/my-project/locations/us-central1/policySets/demo-ps"

func TestExtension_ReportsActivePolicySet(t *testing.T) {
	ext, reader := newTestExtension(t, fakeSource{
		revisionID:  "rev-123",
		policySetID: testPolicySetID,
	})

	require.NoError(t, ext.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { assert.NoError(t, ext.Shutdown(context.Background())) })

	metrics := collect(t, reader)

	// One metric, one series. This extension reports set level state only;
	// per-policy application is reported as an OTLP event elsewhere.
	require.Len(t, metrics, 1)

	setActive := metrics[metricPolicySetActive]
	require.Len(t, setActive, 1)
	assert.Equal(t, int64(1), setActive[0].Value)
	assert.Equal(t, map[string]string{
		attrPolicySetID:       testPolicySetID,
		attrPolicySetRevision: "rev-123",
	}, attrsOf(setActive[0]))
}

// A collector with no active policy set must still emit a gcp.policy.set.active
// series, otherwise joins against other collector metrics silently drop rows.
func TestExtension_ReportsZeroWhenNoActivePolicySet(t *testing.T) {
	ext, reader := newTestExtension(t, fakeSource{})

	require.NoError(t, ext.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { assert.NoError(t, ext.Shutdown(context.Background())) })

	metrics := collect(t, reader)

	setActive := metrics[metricPolicySetActive]
	require.Len(t, setActive, 1)
	assert.Equal(t, int64(0), setActive[0].Value)
	// Identity is derived from the active policies, so with none active there
	// is nothing to report. Both labels are present but empty rather than
	// absent, so the series shape matches that of an active collector.
	assert.Equal(t, map[string]string{
		attrPolicySetID:       "",
		attrPolicySetRevision: "",
	}, attrsOf(setActive[0]))
}

// The callback is a pull, so a policy set applied after Start is picked up on
// the next collection without any watcher plumbing.
func TestExtension_PicksUpPolicySetChanges(t *testing.T) {
	source := &mutableSource{}
	ext, reader := newTestExtension(t, source)

	require.NoError(t, ext.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { assert.NoError(t, ext.Shutdown(context.Background())) })

	assert.Equal(t, int64(0), collect(t, reader)[metricPolicySetActive][0].Value)

	source.set("rev-456", testPolicySetID)

	metrics := collect(t, reader)
	setActive := metrics[metricPolicySetActive]
	require.Len(t, setActive, 1)
	assert.Equal(t, int64(1), setActive[0].Value)
	attrs := attrsOf(setActive[0])
	assert.Equal(t, "rev-456", attrs[attrPolicySetRevision])
	// Identity is read on every collection, so it tracks the policy set rather
	// than being captured once at Start.
	assert.Equal(t, testPolicySetID, attrs[attrPolicySetID])
}

// Shutdown must unregister the callback so the instruments stop being observed.
func TestExtension_ShutdownUnregistersCallback(t *testing.T) {
	ext, reader := newTestExtension(t, fakeSource{revisionID: "rev-789"})

	require.NoError(t, ext.Start(context.Background(), componenttest.NewNopHost()))
	require.Len(t, collect(t, reader)[metricPolicySetActive], 1)

	require.NoError(t, ext.Shutdown(context.Background()))
	assert.Empty(t, collect(t, reader)[metricPolicySetActive])

	// Shutdown is idempotent.
	assert.NoError(t, ext.Shutdown(context.Background()))
}

// A nil MeterProvider must not be fatal; the extension simply reports nothing.
func TestExtension_NilMeterProviderIsNotFatal(t *testing.T) {
	set := extensiontest.NewNopSettings(metadata.Type)
	set.TelemetrySettings.MeterProvider = nil

	ext := newControlPlaneExtension(&Config{}, set, fakeSource{})
	require.NoError(t, ext.Start(context.Background(), componenttest.NewNopHost()))
	assert.NoError(t, ext.Shutdown(context.Background()))
}

// When identity cannot be derived, for example a collector running file sourced
// policies whose names are not resource names, gcp.policy.set.id must still
// appear as a label, just empty. Omitting the key entirely would give the
// series a different attribute set than a control plane managed collector, so
// a query written against one would not match the other.
func TestExtension_UnderivablePolicySetIDIsEmptyNotAbsent(t *testing.T) {
	ext, reader := newTestExtension(t, fakeSource{revisionID: "rev-123"})

	require.NoError(t, ext.Start(context.Background(), componenttest.NewNopHost()))
	t.Cleanup(func() { assert.NoError(t, ext.Shutdown(context.Background())) })

	setActive := collect(t, reader)[metricPolicySetActive]
	require.Len(t, setActive, 1)

	attrs := attrsOf(setActive[0])
	require.Contains(t, attrs, attrPolicySetID)
	assert.Empty(t, attrs[attrPolicySetID])
}

// Guards against accidental renames. These names are the contract downstream
// dashboards and alerting policies are written against, and a rename is a
// silent breakage rather than a compile error on their side.
//
// The dots survive into Cloud Monitoring in both the metric name and the label
// keys, so these are queried with PromQL's UTF-8 quoted syntax rather than an
// underscored alias.
func TestMetricContractIsStable(t *testing.T) {
	assert.Equal(t, "gcp.policy.set.active", metricPolicySetActive)

	assert.Equal(t, "gcp.policy.set.id", attrPolicySetID)
	assert.Equal(t, "gcp.policy.set.revision.id", attrPolicySetRevision)
}

type mutableSource struct {
	state PolicySetState
}

func (m *mutableSource) ActivePolicySet() PolicySetState { return m.state }
func (m *mutableSource) set(rev, policySetID string) {
	m.state = PolicySetState{ID: policySetID, Revision: rev}
}
