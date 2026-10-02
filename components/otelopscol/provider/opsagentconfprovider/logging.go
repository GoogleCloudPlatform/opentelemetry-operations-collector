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

// Logging represents logging pipelines, receivers, and processors in the Ops Agent configuration.
type Logging struct {
	Receivers  map[string]LoggingReceiver  `yaml:"receivers,omitempty"`
	Processors map[string]LoggingProcessor `yaml:"processors,omitempty"`
	Service    *LoggingService             `yaml:"service,omitempty"`
}

// LoggingReceiver represents a logging receiver in the Ops Agent configuration.
type LoggingReceiver struct {
	Type                    string   `yaml:"type"`
	IncludePaths            []string `yaml:"include_paths,omitempty"`
	ExcludePaths            []string `yaml:"exclude_paths,omitempty"`
	WildcardRefreshInterval string   `yaml:"wildcard_refresh_interval,omitempty"`
	RecordLogFilePath       *bool    `yaml:"record_log_file_path,omitempty"`
}

// LoggingProcessor represents a logging processor in the Ops Agent configuration.
type LoggingProcessor struct {
	Type string `yaml:"type"`
}

// LoggingService represents the logging service section in the Ops Agent configuration.
type LoggingService struct {
	LogLevel  string               `yaml:"log_level,omitempty"`
	Pipelines map[string]*Pipeline `yaml:"pipelines,omitempty"`
}

func defaultLoggingConfig(isWindows bool) *Logging {
	if isWindows {
		return &Logging{
			Receivers:  map[string]LoggingReceiver{},
			Processors: map[string]LoggingProcessor{},
			Service: &LoggingService{
				Pipelines: map[string]*Pipeline{
					"default_pipeline": {},
				},
			},
		}
	}
	return &Logging{
		Receivers: map[string]LoggingReceiver{
			"syslog": {
				Type:         "files",
				IncludePaths: []string{"/var/log/messages", "/var/log/syslog"},
			},
		},
		Processors: map[string]LoggingProcessor{},
		Service: &LoggingService{
			Pipelines: map[string]*Pipeline{
				"default_pipeline": {
					ReceiverIDs: []string{"syslog"},
				},
			},
		},
	}
}

func mergeLoggingConfig(user *Logging, isWindows bool) *Logging {
	merged := defaultLoggingConfig(isWindows)
	if user == nil {
		return merged
	}

	maps.Copy(merged.Receivers, user.Receivers)
	maps.Copy(merged.Processors, user.Processors)

	if user.Service != nil {
		if user.Service.LogLevel != "" && user.Service.LogLevel != "info" {
			merged.Service.LogLevel = user.Service.LogLevel
		}
		mergePipelines(merged.Service.Pipelines, user.Service.Pipelines)
	}

	return merged
}

func validateWildcardRefreshInterval(id, interval string) error {
	if interval == "" {
		return nil
	}
	d, err := time.ParseDuration(interval)
	if err != nil || d < time.Second || d%time.Second != 0 {
		return fmt.Errorf("logging receiver %q has invalid wildcard_refresh_interval %q: must be a duration >= 1s and a multiple of 1s", id, interval)
	}
	return nil
}

func (l *Logging) validate(combined *Combined) error {
	for _, id := range slices.Sorted(maps.Keys(l.Receivers)) {
		if err := validateComponentID("logging", "receiver", id); err != nil {
			return err
		}
		r := l.Receivers[id]
		switch r.Type {
		case "files":
			if len(r.IncludePaths) == 0 {
				return fmt.Errorf("logging receiver %q with type %q requires non-empty include_paths", id, r.Type)
			}
			for _, p := range slices.Concat(r.IncludePaths, r.ExcludePaths) {
				if strings.Contains(p, "${HOSTNAME}") {
					return fmt.Errorf("logging receiver %q with type %q does not support ${HOSTNAME} in paths", id, r.Type)
				}
			}
			if err := validateWildcardRefreshInterval(id, r.WildcardRefreshInterval); err != nil {
				return err
			}
		default:
			return fmt.Errorf("logging receiver %q with type %q is not supported", id, r.Type)
		}
	}

	if combined != nil {
		for _, id := range slices.Sorted(maps.Keys(combined.Receivers)) {
			if _, ok := l.Receivers[id]; ok {
				return fmt.Errorf("logging receiver %q has the same name as combined receiver %q", id, id)
			}
		}
	}

	for _, id := range slices.Sorted(maps.Keys(l.Processors)) {
		if err := validateComponentID("logging", "processor", id); err != nil {
			return err
		}
		p := l.Processors[id]
		return fmt.Errorf("logging processor %q with type %q is not supported", id, p.Type)
	}

	if l.Service == nil {
		return nil
	}

	if l.Service.LogLevel != "" {
		switch l.Service.LogLevel {
		case "error", "warn", "info", "debug", "trace":
		default:
			return fmt.Errorf("logging service has invalid log_level %q", l.Service.LogLevel)
		}
	}

	for _, pID := range slices.Sorted(maps.Keys(l.Service.Pipelines)) {
		if err := validateComponentID("logging", "pipeline", pID); err != nil {
			return err
		}
		p := l.Service.Pipelines[pID]
		if p == nil {
			continue
		}
		for _, rID := range p.ReceiverIDs {
			if _, ok := l.Receivers[rID]; !ok {
				return fmt.Errorf("logging receiver %q from pipeline %q is not defined", rID, pID)
			}
		}
		for _, prID := range p.ProcessorIDs {
			if _, ok := l.Processors[prID]; !ok {
				return fmt.Errorf("logging processor %q from pipeline %q is not defined", prID, pID)
			}
		}
	}

	return nil
}

func (r LoggingReceiver) filesReceiver() map[string]any {
	excludePaths := r.ExcludePaths
	if excludePaths == nil {
		excludePaths = []string{}
	}
	operators := []map[string]any{
		{
			"id":   "body",
			"type": "move",
			"from": "body",
			"to":   "body.message",
		},
	}
	cfg := map[string]any{
		"include":                       slices.Clone(r.IncludePaths),
		"exclude":                       slices.Clone(excludePaths),
		"start_at":                      "beginning",
		"include_file_name":             false,
		"preserve_leading_whitespaces":  true,
		"preserve_trailing_whitespaces": true,
		"fingerprint_size":              "5kb",
		"storage":                       fileStorageExtensionType,
	}
	if r.WildcardRefreshInterval != "" {
		if d, err := time.ParseDuration(r.WildcardRefreshInterval); err == nil {
			cfg["poll_interval"] = d.String()
		}
	}
	if r.RecordLogFilePath != nil && *r.RecordLogFilePath {
		cfg["include_file_path"] = true
		operators = append(operators, map[string]any{
			"id":   "record_log_file_path",
			"type": "move",
			"from": `attributes["log.file.path"]`,
			"to":   `attributes["agent.googleapis.com/log_file_path"]`,
		})
	}
	cfg["operators"] = operators
	return cfg
}

func (b *collectorConfig) addLoggingPipelines(l *Logging, info hostInfo) {
	if l == nil || l.Service == nil {
		return
	}
	for _, pID := range slices.Sorted(maps.Keys(l.Service.Pipelines)) {
		p := l.Service.Pipelines[pID]
		if p == nil {
			continue
		}
		escapedPID := escapeComponentID(pID)
		for _, rID := range p.ReceiverIDs {
			escapedRID := escapeComponentID(rID)

			if r, ok := l.Receivers[rID]; ok {
				receiverPipelineName := fmt.Sprintf("logging_%s", escapedRID)
				prefix := fmt.Sprintf("logs_%s_%s", escapedPID, receiverPipelineName)
				switch r.Type {
				case "files":
					receiverName := fmt.Sprintf("file_log/%s", receiverPipelineName)
					b.receivers[receiverName] = r.filesReceiver()
					receiverProcessors := []namedProcessor{
						{
							id:     fmt.Sprintf("transform/%s_0", receiverPipelineName),
							config: setLogNameProcessor(rID, info.Hostname),
						},
					}
					b.registerLoggingPipeline(prefix, receiverName, receiverProcessors)
				}
			}
		}
	}
}

func (b *collectorConfig) registerLoggingPipeline(prefix, receiverName string, receiverProcessors []namedProcessor) {
	b.processors["resource/otlp_grpc/otlp_logs_logs_1"] = disableOtlpRoundTripProcessor()
	b.processors["transform/otlp_grpc/otlp_logs_logs_2"] = preserveInstrumentationScopeProcessor()
	b.processors["transform/otlp_grpc/otlp_logs_logs_3"] = copyServiceResourceLabelsProcessor()

	processorNames := append(
		registerProcessors(b.processors, receiverProcessors),
		"resourcedetection/_global_0",
		"resource/otlp_grpc/otlp_logs_logs_1",
		"transform/otlp_grpc/otlp_logs_logs_2",
		"transform/otlp_grpc/otlp_logs_logs_3",
	)

	b.pipelines["logs/"+prefix] = map[string]any{
		"receivers":  []string{receiverName},
		"processors": processorNames,
		"exporters":  []string{"otlp_grpc/otlp_logs"},
	}
}
