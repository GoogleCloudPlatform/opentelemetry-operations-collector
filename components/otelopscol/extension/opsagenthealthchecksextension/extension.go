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

package opsagenthealthchecksextension

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/healthchecks"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/logs"
)

type opsagenthealthchecks struct {
	config *Config
	logger component.TelemetrySettings
}

func (ext *opsagenthealthchecks) Start(ctx context.Context, host component.Host) error {
	ext.logger.Logger.Info("Starting Ops Agent health checks...")
	go func() {
		// Create file logger for detailed results
		logDir := ext.config.LogDir
		if logDir == "" {
			logDir = "/var/log/google-cloud-ops-agent"
		}
		fileLogger := healthchecks.CreateHealthChecksLogger(logDir)

		// Create wrapper for collector logger to print summary/errors to console/syslog
		collectorLogger := logs.NewFromZap(ext.logger.Logger)

		registry := healthchecks.HealthCheckRegistryFactory()
		
		// Run checks. They will log to fileLogger during run.
		results := registry.RunAllHealthChecks(fileLogger)

		// Log results to collector logger (stdout/syslog)
		healthchecks.LogHealthCheckResults(results, collectorLogger)

		ext.logger.Logger.Info("Ops Agent health checks finished.")
	}()
	return nil
}

func (ext *opsagenthealthchecks) Shutdown(ctx context.Context) error {
	return nil
}
