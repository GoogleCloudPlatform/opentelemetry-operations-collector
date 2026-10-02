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
	"fmt"
	"maps"
	"slices"
)

// Traces represents traces pipelines in the Ops Agent configuration.
type Traces struct {
	Service *TracesService `yaml:"service,omitempty"`
}

// TracesService represents the traces service configuration.
type TracesService struct {
	Pipelines map[string]*Pipeline `yaml:"pipelines,omitempty"`
}

func (t *Traces) validate(combined *Combined) error {
	if t == nil || t.Service == nil {
		return nil
	}
	for _, pID := range slices.Sorted(maps.Keys(t.Service.Pipelines)) {
		if err := validateComponentID("traces", "pipeline", pID); err != nil {
			return err
		}
		p := t.Service.Pipelines[pID]
		if p == nil {
			continue
		}
		for _, rID := range p.ReceiverIDs {
			if combined == nil {
				return fmt.Errorf("traces receiver %q from pipeline %q is not defined", rID, pID)
			}
			if _, ok := combined.Receivers[rID]; !ok {
				return fmt.Errorf("traces receiver %q from pipeline %q is not defined", rID, pID)
			}
		}
		if len(p.ProcessorIDs) > 0 {
			return fmt.Errorf("traces pipeline %q uses processors but traces pipelines do not support processors", pID)
		}
	}
	return nil
}

func (b *collectorConfig) addTracesPipelines(t *Traces, combined *Combined, userAgent string) {
	if t == nil || t.Service == nil || combined == nil {
		return
	}
	for _, pID := range slices.Sorted(maps.Keys(t.Service.Pipelines)) {
		p := t.Service.Pipelines[pID]
		if p == nil {
			continue
		}
		escapedPID := escapeComponentID(pID)
		for _, rID := range p.ReceiverIDs {
			r := combined.Receivers[rID]
			escapedRID := escapeComponentID(rID)
			prefix := fmt.Sprintf("traces_%s_%s", escapedPID, escapedRID)

			receiverName := fmt.Sprintf("otlp/%s", escapedRID)
			b.receivers[receiverName] = r.otlpReceiver()
			b.processors["resourcedetection/_global_1"] = gcpResourceDetectorProcessor(false)
			b.processors["batch/otlp_grpc/otlp_traces_traces_1"] = batchProcessor()
			b.exporters["otlp_grpc/otlp_traces"] = otlpExporter(userAgent)

			b.pipelines["traces/"+prefix] = map[string]any{
				"receivers": []string{receiverName},
				"processors": []string{
					"resourcedetection/_global_1",
					"batch/otlp_grpc/otlp_traces_traces_1",
				},
				"exporters": []string{"otlp_grpc/otlp_traces"},
			}
		}
	}
}
