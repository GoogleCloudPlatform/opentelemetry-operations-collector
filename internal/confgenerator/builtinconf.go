// Copyright 2021 Google LLC
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

package confgenerator

// BuiltInConfStructs contains the default configuration for each platform.
var BuiltInConfStructs = map[string]*UnifiedConfig{
	"linux": {
		Logging: &Logging{
			Receivers: map[string]LoggingReceiver{
				"syslog": &LoggingReceiverFiles{
					ConfigComponent: ConfigComponent{Type: "files"},
					IncludePaths:    []string{"/var/log/messages", "/var/log/syslog"},
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
		},
		Metrics: &Metrics{
			Receivers: map[string]MetricsReceiver{
				"hostmetrics": &MetricsReceiverHostmetrics{
					ConfigComponent:       ConfigComponent{Type: "hostmetrics"},
					MetricsReceiverShared: MetricsReceiverShared{CollectionInterval: "60s"},
				},
			},
			Processors: map[string]MetricsProcessor{
				"metrics_filter": &MetricsProcessorExcludeMetrics{
					ConfigComponent: ConfigComponent{Type: "exclude_metrics"},
				},
			},
			Service: &MetricsService{
				Pipelines: map[string]*Pipeline{
					"default_pipeline": {
						ReceiverIDs:  []string{"hostmetrics"},
						ProcessorIDs: []string{"metrics_filter"},
					},
				},
			},
		},
	},
	"windows": {
		Logging: &Logging{
			Receivers: map[string]LoggingReceiver{
				"windows_event_log": &LoggingReceiverWindowsEventLog{
					ConfigComponent: ConfigComponent{Type: "windows_event_log"},
					Channels:        []string{"System", "Application", "Security"},
				},
			},
			Processors: map[string]LoggingProcessor{},
			Service: &LoggingService{
				Pipelines: map[string]*Pipeline{
					"default_pipeline": {
						ReceiverIDs: []string{"windows_event_log"},
					},
				},
			},
		},
		Metrics: &Metrics{
			Receivers: map[string]MetricsReceiver{
				"hostmetrics": &MetricsReceiverHostmetrics{
					ConfigComponent:       ConfigComponent{Type: "hostmetrics"},
					MetricsReceiverShared: MetricsReceiverShared{CollectionInterval: "60s"},
				},
				"iis": &MetricsReceiverIis{
					ConfigComponent:       ConfigComponent{Type: "iis"},
					MetricsReceiverShared: MetricsReceiverShared{CollectionInterval: "60s"},
				},
				"mssql": &MetricsReceiverMssql{
					ConfigComponent:       ConfigComponent{Type: "mssql"},
					MetricsReceiverShared: MetricsReceiverShared{CollectionInterval: "60s"},
				},
			},
			Processors: map[string]MetricsProcessor{
				"metrics_filter": &MetricsProcessorExcludeMetrics{
					ConfigComponent: ConfigComponent{Type: "exclude_metrics"},
				},
			},
			Service: &MetricsService{
				Pipelines: map[string]*Pipeline{
					"default_pipeline": {
						ReceiverIDs:  []string{"hostmetrics", "iis", "mssql"},
						ProcessorIDs: []string{"metrics_filter"},
					},
				},
			},
		},
	},
}
