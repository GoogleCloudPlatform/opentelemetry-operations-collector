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

func TestWindowsEventLogSourcePolicy_Serialization(t *testing.T) {
	policy := &policyv1alpha1.WindowsEventLogSourcePolicy{
		Id: "windows-vm-standard-logs",
		Channels: []string{
			"Security",
			"System",
			"Application",
			"Directory Service",
			"Microsoft-Windows-Sysmon/Operational",
		},
	}

	// Verify Any packaging.
	anyPolicy, err := anypb.New(policy)
	require.NoError(t, err)
	assert.Equal(t, "type.googleapis.com/google.telemetry.policy.v1alpha1.WindowsEventLogSourcePolicy", anyPolicy.GetTypeUrl())

	unpacked, err := anyPolicy.UnmarshalNew()
	require.NoError(t, err)
	assert.True(t, proto.Equal(policy, unpacked))

	// Round-trip binary wire serialization.
	data, err := proto.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.WindowsEventLogSourcePolicy{}
	require.NoError(t, proto.Unmarshal(data, unmarshaled))

	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "windows-vm-standard-logs", unmarshaled.GetId())
	assert.Equal(t, []string{
		"Security",
		"System",
		"Application",
		"Directory Service",
		"Microsoft-Windows-Sysmon/Operational",
	}, unmarshaled.GetChannels())
}

func TestWindowsEventLogSourcePolicy_JSONSerialization(t *testing.T) {
	policy := &policyv1alpha1.WindowsEventLogSourcePolicy{
		Id: "active-directory-logs",
		Channels: []string{
			"Security",
			"System",
			"Directory Service",
		},
	}

	// ProtoJSON marshal and unmarshal round-trip.
	jsonData, err := protojson.Marshal(policy)
	require.NoError(t, err)

	unmarshaled := &policyv1alpha1.WindowsEventLogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(jsonData, unmarshaled))
	assert.True(t, proto.Equal(policy, unmarshaled))
	assert.Equal(t, "active-directory-logs", unmarshaled.GetId())
	assert.Equal(t, []string{"Security", "System", "Directory Service"}, unmarshaled.GetChannels())

	// Unmarshal from explicit JSON literal.
	rawJSON := []byte(`{"id": "dc-security-logs", "channels": ["Security"]}`)
	fromRaw := &policyv1alpha1.WindowsEventLogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal(rawJSON, fromRaw))
	assert.Equal(t, "dc-security-logs", fromRaw.GetId())
	assert.Equal(t, []string{"Security"}, fromRaw.GetChannels())

	// Empty JSON object unmarshals to default zero values.
	emptyPolicy := &policyv1alpha1.WindowsEventLogSourcePolicy{}
	require.NoError(t, protojson.Unmarshal([]byte(`{}`), emptyPolicy))
	assert.Empty(t, emptyPolicy.GetId())
	assert.Empty(t, emptyPolicy.GetChannels())

	// EmitUnpopulated includes default fields in JSON output.
	marshaledUnpopulated, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(emptyPolicy)
	require.NoError(t, err)
	assert.Contains(t, string(marshaledUnpopulated), `"id":""`)
	assert.Contains(t, string(marshaledUnpopulated), `"channels":[]`)
}
