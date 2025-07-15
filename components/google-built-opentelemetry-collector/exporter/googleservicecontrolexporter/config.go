// Copyright 2025 Google LLC
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

package googleservicecontrolexporter

import (
	"fmt"

	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

// Config defines configuration for Service Control Exporter
type Config struct {
	ServiceName               string `mapstructure:"service_name"`
	ConsumerProject           string `mapstructure:"consumer_project"`
	ServiceControlEndpoint    string `mapstructure:"service_control_endpoint"`
	ServiceConfigID           string `mapstructure:"service_config_id"`
	ImpersonateServiceAccount string `mapstructure:"impersonate_service_account"`
	// Whether to use servicecontrol library or raw sc client.
	// The Client Library's SC client supports authentications using ADC and WIF
	// https://cloud.google.com/kubernetes-engine/fleet-management/docs/use-workload-identity#authenticate_from_your_code
	// Defaults to `true`, so that existing customers are unaffected by changes.
	// See go/agent-gdce
	// TODO(b/400987158): remove the option and migrate all to Client Library.
	UseRawServiceControlClient string `mapstructure:"use_raw_sc_client"`
	EnableDebugHeaders         bool   `mapstructure:"enable_debug_headers"`
	// UseInsecure config the grpc client to use insecure credentials. Originally
	// the `disable_auth` config option in FluentBit.
	UseInsecure bool      `mapstructure:"use_insecure"`
	LogConfig   LogConfig `mapstructure:"log"`

	TimeoutConfig             exporterhelper.TimeoutConfig `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct.
	configretry.BackOffConfig `mapstructure:"retry_on_failure"`
	QueueConfig               exporterhelper.QueueBatchConfig `mapstructure:"sending_queue"`
}

type LogConfig struct {
	// DefaultLogName sets the fallback log name to use when one isn't explicitly set
	// for a log entry. If unset, logs without a log name will raise an error.
	// Corresponding to the plugin `alias` setting in FluentBit exporter
	DefaultLogName string `mapstructure:"default_log_name"`
	// OperationName sets the operation name for Service Control logs. If not
	// set, default to `log_entry`
	OperationName string `mapstructure:"operation_name"`
}

func (c *Config) Validate() error {
	if c.ServiceName == "" {
		return fmt.Errorf("empty service_name")
	}
	if c.ConsumerProject == "" {
		return fmt.Errorf("empty consumer_project")
	}
	if c.ServiceControlEndpoint == "" {
		return fmt.Errorf("empty service_control_endpoint")
	}
	return nil
}
