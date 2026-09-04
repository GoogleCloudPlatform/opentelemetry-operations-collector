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
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"go.opentelemetry.io/collector/confmap"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/encoding/protojson"

	xdsv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/xds/v1alpha1"
)

var _ policyManager = (*xdsPolicyManager)(nil)

// xdsPolicyManager connects to an xDS control plane, prints received TelemetryCollector objects, and ACKs them.
type xdsPolicyManager struct {
	logger  *zap.Logger
	uri     *url.URL
	watcher confmap.WatcherFunc

	cancel context.CancelFunc
	done   chan struct{}
}

// NewXDSPolicyManager creates a new xdsPolicyManager.
func NewXDSPolicyManager(logger *zap.Logger, uri *url.URL, watcher confmap.WatcherFunc) (*xdsPolicyManager, error) {
	if uri.Host == "" && uri.Path == "" {
		return nil, errors.New("xDS server address cannot be empty in URI")
	}
	return &xdsPolicyManager{
		logger:  logger,
		uri:     uri,
		watcher: watcher,
	}, nil
}

// Start connects to the xDS server, sends the initial DiscoveryRequest, and monitors in background.
// If connecting or opening the stream fails, it returns an error immediately.
func (m *xdsPolicyManager) Start() error {
	serverAddr := m.uri.Host
	if serverAddr == "" {
		serverAddr = strings.TrimPrefix(m.uri.Path, "/")
	}
	fleetID := m.uri.Query().Get("fleet")
	if fleetID == "" {
		fleetID = defaultFleetID
	}

	ctx, cancel := context.WithCancel(context.Background())

	conn, err := m.dial(ctx, serverAddr)
	if err != nil {
		cancel()
		return fmt.Errorf("failed to dial xDS server %s: %w", serverAddr, err)
	}

	client := discoveryv3.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := client.StreamAggregatedResources(ctx)
	if err != nil {
		conn.Close()
		cancel()
		return fmt.Errorf("failed to open xDS stream to %s: %w", serverAddr, err)
	}

	node := &corev3.Node{
		Id:      "edge-collector-01",
		Cluster: fleetID,
		Locality: &corev3.Locality{
			Region: "asia-east1",
		},
	}

	typeURL := m.uri.Query().Get("type_url")
	if typeURL == "" {
		typeURL = defaultXdsTypeURL
	}

	// Send initial DiscoveryRequest
	if err := stream.Send(&discoveryv3.DiscoveryRequest{
		Node:    node,
		TypeUrl: typeURL,
	}); err != nil {
		conn.Close()
		cancel()
		return fmt.Errorf("failed to send DiscoveryRequest to %s: %w", serverAddr, err)
	}

	if m.logger != nil {
		m.logger.Info("Connected to xDS server and sent DiscoveryRequest",
			zap.String("server", serverAddr),
			zap.String("fleet", fleetID),
			zap.String("type_url", typeURL),
		)
	}

	m.cancel = cancel
	m.done = make(chan struct{})

	go m.listen(ctx, conn, stream, node, typeURL)
	return nil
}

// Stop terminates the background monitoring loop and waits for it to exit.
func (m *xdsPolicyManager) Stop() error {
	if m.cancel != nil {
		m.cancel()
		<-m.done
	}
	return nil
}

// URI returns the configured URI.
func (m *xdsPolicyManager) URI() *url.URL {
	return m.uri
}

// PolicyEvaluationResult satisfies the policyManager interface.
func (m *xdsPolicyManager) PolicyEvaluationResult(string, error) {}

// listen receives DiscoveryResponses, prints TelemetryCollector objects, and sends ACKs.
func (m *xdsPolicyManager) listen(ctx context.Context, conn *grpc.ClientConn, stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesClient, node *corev3.Node, typeURL string) {
	defer close(m.done)
	defer conn.Close()

	jsonFormatter := protojson.MarshalOptions{Multiline: true, Indent: "  ", UseProtoNames: true}

	for {
		resp, err := stream.Recv()
		if err != nil {
			if m.logger != nil {
				m.logger.Info("xDS stream closed", zap.Error(err))
			}
			return
		}

		if m.logger != nil {
			m.logger.Info("Received xDS DiscoveryResponse",
				zap.String("version", resp.GetVersionInfo()),
				zap.String("nonce", resp.GetNonce()),
				zap.Int("resources", len(resp.GetResources())),
			)
		}

		// Print each TelemetryCollector resource
		for i, anyRes := range resp.GetResources() {
			collector := &xdsv1alpha1.TelemetryCollector{}
			if err := anyRes.UnmarshalTo(collector); err == nil {
				formatted, _ := jsonFormatter.Marshal(collector)
				if m.logger != nil {
					m.logger.Info("xDS TelemetryCollector",
						zap.Int("resource_index", i+1),
						zap.String("version", resp.GetVersionInfo()),
						zap.String("content", string(formatted)),
					)
				} else {
					fmt.Printf("[xDS] TelemetryCollector #%d (v%s):\n%s\n", i+1, resp.GetVersionInfo(), string(formatted))
				}
			} else {
				formatted, _ := jsonFormatter.Marshal(anyRes)
				if m.logger != nil {
					m.logger.Info("xDS Resource",
						zap.Int("resource_index", i+1),
						zap.String("type_url", anyRes.GetTypeUrl()),
						zap.String("version", resp.GetVersionInfo()),
						zap.String("content", string(formatted)),
					)
				} else {
					fmt.Printf("[xDS] Resource #%d (type %s, v%s):\n%s\n", i+1, anyRes.GetTypeUrl(), resp.GetVersionInfo(), string(formatted))
				}
			}
		}

		// For now, keep the stream persistently alive to monitor and ACK messages without
		// triggering pipeline reloads until policy-to-config translation is wired up.
		// if m.watcher != nil {
		// 	m.watcher(&confmap.ChangeEvent{})
		// }

		// Send ACK on the same stream
		ackTypeURL := resp.GetTypeUrl()
		if ackTypeURL == "" {
			ackTypeURL = typeURL
		}
		_ = stream.Send(&discoveryv3.DiscoveryRequest{
			Node:          node,
			TypeUrl:       ackTypeURL,
			VersionInfo:   resp.GetVersionInfo(),
			ResponseNonce: resp.GetNonce(),
		})
	}
}

func (m *xdsPolicyManager) dial(ctx context.Context, serverAddr string) (*grpc.ClientConn, error) {
	tokenSource, err := resolveTokenSource(ctx, serverAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve credentials: %w", err)
	}

	tlsCreds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: false})
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
	}
	if tokenSource != nil {
		dialOpts = append(dialOpts, grpc.WithPerRPCCredentials(tokenAuth{ts: tokenSource}))
	}

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return grpc.DialContext(dialCtx, serverAddr, dialOpts...)
}

// resolveTokenSource returns a TokenSource providing Google OIDC ID tokens.
// It supports:
//  1. GCE / GKE / Service Accounts: Uses idtoken.NewTokenSource (queries VM metadata server or SA key).
//  2. Cloudtop / Developer Workstations: idtoken fails on "authorized_user" credentials, so it
//     falls back to ADC with OpenID scopes and extracts the ID token via googleIDTokenSource.
func resolveTokenSource(ctx context.Context, serverAddr string) (oauth2.TokenSource, error) {
	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		host = serverAddr
	}
	audience := "https://" + host

	// 1. GCE / GKE / Service Accounts: metadata server or SA key
	if idTS, err := idtoken.NewTokenSource(ctx, audience); err == nil {
		return idTS, nil
	}

	// 2. Cloudtop / Developer Workstation: ADC user credentials fallback
	defTS, err := google.DefaultTokenSource(ctx, "openid", "email")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ADC: %w", err)
	}

	wrapped := &googleIDTokenSource{src: defTS}
	initialTok, err := wrapped.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve initial ID token: %w", err)
	}
	return oauth2.ReuseTokenSource(initialTok, wrapped), nil
}

// googleIDTokenSource extracts the OIDC ID token from OAuth2 credentials
// (where it is stored in tok.Extra("id_token")) so tok.AccessToken contains the ID token.
type googleIDTokenSource struct {
	src oauth2.TokenSource
}

func (s *googleIDTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	idTok, ok := tok.Extra("id_token").(string)
	if !ok || idTok == "" {
		return nil, errors.New("no id_token found in ADC credentials; please run 'gcloud auth application-default login'")
	}
	return &oauth2.Token{
		AccessToken: idTok,
		TokenType:   "Bearer",
		Expiry:      tok.Expiry,
	}, nil
}

// tokenAuth adapts an oauth2.TokenSource to gRPC's credentials.PerRPCCredentials interface,
// injecting the Authorization: Bearer <id_token> header into each outgoing gRPC request.
type tokenAuth struct {
	ts oauth2.TokenSource
}

func (a tokenAuth) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	tok, err := a.ts.Token()
	if err != nil {
		return nil, err
	}
	return map[string]string{"authorization": "Bearer " + tok.AccessToken}, nil
}

// RequireTransportSecurity satisfies the credentials.PerRPCCredentials interface.
func (a tokenAuth) RequireTransportSecurity() bool {
	return false
}

