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

package gcpdestination

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
)

func TestGCPDestinationPolicy_Evaluate_WithExplicitProjectID(t *testing.T) {
	p := &GCPDestinationPolicy{
		Name:      "test_dest",
		ProjectID: "my-custom-project",
	}

	conf, err := p.Evaluate(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conf)

	// Check authenticator project
	assert.Equal(t, "my-custom-project", conf.Get("extensions::googleclientauth/test_dest::project"))

	// Check resource processor for gcp.project_id
	procKey := "processors::resource/test_dest_gcp_project_id::attributes"
	assert.True(t, conf.IsSet(procKey))

	attrs := conf.Get(procKey).([]any)
	require.NotEmpty(t, attrs)
	firstAction := attrs[0].(map[string]any)
	assert.Equal(t, "insert", firstAction["action"])
	assert.Equal(t, "gcp.project_id", firstAction["key"])
	assert.Equal(t, "my-custom-project", firstAction["value"])

	// Check pipeline preprocessor ordering
	resourceType, _ := component.NewType("resource")
	resourceProjectID := component.NewIDWithName(resourceType, "test_dest_gcp_project_id")
	assert.Contains(t, p.preprocessorMetricIDs, resourceProjectID)
	assert.Contains(t, p.preprocessorLogIDs, resourceProjectID)
	assert.Contains(t, p.preprocessorTraceIDs, resourceProjectID)
}

func TestGCPDestinationPolicy_Evaluate_FallbackToMetadataProjectID(t *testing.T) {
	p := &GCPDestinationPolicy{
		Name: "test_dest",
	}

	conf, err := p.Evaluate(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conf)

	procKey := "processors::resource/test_dest_gcp_project_id::attributes"
	assert.True(t, conf.IsSet(procKey))

	attrs := conf.Get(procKey).([]any)
	require.Len(t, attrs, 2)
	firstAction := attrs[0].(map[string]any)
	assert.Equal(t, "insert", firstAction["action"])
	assert.Equal(t, "gcp.project_id", firstAction["key"])
	assert.Equal(t, "cloud.account.id", firstAction["from_attribute"])
}

func TestGCPDestinationPolicy_Validate(t *testing.T) {
	pNoName := &GCPDestinationPolicy{}
	assert.Error(t, pNoName.Validate())

	pValid := &GCPDestinationPolicy{Name: "valid_destination"}
	assert.NoError(t, pValid.Validate())
}
