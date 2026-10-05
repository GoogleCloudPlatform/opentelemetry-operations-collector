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
	"net"
	"slices"
	"strings"
	"time"
)

const (
	defaultFluentForwardHost     = "127.0.0.1"
	defaultFluentForwardPort     = uint16(24224)
	defaultSyslogProtocolRFC5424 = "rfc5424"
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
	TransportProtocol       string   `yaml:"transport_protocol,omitempty"`
	ListenHost              string   `yaml:"listen_host,omitempty"`
	ListenPort              uint16   `yaml:"listen_port,omitempty"`
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

func (r LoggingReceiver) validateNoFilesFields(id string) error {
	if len(r.IncludePaths) > 0 {
		return fmt.Errorf("logging receiver %q with type %q does not support include_paths", id, r.Type)
	}
	if len(r.ExcludePaths) > 0 {
		return fmt.Errorf("logging receiver %q with type %q does not support exclude_paths", id, r.Type)
	}
	if r.WildcardRefreshInterval != "" {
		return fmt.Errorf("logging receiver %q with type %q does not support wildcard_refresh_interval", id, r.Type)
	}
	if r.RecordLogFilePath != nil {
		return fmt.Errorf("logging receiver %q with type %q does not support record_log_file_path", id, r.Type)
	}
	return nil
}

func (r LoggingReceiver) validateNoNetworkFields(id string) error {
	if r.TransportProtocol != "" {
		return fmt.Errorf("logging receiver %q with type %q does not support transport_protocol", id, r.Type)
	}
	if r.ListenHost != "" {
		return fmt.Errorf("logging receiver %q with type %q does not support listen_host", id, r.Type)
	}
	if r.ListenPort != 0 {
		return fmt.Errorf("logging receiver %q with type %q does not support listen_port", id, r.Type)
	}
	return nil
}

func (r LoggingReceiver) listenHost() string {
	if r.ListenHost == "" {
		return defaultFluentForwardHost
	}
	return r.ListenHost
}

func (r LoggingReceiver) listenPort() (uint16, bool) {
	switch r.Type {
	case "syslog":
		return r.ListenPort, true
	case "fluent_forward":
		if r.ListenPort == 0 {
			return defaultFluentForwardPort, true
		}
		return r.ListenPort, true
	default:
		return 0, false
	}
}

func (l *Logging) validate(combined *Combined, isWindows bool) error {
	for _, id := range slices.Sorted(maps.Keys(l.Receivers)) {
		if err := validateComponentID("logging", "receiver", id); err != nil {
			return err
		}
		r := l.Receivers[id]
		switch r.Type {
		case "files":
			if err := r.validateNoNetworkFields(id); err != nil {
				return err
			}
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
		case "syslog":
			if err := r.validateNoFilesFields(id); err != nil {
				return err
			}
			switch r.TransportProtocol {
			case "tcp", "udp":
			default:
				return fmt.Errorf("logging receiver %q with type %q has invalid transport_protocol %q: must be one of [tcp udp]", id, r.Type, r.TransportProtocol)
			}
			if r.ListenHost == "" || net.ParseIP(r.ListenHost) == nil {
				return fmt.Errorf("logging receiver %q with type %q has invalid listen_host %q: must be a valid IP address", id, r.Type, r.ListenHost)
			}
			if r.ListenPort == 0 {
				return fmt.Errorf("logging receiver %q with type %q requires non-zero listen_port", id, r.Type)
			}
		case "fluent_forward":
			if err := r.validateNoFilesFields(id); err != nil {
				return err
			}
			if r.TransportProtocol != "" {
				return fmt.Errorf("logging receiver %q with type %q does not support transport_protocol", id, r.Type)
			}
			if r.ListenHost != "" && net.ParseIP(r.ListenHost) == nil {
				return fmt.Errorf("logging receiver %q with type %q has invalid listen_host %q: must be a valid IP address", id, r.Type, r.ListenHost)
			}
		case "systemd_journald":
			if isWindows {
				return fmt.Errorf("logging receiver %q with type %q is not supported", id, r.Type)
			}
			if err := r.validateNoFilesFields(id); err != nil {
				return err
			}
			if err := r.validateNoNetworkFields(id); err != nil {
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

	portTaken := map[uint16]string{}
	for _, pID := range slices.Sorted(maps.Keys(l.Service.Pipelines)) {
		if err := validateComponentID("logging", "pipeline", pID); err != nil {
			return err
		}
		p := l.Service.Pipelines[pID]
		if p == nil {
			continue
		}
		for _, rID := range p.ReceiverIDs {
			r, inLogging := l.Receivers[rID]
			inCombined := combined != nil && combined.Receivers != nil
			if inCombined {
				_, inCombined = combined.Receivers[rID]
			}
			if !inLogging && !inCombined {
				return fmt.Errorf("logging receiver %q from pipeline %q is not defined", rID, pID)
			}
			if inLogging {
				if port, ok := r.listenPort(); ok {
					if prevRID, taken := portTaken[port]; taken {
						if prevRID == rID {
							return fmt.Errorf("logging receiver %q listening on port %d cannot be used in two pipelines", rID, port)
						}
						return fmt.Errorf("two logging receivers %q and %q cannot listen on the same port %d", prevRID, rID, port)
					}
					portTaken[port] = rID
				}
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

func (r LoggingReceiver) syslogReceiver() map[string]any {
	return map[string]any{
		r.TransportProtocol: map[string]any{
			"listen_address": fmt.Sprintf("%s:%d", r.ListenHost, r.ListenPort),
		},
		"protocol": defaultSyslogProtocolRFC5424,
	}
}

func (r LoggingReceiver) fluentForwardReceiver() map[string]any {
	port, _ := r.listenPort()
	return map[string]any{
		"endpoint": fmt.Sprintf("%s:%d", r.listenHost(), port),
	}
}

func journaldReceiver() map[string]any {
	return map[string]any{
		"start_at": "beginning",
		"priority": "debug",
		"storage":  fileStorageExtensionType,
	}
}

func (b *collectorConfig) addLoggingPipelines(l *Logging, combined *Combined, info hostInfo) {
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
					b.registerLoggingPipeline(
						prefix,
						receiverName,
						numberedTransformProcessors(receiverPipelineName, setLogNameProcessor(rID, info.Hostname)),
						true,
					)
				case "syslog":
					receiverName := fmt.Sprintf("syslog/%s", receiverPipelineName)
					b.receivers[receiverName] = r.syslogReceiver()
					b.registerLoggingPipeline(
						prefix,
						receiverName,
						numberedTransformProcessors(
							receiverPipelineName,
							syslogTransformProcessor(),
							setLogNameProcessor(rID, info.Hostname),
						),
						true,
					)
				case "fluent_forward":
					receiverName := fmt.Sprintf("fluentforward/%s", receiverPipelineName)
					b.receivers[receiverName] = r.fluentForwardReceiver()
					b.registerLoggingPipeline(
						prefix,
						receiverName,
						numberedTransformProcessors(
							receiverPipelineName,
							fluentForwardTransformProcessor(),
							setLogNameProcessor(rID, info.Hostname),
							fluentForwardSetLogNameProcessor(),
						),
						true,
					)
				case "systemd_journald":
					receiverName := fmt.Sprintf("journald/%s", receiverPipelineName)
					b.receivers[receiverName] = journaldReceiver()
					b.registerLoggingPipeline(
						prefix,
						receiverName,
						numberedTransformProcessors(
							receiverPipelineName,
							journaldTransformProcessor(),
							setLogNameProcessor(rID, info.Hostname),
						),
						true,
					)
				}
				continue
			}

			if combined != nil {
				if cr, ok := combined.Receivers[rID]; ok && cr.Type == "otlp" {
					receiverPipelineName := escapedRID
					prefix := fmt.Sprintf("logs_%s_%s", escapedPID, receiverPipelineName)
					receiverName := fmt.Sprintf("otlp/%s", receiverPipelineName)
					b.receivers[receiverName] = cr.otlpReceiver()
					b.registerLoggingPipeline(
						prefix,
						receiverName,
						numberedTransformProcessors(receiverPipelineName, setLogNameProcessor(rID, info.Hostname)),
						false,
					)
				}
			}
		}
	}
}

func numberedTransformProcessors(receiverPipelineName string, configs ...map[string]any) []namedProcessor {
	out := make([]namedProcessor, 0, len(configs))
	for i, cfg := range configs {
		out = append(out, namedProcessor{
			id:     fmt.Sprintf("transform/%s_%d", receiverPipelineName, i),
			config: cfg,
		})
	}
	return out
}

func (b *collectorConfig) registerLoggingPipeline(prefix, receiverName string, receiverProcessors []namedProcessor, overrideResource bool) {
	resourceDetectionID := "resourcedetection/_global_0"
	if !overrideResource {
		resourceDetectionID = "resourcedetection/_global_1"
		b.processors[resourceDetectionID] = gcpResourceDetectorProcessor(false)
	}
	b.processors["resource/otlp_grpc/otlp_logs_logs_1"] = disableOtlpRoundTripProcessor()
	b.processors["transform/otlp_grpc/otlp_logs_logs_2"] = preserveInstrumentationScopeProcessor()
	b.processors["transform/otlp_grpc/otlp_logs_logs_3"] = copyServiceResourceLabelsProcessor()

	processorNames := append(
		registerProcessors(b.processors, receiverProcessors),
		resourceDetectionID,
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
