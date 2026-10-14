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

package policyv1alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
)

func TestFileLogSourcePolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.FileLogSourcePolicy{
		Id: "tail-var-log",
		Include: []string{
			"/var/log/**/*.log",
			"/var/log/syslog",
		},
	}

	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.FileLogSourcePolicy{}
	err = proto.Unmarshal(data, unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, policy.GetId(), unmarshaled.GetId())
	assert.Equal(t, policy.GetInclude(), unmarshaled.GetInclude())
	assert.True(t, proto.Equal(policy, unmarshaled))
}

func TestFileLogSourcePolicy_AnyPackaging(t *testing.T) {
	policy := &policyv1alpha1.FileLogSourcePolicy{
		Id: "iis-logs",
		Include: []string{
			`%SystemDrive%\inetpub\logs\**`,
		},
	}

	anyMsg, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.FileLogSourcePolicy", anyMsg.GetTypeUrl())

	unpacked, err := anyMsg.UnmarshalNew()
	require.NoError(t, err)

	unpackedPolicy, ok := unpacked.(*policyv1alpha1.FileLogSourcePolicy)
	require.True(t, ok)
	assert.True(t, proto.Equal(policy, unpackedPolicy))
}

func TestFileLogSourcePolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.FileLogSourcePolicy{
		Id: "app-logs",
		Include: []string{
			"/opt/my-app/logs/*.log",
		},
	}

	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.FileLogSourcePolicy{}
	err = protojson.Unmarshal(jsonData, unmarshaled)
	require.NoError(t, err)

	assert.True(t, proto.Equal(policy, unmarshaled))
}
