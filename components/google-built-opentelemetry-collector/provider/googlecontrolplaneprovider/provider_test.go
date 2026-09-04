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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
