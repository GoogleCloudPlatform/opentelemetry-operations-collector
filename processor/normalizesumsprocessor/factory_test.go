// Copyright 2021 Google LLC
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

package normalizesumsprocessor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configtest"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.uber.org/multierr"
)

func TestCreateDefaultConfig(t *testing.T) {
	m, err := component.MakeProcessorFactoryMap(NewFactory())
	assert.NoError(t, err)
	assert.NoError(t, validateConfigFromFactories(component.Factories{
		Processors: m,
	}))
}

func validateConfigFromFactories(factories component.Factories) error {
	var errs error

	for _, factory := range factories.Receivers {
		errs = multierr.Append(errs, configtest.CheckConfigStruct(factory.CreateDefaultConfig()))
	}
	for _, factory := range factories.Processors {
		errs = multierr.Append(errs, configtest.CheckConfigStruct(factory.CreateDefaultConfig()))
	}
	for _, factory := range factories.Exporters {
		errs = multierr.Append(errs, configtest.CheckConfigStruct(factory.CreateDefaultConfig()))
	}
	for _, factory := range factories.Extensions {
		errs = multierr.Append(errs, configtest.CheckConfigStruct(factory.CreateDefaultConfig()))
	}

	return errs
}

func TestCreateProcessor(t *testing.T) {
	mp, err := createMetricsProcessor(context.Background(), component.ProcessorCreateSettings{}, createDefaultConfig(), consumertest.NewNop())
	assert.NoError(t, err)
	assert.NotNil(t, mp)
}
