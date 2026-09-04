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

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
)

func TestValue_Variants(t *testing.T) {
	testCases := []struct {
		name     string
		val      *policyv1alpha1.Value
		validate func(t *testing.T, v *policyv1alpha1.Value)
	}{
		{
			name: "string_value",
			val: &policyv1alpha1.Value{
				Value: &policyv1alpha1.Value_StringValue{
					StringValue: "production",
				},
			},
			validate: func(t *testing.T, v *policyv1alpha1.Value) {
				assert.Equal(t, "production", v.GetStringValue())
				assert.IsType(t, &policyv1alpha1.Value_StringValue{}, v.GetValue())
			},
		},
		{
			name: "int_value",
			val: &policyv1alpha1.Value{
				Value: &policyv1alpha1.Value_IntValue{
					IntValue: 200,
				},
			},
			validate: func(t *testing.T, v *policyv1alpha1.Value) {
				assert.Equal(t, int64(200), v.GetIntValue())
				assert.IsType(t, &policyv1alpha1.Value_IntValue{}, v.GetValue())
			},
		},
		{
			name: "bool_value",
			val: &policyv1alpha1.Value{
				Value: &policyv1alpha1.Value_BoolValue{
					BoolValue: true,
				},
			},
			validate: func(t *testing.T, v *policyv1alpha1.Value) {
				assert.True(t, v.GetBoolValue())
				assert.IsType(t, &policyv1alpha1.Value_BoolValue{}, v.GetValue())
			},
		},
		{
			name: "double_value",
			val: &policyv1alpha1.Value{
				Value: &policyv1alpha1.Value_DoubleValue{
					DoubleValue: 3.14159,
				},
			},
			validate: func(t *testing.T, v *policyv1alpha1.Value) {
				assert.InDelta(t, 3.14159, v.GetDoubleValue(), 0.00001)
				assert.IsType(t, &policyv1alpha1.Value_DoubleValue{}, v.GetValue())
			},
		},
		{
			name: "bytes_value",
			val: &policyv1alpha1.Value{
				Value: &policyv1alpha1.Value_BytesValue{
					BytesValue: []byte{0x01, 0x02, 0x03, 0x04},
				},
			},
			validate: func(t *testing.T, v *policyv1alpha1.Value) {
				assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, v.GetBytesValue())
				assert.IsType(t, &policyv1alpha1.Value_BytesValue{}, v.GetValue())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Round-trip binary proto.
			data, err := proto.Marshal(tc.val)
			require.NoError(t, err)

			unmarshaled := &policyv1alpha1.Value{}
			require.NoError(t, proto.Unmarshal(data, unmarshaled))
			assert.True(t, proto.Equal(tc.val, unmarshaled))
			tc.validate(t, unmarshaled)

			// Round-trip JSON proto.
			jsonData, err := protojson.Marshal(tc.val)
			require.NoError(t, err)

			jsonUnmarshaled := &policyv1alpha1.Value{}
			require.NoError(t, protojson.Unmarshal(jsonData, jsonUnmarshaled))
			assert.True(t, proto.Equal(tc.val, jsonUnmarshaled))
			tc.validate(t, jsonUnmarshaled)
		})
	}
}

func TestNumericValue_Variants(t *testing.T) {
	testCases := []struct {
		name     string
		num      *policyv1alpha1.NumericValue
		validate func(t *testing.T, n *policyv1alpha1.NumericValue)
	}{
		{
			name: "int_value",
			num: &policyv1alpha1.NumericValue{
				Value: &policyv1alpha1.NumericValue_IntValue{
					IntValue: 404,
				},
			},
			validate: func(t *testing.T, n *policyv1alpha1.NumericValue) {
				assert.Equal(t, int64(404), n.GetIntValue())
				assert.IsType(t, &policyv1alpha1.NumericValue_IntValue{}, n.GetValue())
			},
		},
		{
			name: "double_value",
			num: &policyv1alpha1.NumericValue{
				Value: &policyv1alpha1.NumericValue_DoubleValue{
					DoubleValue: 99.99,
				},
			},
			validate: func(t *testing.T, n *policyv1alpha1.NumericValue) {
				assert.InDelta(t, 99.99, n.GetDoubleValue(), 0.001)
				assert.IsType(t, &policyv1alpha1.NumericValue_DoubleValue{}, n.GetValue())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := proto.Marshal(tc.num)
			require.NoError(t, err)

			unmarshaled := &policyv1alpha1.NumericValue{}
			require.NoError(t, proto.Unmarshal(data, unmarshaled))
			assert.True(t, proto.Equal(tc.num, unmarshaled))
			tc.validate(t, unmarshaled)

			jsonData, err := protojson.Marshal(tc.num)
			require.NoError(t, err)

			jsonUnmarshaled := &policyv1alpha1.NumericValue{}
			require.NoError(t, protojson.Unmarshal(jsonData, jsonUnmarshaled))
			assert.True(t, proto.Equal(tc.num, jsonUnmarshaled))
			tc.validate(t, jsonUnmarshaled)
		})
	}
}

func TestAttributePath_Serialization(t *testing.T) {
	testCases := []struct {
		name string
		path *policyv1alpha1.AttributePath
	}{
		{
			name: "single_segment_flat",
			path: &policyv1alpha1.AttributePath{
				Path: []string{"http.status_code"},
			},
		},
		{
			name: "multi_segment_nested",
			path: &policyv1alpha1.AttributePath{
				Path: []string{"http", "request", "headers", "authorization"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := proto.Marshal(tc.path)
			require.NoError(t, err)

			unmarshaled := &policyv1alpha1.AttributePath{}
			require.NoError(t, proto.Unmarshal(data, unmarshaled))
			assert.True(t, proto.Equal(tc.path, unmarshaled))
			assert.Equal(t, tc.path.GetPath(), unmarshaled.GetPath())

			jsonData, err := protojson.Marshal(tc.path)
			require.NoError(t, err)

			jsonUnmarshaled := &policyv1alpha1.AttributePath{}
			require.NoError(t, protojson.Unmarshal(jsonData, jsonUnmarshaled))
			assert.True(t, proto.Equal(tc.path, jsonUnmarshaled))
			assert.Equal(t, tc.path.GetPath(), jsonUnmarshaled.GetPath())
		})
	}
}
