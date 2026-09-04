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
	"io"
	"testing"

	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider/policies/selfmetrics"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/confmaptest"
)

func createProvider() confmap.Provider {
	return NewFactory().Create(confmaptest.NewNopProviderSettings())
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

func TestUnsupportedInnerScheme(t *testing.T) {
	p := createProvider()
	_, err := p.Retrieve(context.Background(), "googlecontrolplane:my-config", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrURIInvalidInnerScheme)
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

type mockADSServer struct {
	discoveryv3.UnimplementedAggregatedDiscoveryServiceServer
	receivedReq chan *discoveryv3.DiscoveryRequest
	sendResp    chan *discoveryv3.DiscoveryResponse
}

func newMockADSServer() *mockADSServer {
	return &mockADSServer{
		receivedReq: make(chan *discoveryv3.DiscoveryRequest, 10),
		sendResp:    make(chan *discoveryv3.DiscoveryResponse, 10),
	}
}

func (s *mockADSServer) StreamAggregatedResources(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	ctx := stream.Context()
	errCh := make(chan error, 2)

	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			s.receivedReq <- req
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case resp, ok := <-s.sendResp:
				if !ok {
					return
				}
				if err := stream.Send(resp); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
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
	name string
}

func (m *mockSourcePolicy) PolicyName() string { return m.name }
func (m *mockSourcePolicy) PolicyType() string { return "mock_source" }
func (m *mockSourcePolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}
func (m *mockSourcePolicy) Validate() error { return nil }
func (m *mockSourcePolicy) Evaluate(context.Context) (*confmap.Conf, error) {
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
	return nil, nil
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

func TestRetrieve_MultipleDestinationPolicies(t *testing.T) {
	t.Setenv("FLEET_ID", "1234")
	p := createProvider()

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

	_, err := p.Retrieve(context.Background(), "googlecontrolplane:component:my-config", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMultipleDestinationPolicies)
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
	// Built-in self metrics should also be added
	assert.True(t, conf.IsSet("receivers::otlp/default_self_metrics"))

	assert.NoError(t, p.Shutdown(context.Background()))
}
