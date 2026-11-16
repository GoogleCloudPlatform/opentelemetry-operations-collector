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

package googlepolicy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSelfResourceDetectionProcessorConfig(t *testing.T) {
	cfg := SelfResourceDetectionProcessorConfig()
	assert.Equal(t, map[string]any{
		"detectors": []string{"env", "gcp", "system"},
		"override":  false,
		"timeout":   10 * time.Second,
		"env": map[string]any{
			"attributes": map[string]any{
				"included": []string{
					"cloud.*",
					"faas.*",
					"host.*",
					"k8s.*",
					"location",
					"os.*",
					"service.*",
				},
			},
		},
		"system": map[string]any{
			"resource_attributes": map[string]any{
				"host.id": map[string]any{
					"enabled": true,
				},
				"os.type": map[string]any{
					"enabled": false,
				},
			},
		},
	}, cfg)

	cfg["override"] = true
	assert.Equal(t, false, SelfResourceDetectionProcessorConfig()["override"])
}

func TestWorkloadResourceDetectionProcessorConfig(t *testing.T) {
	cfg := WorkloadResourceDetectionProcessorConfig()
	assert.Equal(t, map[string]any{
		"detectors": []string{"env", "gcp", "system"},
		"override":  false,
		"timeout":   10 * time.Second,
		"env": map[string]any{
			"attributes": map[string]any{
				"excluded": []string{
					"service.*",
					"k8s.pod.*",
					"k8s.container.*",
					"k8s.namespace.*",
					"k8s.daemonset.*",
					"k8s.deployment.*",
					"k8s.replicaset.*",
					"k8s.statefulset.*",
					"k8s.job.*",
					"k8s.cronjob.*",
				},
			},
		},
		"system": map[string]any{
			"resource_attributes": map[string]any{
				"host.id": map[string]any{
					"enabled": true,
				},
				"os.type": map[string]any{
					"enabled": false,
				},
			},
		},
	}, cfg)

	cfg["override"] = true
	assert.Equal(t, false, WorkloadResourceDetectionProcessorConfig()["override"])
}

func TestCloudRegionFallbackStatement(t *testing.T) {
	assert.Empty(t, CloudRegionFallbackStatement(""))
	assert.Equal(t,
		`set(attributes["cloud.region"], "us-east1") where attributes["cloud.region"] == nil and attributes["cloud.availability_zone"] == nil and attributes["location"] == nil`,
		CloudRegionFallbackStatement("us-east1"))
	// Quotes are escaped so the value stays a single OTTL string literal.
	assert.Contains(t, CloudRegionFallbackStatement(`a"b`), `"a\"b"`)
}
