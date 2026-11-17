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

package policyv1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	policyv1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1"
)

func TestSyslogSourcePolicy_Serialization(t *testing.T) {
	tests := []struct {
		name   string
		policy *policyv1.SyslogSourcePolicy
	}{
		{
			name: "tcp endpoint",
			policy: &policyv1.SyslogSourcePolicy{
				Id: "syslog-tcp-local",
				Transport: &policyv1.SyslogSourcePolicy_Tcp{
					Tcp: &policyv1.SyslogSourcePolicy_TcpEndpoint{
						ListenAddress: "127.0.0.1:1514",
					},
				},
			},
		},
		{
			name: "udp endpoint",
			policy: &policyv1.SyslogSourcePolicy{
				Id: "syslog-udp-all",
				Transport: &policyv1.SyslogSourcePolicy_Udp{
					Udp: &policyv1.SyslogSourcePolicy_UdpEndpoint{
						ListenAddress: "[::]:1514",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anyPolicy, err := anypb.New(tt.policy)
			require.NoError(t, err)
			assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1.SyslogSourcePolicy", anyPolicy.GetTypeUrl())

			unpacked, err := anyPolicy.UnmarshalNew()
			require.NoError(t, err)
			assert.True(t, proto.Equal(tt.policy, unpacked))

			data, err := proto.Marshal(tt.policy)
			require.NoError(t, err)

			unmarshaled := &policyv1.SyslogSourcePolicy{}
			require.NoError(t, proto.Unmarshal(data, unmarshaled))
			assert.True(t, proto.Equal(tt.policy, unmarshaled))
		})
	}
}

func TestSyslogSourcePolicy_JSONSerialization(t *testing.T) {
	tcpPolicy := &policyv1.SyslogSourcePolicy{
		Id: "appliance-syslog-tcp",
		Transport: &policyv1.SyslogSourcePolicy_Tcp{
			Tcp: &policyv1.SyslogSourcePolicy_TcpEndpoint{
				ListenAddress: "0.0.0.0:1514",
			},
		},
	}

	jsonData, err := protojson.Marshal(tcpPolicy)
	require.NoError(t, err)

	unmarshaled := &policyv1.SyslogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(tcpPolicy, unmarshaled))
	assert.Equal(t, "0.0.0.0:1514", unmarshaled.GetTcp().GetListenAddress())
	assert.Nil(t, unmarshaled.GetUdp())

	rawSnakeJSON := []byte(`{"id": "local-udp", "udp": {"listen_address": "127.0.0.1:5514"}}`)
	fromSnake := &policyv1.SyslogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(rawSnakeJSON, fromSnake))
	assert.Equal(t, "local-udp", fromSnake.GetId())
	assert.Equal(t, "127.0.0.1:5514", fromSnake.GetUdp().GetListenAddress())
	assert.Nil(t, fromSnake.GetTcp())

	emptyPolicy := &policyv1.SyslogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal([]byte(`{}`), emptyPolicy))
	assert.Empty(t, emptyPolicy.GetId())
	assert.Nil(t, emptyPolicy.GetTransport())
}
