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
	"strings"
	"time"
)

const minCollectionInterval = 10 * time.Second

// Metrics represents metrics pipelines, receivers, and processors in the Ops Agent configuration.
type Metrics struct {
	Receivers  map[string]MetricsReceiver  `yaml:"receivers,omitempty"`
	Processors map[string]MetricsProcessor `yaml:"processors,omitempty"`
	Service    *MetricsService             `yaml:"service,omitempty"`
}

// MetricsReceiver represents a metrics receiver in the Ops Agent configuration.
type MetricsReceiver struct {
	Type               string         `yaml:"type"`
	CollectionInterval string         `yaml:"collection_interval,omitempty"`
	ReceiverVersion    string         `yaml:"receiver_version,omitempty"`
	Config             map[string]any `yaml:"config,omitempty"`
}

func (r MetricsReceiver) collectionInterval() string {
	if r.CollectionInterval != "" {
		return r.CollectionInterval
	}
	return defaultHostmetricsCollectionInterval
}

// MetricsProcessor represents a metrics processor in the Ops Agent configuration.
type MetricsProcessor struct {
	Type           string   `yaml:"type"`
	MetricsPattern []string `yaml:"metrics_pattern,omitempty,flow"`
}

// MetricsService represents the metrics service configuration.
type MetricsService struct {
	LogLevel  string               `yaml:"log_level,omitempty"`
	Pipelines map[string]*Pipeline `yaml:"pipelines,omitempty"`
}

// Pipeline represents a pipeline of receivers and processors.
type Pipeline struct {
	ReceiverIDs  []string `yaml:"receivers,omitempty,flow"`
	ProcessorIDs []string `yaml:"processors,omitempty,flow"`
}

func defaultMetricsConfig(isWindows bool) *Metrics {
	receivers := map[string]MetricsReceiver{
		"hostmetrics": {
			Type:               "hostmetrics",
			CollectionInterval: defaultHostmetricsCollectionInterval,
		},
	}
	defaultReceiverIDs := []string{"hostmetrics"}
	if isWindows {
		receivers["iis"] = MetricsReceiver{
			Type:               "iis",
			CollectionInterval: defaultHostmetricsCollectionInterval,
		}
		receivers["mssql"] = MetricsReceiver{
			Type:               "mssql",
			CollectionInterval: defaultHostmetricsCollectionInterval,
		}
		defaultReceiverIDs = []string{"hostmetrics", "iis", "mssql"}
	}
	return &Metrics{
		Receivers: receivers,
		Processors: map[string]MetricsProcessor{
			"metrics_filter": {
				Type: "exclude_metrics",
			},
		},
		Service: &MetricsService{
			Pipelines: map[string]*Pipeline{
				"default_pipeline": {
					ReceiverIDs:  defaultReceiverIDs,
					ProcessorIDs: []string{"metrics_filter"},
				},
			},
		},
	}
}

func mergeMetricsConfig(user *Metrics, isWindows bool) *Metrics {
	merged := defaultMetricsConfig(isWindows)
	if user == nil {
		return merged
	}

	maps.Copy(merged.Receivers, user.Receivers)
	maps.Copy(merged.Processors, user.Processors)

	if user.Service != nil {
		if user.Service.LogLevel != "" && user.Service.LogLevel != "info" {
			merged.Service.LogLevel = user.Service.LogLevel
		}
		for name, p := range user.Service.Pipelines {
			if p == nil {
				merged.Service.Pipelines[name] = &Pipeline{}
				continue
			}
			merged.Service.Pipelines[name] = &Pipeline{
				ReceiverIDs:  slices.Clone(p.ReceiverIDs),
				ProcessorIDs: slices.Clone(p.ProcessorIDs),
			}
		}
	}

	return merged
}

func validateComponentID(subagent, kind, id string) error {
	if strings.HasPrefix(id, "lib:") {
		return fmt.Errorf("%s %s ID %q cannot start with \"lib:\"", subagent, kind, id)
	}
	return nil
}

func validateCollectionInterval(id, interval string) error {
	if interval == "" {
		return nil
	}
	d, err := time.ParseDuration(interval)
	if err != nil || d < minCollectionInterval {
		return fmt.Errorf("metrics receiver %q has invalid collection_interval %q: must be a duration >= %s", id, interval, minCollectionInterval)
	}
	return nil
}

func (m *Metrics) validate(combined *Combined, isWindows bool) error {
	for _, id := range slices.Sorted(maps.Keys(m.Receivers)) {
		if err := validateComponentID("metrics", "receiver", id); err != nil {
			return err
		}
		r := m.Receivers[id]
		switch r.Type {
		case "hostmetrics":
			if r.Config != nil {
				return fmt.Errorf("metrics receiver %q with type %q does not support config", id, r.Type)
			}
			if r.ReceiverVersion != "" {
				return fmt.Errorf("metrics receiver %q with type %q does not support receiver_version", id, r.Type)
			}
			if err := validateCollectionInterval(id, r.CollectionInterval); err != nil {
				return err
			}
		case "iis", "mssql":
			if !isWindows {
				return fmt.Errorf("metrics receiver %q with type %q is not supported", id, r.Type)
			}
			if r.Config != nil {
				return fmt.Errorf("metrics receiver %q with type %q does not support config", id, r.Type)
			}
			switch r.ReceiverVersion {
			case "", "1", "2":
			default:
				return fmt.Errorf("metrics receiver %q has invalid receiver_version %q: must be one of [1 2]", id, r.ReceiverVersion)
			}
			if err := validateCollectionInterval(id, r.CollectionInterval); err != nil {
				return err
			}
		case "prometheus":
			if r.CollectionInterval != "" {
				return fmt.Errorf("metrics receiver %q with type %q does not support collection_interval", id, r.Type)
			}
			if r.ReceiverVersion != "" {
				return fmt.Errorf("metrics receiver %q with type %q does not support receiver_version", id, r.Type)
			}
			if err := validatePrometheusConfig(r.Config); err != nil {
				return fmt.Errorf("metrics receiver %q has invalid prometheus config: %w", id, err)
			}
		default:
			return fmt.Errorf("metrics receiver %q with type %q is not supported", id, r.Type)
		}
	}

	if combined != nil {
		for _, id := range slices.Sorted(maps.Keys(combined.Receivers)) {
			if _, ok := m.Receivers[id]; ok {
				return fmt.Errorf("metrics receiver %q has the same name as combined receiver %q", id, id)
			}
		}
	}

	for _, id := range slices.Sorted(maps.Keys(m.Processors)) {
		if err := validateComponentID("metrics", "processor", id); err != nil {
			return err
		}
		p := m.Processors[id]
		if p.Type != "exclude_metrics" {
			return fmt.Errorf("metrics processor %q with type %q is not supported", id, p.Type)
		}
	}

	if m.Service == nil {
		return nil
	}

	if m.Service.LogLevel != "" {
		switch m.Service.LogLevel {
		case "error", "warn", "info", "debug":
		default:
			return fmt.Errorf("metrics service has invalid log_level %q", m.Service.LogLevel)
		}
	}

	for _, pID := range slices.Sorted(maps.Keys(m.Service.Pipelines)) {
		if err := validateComponentID("metrics", "pipeline", pID); err != nil {
			return err
		}
		p := m.Service.Pipelines[pID]
		if p == nil {
			continue
		}
		typeCounts := map[string]int{}
		for _, rID := range p.ReceiverIDs {
			if r, ok := m.Receivers[rID]; ok {
				switch r.Type {
				case "hostmetrics", "iis", "mssql":
					typeCounts[r.Type]++
					if typeCounts[r.Type] > 1 {
						return fmt.Errorf("at most one metrics receiver with type %q is allowed in pipeline %q", r.Type, pID)
					}
				case "prometheus":
					if len(p.ProcessorIDs) > 0 {
						return fmt.Errorf("%s receiver is incompatible with Ops Agent processors", rID)
					}
				}
				continue
			}
			if combined != nil {
				if cr, ok := combined.Receivers[rID]; ok {
					if !cr.allowCustomProcessors() && len(p.ProcessorIDs) > 0 {
						return fmt.Errorf("%s receiver is incompatible with Ops Agent processors", rID)
					}
					continue
				}
			}
			return fmt.Errorf("metrics receiver %q from pipeline %q is not defined", rID, pID)
		}
		for _, prID := range p.ProcessorIDs {
			if _, ok := m.Processors[prID]; !ok {
				return fmt.Errorf("metrics processor %q from pipeline %q is not defined", prID, pID)
			}
		}
	}

	return nil
}

func (b *collectorConfig) addMetricsPipelines(m *Metrics, combined *Combined, isWindows bool, exporterProcessors []string) {
	if m == nil || m.Service == nil {
		return
	}
	for _, pID := range slices.Sorted(maps.Keys(m.Service.Pipelines)) {
		p := m.Service.Pipelines[pID]
		if p == nil {
			continue
		}
		escapedPID := escapeComponentID(pID)
		for _, rID := range p.ReceiverIDs {
			escapedRID := escapeComponentID(rID)
			prefix := fmt.Sprintf("%s_%s", escapedPID, escapedRID)

			var receiverName string
			var receiverProcIDs []string
			pipelineExporterProcs := exporterProcessors

			if r, ok := m.Receivers[rID]; ok {
				switch r.Type {
				case "hostmetrics":
					receiverName = fmt.Sprintf("hostmetrics/%s", escapedRID)
					b.receivers[receiverName] = hostmetricsReceiver(r.CollectionInterval, isWindows)
					receiverProcIDs = registerProcessors(b.processors, hostmetricsProcessors(escapedRID, isWindows))
				case "iis":
					var receiverCfg map[string]any
					receiverName, receiverCfg = r.iisReceiver(escapedRID)
					b.receivers[receiverName] = receiverCfg
					receiverProcIDs = registerProcessors(b.processors, r.iisProcessors(escapedRID))
				case "mssql":
					var receiverCfg map[string]any
					receiverName, receiverCfg = r.mssqlReceiver(escapedRID)
					b.receivers[receiverName] = receiverCfg
					receiverProcIDs = registerProcessors(b.processors, r.mssqlProcessors(escapedRID))
				case "prometheus":
					receiverName = fmt.Sprintf("prometheus/%s", escapedRID)
					b.receivers[receiverName] = prometheusReceiver(r.Config)
					receiverProcIDs = registerProcessors(b.processors, gmpMetricsProcessors(escapedRID))
					pipelineExporterProcs = exporterProcessors[1:]
				}
			} else if combined != nil {
				if cr, ok := combined.Receivers[rID]; ok {
					receiverName = fmt.Sprintf("otlp/%s", escapedRID)
					b.receivers[receiverName] = cr.otlpReceiver()
					receiverProcIDs = registerProcessors(b.processors, cr.otlpMetricsProcessors(escapedRID))
					pipelineExporterProcs = cr.metricsExporterProcessors(b.processors, exporterProcessors[1:])
				}
			}

			pipelineProcIDs := make([]string, 0, len(p.ProcessorIDs))
			for i, prID := range p.ProcessorIDs {
				proc := m.Processors[prID]
				procID := fmt.Sprintf("filter/%s_%d", prefix, i)
				b.processors[procID] = metricsExcludeRegexpFilterProcessor(proc.MetricsPattern...)
				pipelineProcIDs = append(pipelineProcIDs, procID)
			}

			b.pipelines["metrics/"+prefix] = map[string]any{
				"receivers":  []string{receiverName},
				"processors": slices.Concat(receiverProcIDs, pipelineProcIDs, pipelineExporterProcs),
				"exporters":  []string{"otlp_grpc/otlp_metrics"},
			}
		}
	}
}

func escapeComponentID(id string) string {
	return strings.ReplaceAll(id, "_", "__")
}
