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

package varnishreceiver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/varnishreceiver/internal/metadata"
)

func TestType(t *testing.T) {
	factory := NewFactory()
	require.EqualValues(t, metadata.Type, factory.Type())
}

func TestValidConfig(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		factory := NewFactory()
		cfg := factory.CreateDefaultConfig().(*Config)
		require.NoError(t, cfg.Validate())
	})
	t.Run("config with fields", func(t *testing.T) {
		testDir := t.TempDir()
		factory := NewFactory()
		cfg := factory.CreateDefaultConfig().(*Config)
		cfg.CacheDir = testDir
		cfg.ExecDir = testDir
		require.NoError(t, cfg.Validate())
	})
}

func TestCreateMetricsReceiver(t *testing.T) {
	testCases := []struct {
		desc string
		run  func(t *testing.T)
	}{
		{
			desc: "Default config",
			run: func(t *testing.T) {
				t.Parallel()

				_, err := createMetricsReceiver(
					context.Background(),
					receivertest.NewNopSettings(metadata.Type),
					createDefaultConfig(),
					consumertest.NewNop(),
				)

				require.NoError(t, err)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.desc, testCase.run)
	}
}
