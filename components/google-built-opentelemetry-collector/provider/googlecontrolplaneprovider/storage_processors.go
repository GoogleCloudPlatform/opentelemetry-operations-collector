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
	"fmt"
	"maps"
	"slices"
	"strings"
)

// splitSharedStorageProcessors works around an upstream limitation: a
// processor with a persistent queue (`storage` set, e.g. queue_batch) keys its
// storage file by component ID and signal only, so when two pipelines reference
// the same processor ID, their two instances open the same file and the second
// fails to start on the file lock ("failed to start ... processor: timeout").
//
// It gives every non-canonical pipeline its own copy of such a processor,
// named <id>_<pipeline name>. The canonical googlecontrolplane pipelines keep
// the original IDs.
//
// TODO: remove once upstream keys persistent queue storage per pipeline.
func splitSharedStorageProcessors(conf map[string]any) {
	processors, _ := conf["processors"].(map[string]any)
	service, _ := conf["service"].(map[string]any)
	pipelines, _ := service["pipelines"].(map[string]any)

	// Number of pipelines referencing each storage-backed processor.
	users := map[string]int{}
	for _, p := range pipelines {
		for _, id := range pipelineProcessorList(p) {
			if hasStorage(processors[fmt.Sprint(id)]) {
				users[fmt.Sprint(id)]++
			}
		}
	}

	for _, pipelineID := range slices.Sorted(maps.Keys(pipelines)) {
		_, name, _ := strings.Cut(pipelineID, "/")
		if name == canonicalPipelineName {
			continue
		}
		list := pipelineProcessorList(pipelines[pipelineID])
		for i, id := range list {
			id := fmt.Sprint(id)
			if users[id] < 2 {
				continue
			}
			sep := "_"
			if !strings.Contains(id, "/") {
				sep = "/" // unnamed processor: "queue_batch" -> "queue_batch/<pipeline>"
			}
			copyID := id + sep + name
			processors[copyID] = processors[id]
			list[i] = copyID
		}
	}
}

// canonicalPipelineName is the name of the canonical workload pipelines
// (logs/googlecontrolplane, metrics/googlecontrolplane, ...).
const canonicalPipelineName = "googlecontrolplane"

func pipelineProcessorList(pipelineConf any) []any {
	m, _ := pipelineConf.(map[string]any)
	list, _ := m["processors"].([]any)
	return list
}

func hasStorage(processorConf any) bool {
	m, _ := processorConf.(map[string]any)
	return m["storage"] != nil && m["storage"] != ""
}
