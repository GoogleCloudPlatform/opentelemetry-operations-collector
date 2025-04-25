// Copyright 2025 Google LLC
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

package healthagent

// File inspired by https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/extension/healthcheckextension/config_test.go.
import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/otelcol/otelcoltest"
)

func TestValidConfig(t *testing.T) {
	factories, err := otelcoltest.NopFactories()
	assert.NoError(t, err)
	factory := NewFactory()
	componentType := component.MustNewType(typeStr)
	factories.Extensions[componentType] = factory
	cfg, err := otelcoltest.LoadConfigAndValidate(filepath.Join("testdata", "config.yaml"), factories)
	require.Nil(t, err)
	require.NotNil(t, cfg)
	ext1 := cfg.Extensions[component.NewID(componentType)]
	assert.Equal(t, &Config{
		Scope:              "container",
		Name:               "otel",
		Port:               "2345",
		ErrorCheckInterval: 60 * time.Second,
	}, ext1)
	ext2 := cfg.Extensions[component.NewIDWithName(componentType, "2")]
	assert.Equal(t, &Config{
		Scope:              defaultScope,
		Name:               defaultName,
		Port:               defaultPort,
		ErrorCheckInterval: 65 * time.Second,
	}, ext2)
	// Extensions which are included in `service` part of the config.yaml.
	assert.Equal(t, 1, len(cfg.Service.Extensions))
	assert.Equal(t, component.NewIDWithName(componentType, "2"), cfg.Service.Extensions[0])
}
