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

package googlepolicyprocessor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/processor/processortest"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/processor/googlepolicyprocessor/internal/metadata"
)

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	assert.Equal(t, metadata.Type, factory.Type())

	cfg := factory.CreateDefaultConfig()
	assert.Equal(t, &Config{}, cfg)

	set := processortest.NewNopSettings(metadata.Type)

	tp, err := factory.CreateTraces(context.Background(), set, cfg, consumertest.NewNop())
	assert.NoError(t, err)
	assert.NotNil(t, tp)

	mp, err := factory.CreateMetrics(context.Background(), set, cfg, consumertest.NewNop())
	assert.NoError(t, err)
	assert.NotNil(t, mp)

	lp, err := factory.CreateLogs(context.Background(), set, cfg, consumertest.NewNop())
	assert.NoError(t, err)
	assert.NotNil(t, lp)
}
