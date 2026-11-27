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

func TestUdpLogSourcePolicy_Serialization(t *testing.T) {
	tests := []struct {
		name   string
		policy *policyv1.UdpLogSourcePolicy
	}{
		{
			name: "ipv4 loopback listener",
			policy: &policyv1.UdpLogSourcePolicy{
				Id:            "udp-syslog-local",
				ListenAddress: "127.0.0.1:1514",
			},
		},
		{
			name: "ipv6 wildcard listener",
			policy: &policyv1.UdpLogSourcePolicy{
				Id:            "udp-appliance-all",
				ListenAddress: "[::]:1514",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anyPolicy, err := anypb.New(tt.policy)
			require.NoError(t, err)
			assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1.UdpLogSourcePolicy", anyPolicy.GetTypeUrl())

			unpacked, err := anyPolicy.UnmarshalNew()
			require.NoError(t, err)
			assert.True(t, proto.Equal(tt.policy, unpacked))

			data, err := proto.Marshal(tt.policy)
			require.NoError(t, err)

			unmarshaled := &policyv1.UdpLogSourcePolicy{}
			require.NoError(t, proto.Unmarshal(data, unmarshaled))
			assert.True(t, proto.Equal(tt.policy, unmarshaled))
		})
	}
}

func TestUdpLogSourcePolicy_JSONSerialization(t *testing.T) {
	udpPolicy := &policyv1.UdpLogSourcePolicy{
		Id:            "appliance-udp",
		ListenAddress: "0.0.0.0:1514",
	}

	jsonData, err := protojson.Marshal(udpPolicy)
	require.NoError(t, err)

	unmarshaled := &policyv1.UdpLogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(udpPolicy, unmarshaled))
	assert.Equal(t, "0.0.0.0:1514", unmarshaled.GetListenAddress())

	rawSnakeJSON := []byte(`{"id": "local-udp", "listen_address": "127.0.0.1:5514"}`)
	fromSnake := &policyv1.UdpLogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(rawSnakeJSON, fromSnake))
	assert.Equal(t, "local-udp", fromSnake.GetId())
	assert.Equal(t, "127.0.0.1:5514", fromSnake.GetListenAddress())

	emptyPolicy := &policyv1.UdpLogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal([]byte(`{}`), emptyPolicy))
	assert.Empty(t, emptyPolicy.GetId())
	assert.Empty(t, emptyPolicy.GetListenAddress())
}
