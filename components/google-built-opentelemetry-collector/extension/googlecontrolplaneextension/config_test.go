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

package googlecontrolplaneextension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/confmap"
)

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := createDefaultConfig()
	assert.NoError(t, componenttest.CheckConfigStruct(cfg))
	require.NoError(t, cfg.(*Config).Validate())
}

// The extension takes no configuration. An empty section must unmarshal
// cleanly, and anything an operator writes under it must be rejected rather
// than silently ignored.
func TestConfigRejectsUnknownKeys(t *testing.T) {
	cfg := createDefaultConfig()
	require.NoError(t, confmap.New().Unmarshal(cfg))
	assert.Equal(t, &Config{}, cfg)

	// policy_set_id in particular was briefly a real field. Failing loudly
	// keeps a stale config from looking like it still works while the label is
	// actually being derived from the policies.
	err := confmap.NewFromStringMap(map[string]any{
		"policy_set_id": "projects/p/locations/l/policySets/ps",
	}).Unmarshal(cfg)
	assert.Error(t, err)
}
