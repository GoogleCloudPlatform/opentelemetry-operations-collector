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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/extension/extensiontest"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/extension/googlecontrolplaneextension/internal/metadata"
)

func TestNewFactory(t *testing.T) {
	f := NewFactory()
	assert.Equal(t, metadata.Type, f.Type())
	assert.Equal(t, &Config{}, f.CreateDefaultConfig())
	assert.Equal(t, metadata.ExtensionStability, f.Stability())
}

func TestCreateExtension(t *testing.T) {
	f := NewFactory()
	ext, err := f.Create(context.Background(), extensiontest.NewNopSettings(metadata.Type), f.CreateDefaultConfig())
	require.NoError(t, err)
	require.NotNil(t, ext)

	// Lifecycle against the nop host must be clean.
	require.NoError(t, ext.Start(context.Background(), componenttest.NewNopHost()))
	assert.NoError(t, ext.Shutdown(context.Background()))
}

func TestCreateExtension_UsesRegistrySource(t *testing.T) {
	f := NewFactory()
	ext, err := f.Create(context.Background(), extensiontest.NewNopSettings(metadata.Type), f.CreateDefaultConfig())
	require.NoError(t, err)

	cpExt, ok := ext.(*controlPlaneExtension)
	require.True(t, ok)
	assert.IsType(t, registrySource{}, cpExt.source)
}
