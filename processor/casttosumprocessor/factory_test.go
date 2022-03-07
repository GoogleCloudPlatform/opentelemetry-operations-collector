// Copyright 2022 Google LLC
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

package casttosumprocessor

import (
	"context"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/config/configtest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/service/servicetest"
)

func TestCreateDefaultConfig(t *testing.T) {
	assert.NoError(t, configtest.CheckConfigStruct(NewFactory().CreateDefaultConfig()))
}
func TestCreateProcessor(t *testing.T) {
	factories, err := componenttest.NopFactories()
	assert.NoError(t, err)

	factory := NewFactory()
	factories.Processors[typeStr] = factory

	config, err := servicetest.LoadConfigAndValidate(path.Join(".", "testdata", "config_full.yaml"), factories)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	for _, cfg := range config.Processors {
		mp, err := createMetricsProcessor(context.Background(), component.ProcessorCreateSettings{}, cfg, consumertest.NewNop())
		assert.NoError(t, err)
		assert.NotNil(t, mp)
	}
}
