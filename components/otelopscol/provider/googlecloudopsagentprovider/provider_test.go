// Copyright 2026 Google LLC
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

package googlecloudopsagentprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/collector/confmap"
)

func TestProvider(t *testing.T) {
	content := []byte(`
metrics:
  receivers:
    hostmetrics:
      type: hostmetrics
      collection_interval: 60s
  service:
    pipelines:
      default_pipeline:
        receivers: [hostmetrics]
`)

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, content, 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	p := newProvider(confmap.ProviderSettings{})
	uri := fmt.Sprintf("googlecloudopsagent:%s?platform=linux&state_dir=%s&out_dir=%s", cfgPath, tmpDir, tmpDir)

	ret, err := p.Retrieve(context.Background(), uri, nil)
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	// Retrieved has AsConf() method in confmap v1.0.0+
	conf, err := ret.AsConf()
	if err != nil {
		t.Fatalf("AsConf failed: %v", err)
	}

	if !conf.IsSet("receivers::hostmetrics/hostmetrics") {
		t.Errorf("expected hostmetrics receiver to be set")
	}
	if !conf.IsSet("service::pipelines::metrics/default__pipeline_hostmetrics") {
		t.Errorf("expected hostmetrics pipeline to be set")
	}
}
