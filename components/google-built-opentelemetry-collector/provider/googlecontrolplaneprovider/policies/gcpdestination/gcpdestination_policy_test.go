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

package gcpdestination

import (
	"testing"

	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/processor/queuebatchprocessor"
)

func TestQueueBatchSizers(t *testing.T) {
	cfg := queuebatchprocessor.NewFactory().CreateDefaultConfig().(*queuebatchprocessor.Config)
	cfg.Sizer = exporterhelper.RequestSizerTypeBytes
	batch := cfg.Batch.GetOrInsertDefault()
	batch.Sizer = exporterhelper.RequestSizerTypeItems

	got, err := queueBatchSizers(cfg)
	if err != nil {
		t.Fatalf("queueBatchSizers() err = %v", err)
	}
	if got["sizer"] != "bytes" {
		t.Errorf("queue sizer = %v, want bytes", got["sizer"])
	}
	batchSizers, ok := got["batch"].(map[string]any)
	if !ok || batchSizers["sizer"] != "items" {
		t.Errorf("batch sizers = %v, want sizer items", got["batch"])
	}
}
