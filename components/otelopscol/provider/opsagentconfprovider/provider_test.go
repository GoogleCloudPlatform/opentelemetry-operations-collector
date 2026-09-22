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

package opsagentconfprovider

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator/resourcedetector"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/platform"
	"github.com/shirou/gopsutil/host"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.uber.org/zap"
)

func assertPrimitiveYAMLTypes(t *testing.T, path string, v any) {
	t.Helper()
	if v == nil {
		return
	}
	switch val := v.(type) {
	case map[string]any:
		for k, elem := range val {
			assertPrimitiveYAMLTypes(t, path+"."+k, elem)
		}
	case []any:
		for _, elem := range val {
			assertPrimitiveYAMLTypes(t, path+"[]", elem)
		}
	case string, int, int64, float64, bool:
		return
	default:
		t.Fatalf("unexpected non-primitive YAML type at %s: %T (%v, kind=%v)", path, v, v, reflect.TypeOf(v).Kind())
	}
}

func TestProviderRetrieve(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(`
logging:
  receivers:
    syslog:
      type: files
      include_paths: [/var/log/syslog]
  service:
    pipelines:
      default_pipeline:
        receivers: [syslog]
`), 0644)
	require.NoError(t, err)

	t.Setenv("RUNTIME_DIRECTORY", filepath.Join(tmpDir, "run"))
	t.Setenv("STATE_DIRECTORY", filepath.Join(tmpDir, "state"))
	t.Setenv("LOGS_DIRECTORY", filepath.Join(tmpDir, "log"))

	p := NewFactory().Create(confmap.ProviderSettings{Logger: zap.NewNop()})
	require.Equal(t, "opsagentconf", p.Scheme())

	ctx := (platform.Platform{
		Type:             platform.Linux,
		HostInfo:         &host.InfoStat{Hostname: "test-host", OS: "linux", Platform: "debian", PlatformVersion: "12"},
		ResourceOverride: resourcedetector.GCEResource{Project: "test-project"},
	}).TestContext(context.Background())
	ret, err := p.Retrieve(ctx, "opsagentconf:"+configPath, nil)
	require.NoError(t, err)

	conf, err := ret.AsConf()
	require.NoError(t, err)
	assertPrimitiveYAMLTypes(t, "root", conf.ToStringMap())
	require.NoError(t, p.Shutdown(ctx))
}
