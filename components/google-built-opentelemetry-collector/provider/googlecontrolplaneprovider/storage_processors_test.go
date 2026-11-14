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

package googlecontrolplaneprovider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitSharedStorageProcessors(t *testing.T) {
	persistent := map[string]any{"storage": "file_storage"}
	conf := map[string]any{
		"processors": map[string]any{
			"queue_batch/persistent": persistent,
			"queue_batch/memory":     map[string]any{},
			"queue_batch":            persistent,
		},
		"service": map[string]any{"pipelines": map[string]any{
			"logs/googlecontrolplane": map[string]any{"processors": []any{"queue_batch/persistent", "queue_batch/memory", "queue_batch"}},
			"logs/self":               map[string]any{"processors": []any{"queue_batch/persistent", "queue_batch/memory", "queue_batch"}},
			"logs/alone":              map[string]any{"processors": []any{"queue_batch/memory"}},
		}},
	}

	splitSharedStorageProcessors(conf)

	pipelines := conf["service"].(map[string]any)["pipelines"].(map[string]any)
	assert.Equal(t, []any{"queue_batch/persistent", "queue_batch/memory", "queue_batch"},
		pipelines["logs/googlecontrolplane"].(map[string]any)["processors"], "canonical pipeline keeps the original IDs")
	assert.Equal(t, []any{"queue_batch/persistent_self", "queue_batch/memory", "queue_batch/self"},
		pipelines["logs/self"].(map[string]any)["processors"], "only storage-backed processors are copied")
	assert.Equal(t, []any{"queue_batch/memory"}, pipelines["logs/alone"].(map[string]any)["processors"])

	processors := conf["processors"].(map[string]any)
	assert.Equal(t, persistent, processors["queue_batch/persistent_self"])
	assert.Equal(t, persistent, processors["queue_batch/self"])
	assert.Len(t, processors, 5)
}
