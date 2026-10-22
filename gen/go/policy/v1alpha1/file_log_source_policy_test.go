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
		Id: "tail-system-and-iis-logs",
		IncludedPaths: []string{
			"/var/log/syslog",
			"/var/log/messages",
			`C:\inetpub\logs\LogFiles\W3SVC*\u_ex*.log`,
		},
	}

	// Verify Any packaging.
	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.FileLogSourcePolicy", anyPolicy.GetTypeUrl())

	unpacked := &policyv1alpha1.FileLogSourcePolicy{}
	require.NoError(t, anyPolicy.UnmarshalTo(unpacked))
	assert.True(t, proto.Equal(policy, unpacked))

	// Round-trip binary serialization.
	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.FileLogSourcePolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "tail-system-and-iis-logs", unmarshaled.GetId())
	assert.Equal(t, []string{
		"/var/log/syslog",
		"/var/log/messages",
		`C:\inetpub\logs\LogFiles\W3SVC*\u_ex*.log`,
	}, unmarshaled.GetIncludedPaths())
}

func TestFileLogSourcePolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.FileLogSourcePolicy{
		Id: "tail-linux-logs",
		IncludedPaths: []string{
			"/var/log/syslog",
			"/opt/app/logs/*.log",
		},
	}

	// ProtoJSON marshal and unmarshal round-trip.
	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.FileLogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "tail-linux-logs", unmarshaled.GetId())
	assert.Equal(t, []string{"/var/log/syslog", "/opt/app/logs/*.log"}, unmarshaled.GetIncludedPaths())

	// Unmarshal from explicit JSON payload.
	rawJSON := []byte(`{"id": "iis-access-logs", "included_paths": ["C:\\inetpub\\logs\\LogFiles\\W3SVC*\\u_ex*.log"]}`)
	fromRaw := &policyv1alpha1.FileLogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(rawJSON, fromRaw))
	assert.Equal(t, "iis-access-logs", fromRaw.GetId())
	assert.Equal(t, []string{`C:\inetpub\logs\LogFiles\W3SVC*\u_ex*.log`}, fromRaw.GetIncludedPaths())

	// Omitted included_paths in JSON defaults to empty/nil slice.
	omittedJSON := []byte(`{"id": "empty-include-policy"}`)
	omittedPolicy := &policyv1alpha1.FileLogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(omittedJSON, omittedPolicy))
	assert.Equal(t, "empty-include-policy", omittedPolicy.GetId())
	assert.Empty(t, omittedPolicy.GetIncludedPaths())

	// EmitUnpopulated includes empty "includedPaths":[] array in output.
	marshaledUnpopulated, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(omittedPolicy)
	require.NoError(t, err)
	assert.Contains(t, string(marshaledUnpopulated), `"includedPaths":[]`)
}
