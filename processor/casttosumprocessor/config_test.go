// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package casttosumprocessor

import (
	"fmt"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/service/servicetest"
)

func TestLoadingFullConfig(t *testing.T) {
	factories, err := componenttest.NopFactories()
	assert.NoError(t, err)

	factory := NewFactory()
	factories.Processors[typeStr] = factory

	cfg, err := servicetest.LoadConfigAndValidate(path.Join(".", "testdata", "config_full.yaml"), factories)
	assert.NoError(t, err)
	assert.NotNil(t, cfg)

	id := config.NewComponentID(typeStr)
	settings := config.NewProcessorSettings(id)
	p1 := cfg.Processors[id]
	expectedCfg := &Config{
		ProcessorSettings: &settings,
		Metrics: []string{
			"metric1",
			"metric2",
		},
	}
	assert.Equal(t, p1, expectedCfg)
}

func TestValidateConfig(t *testing.T) {
	factories, err := componenttest.NopFactories()
	assert.NoError(t, err)

	factory := NewFactory()
	factories.Processors[typeStr] = factory

	_, err = servicetest.LoadConfigAndValidate(path.Join(".", "testdata", "config_missing_name.yaml"), factories)
	assert.EqualError(t, err, fmt.Sprintf("processor %q has invalid configuration: %s", typeStr, "metric names are missing"))
}
