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

package googlecontrolplaneextension

import (
	"context"
	"fmt"
	"strconv"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/extension/googlecontrolplaneextension/internal/metadata"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/extension/googlecontrolplaneextension/internal/telemetry"
)

// MetricPolicySetActive is the name of the gcp.policy.set.active metric.
const MetricPolicySetActive = telemetry.PolicySetActiveMetricName

// AttrPolicySetID is the attribute key for the policy set resource name.
const AttrPolicySetID = "gcp.policy.set.id"

type controlPlaneExtension struct {
	cfg    *Config
	set    extension.Settings
	logger *zap.Logger
	source PolicyStateSource

	metrics            *telemetry.Metrics
	registration       metric.Registration
	lastWarnedRevision string
}

var _ extension.Extension = (*controlPlaneExtension)(nil)

func newControlPlaneExtension(cfg *Config, set extension.Settings, source PolicyStateSource) *controlPlaneExtension {
	return &controlPlaneExtension{
		cfg:    cfg,
		set:    set,
		logger: set.Logger,
		source: source,
	}
}

// Start registers the observable instruments against the collector's internal
// MeterProvider.
//
// Registration happens here rather than in the factory so that it is tied to
// the component lifecycle and can be undone in Shutdown. Instruments registered
// on this MeterProvider are picked up by service::telemetry::metrics and
// exported through the self metrics pipeline; this extension does not need to
// sit in a pipeline itself.
func (e *controlPlaneExtension) Start(_ context.Context, _ component.Host) error {
	mp := e.set.TelemetrySettings.MeterProvider
	if mp == nil {
		e.logger.Debug("No MeterProvider available, control plane metrics will not be reported")
		return nil
	}

	meter := mp.Meter(metadata.ScopeName, metric.WithSchemaURL(telemetry.SchemaURL))

	metrics, err := telemetry.NewMetrics(meter)
	if err != nil {
		return err
	}
	e.metrics = metrics

	e.registration, err = meter.RegisterCallback(e.observe, e.metrics.Observables()...)
	if err != nil {
		return fmt.Errorf("failed to register control plane metric callback: %w", err)
	}

	return nil
}

// observe is invoked by the SDK on every collection cycle, so the reported
// value is always current. That is why this extension does not need to
// subscribe to googlepolicy.RegisterWatcherChannel the way the processor does:
// pulling on collection is simpler than maintaining push state, and cannot
// drift from the registry.
func (e *controlPlaneExtension) observe(_ context.Context, obs metric.Observer) error {
	// Exactly one series, always. Emitting nothing when no policy set is active
	// would make the series disappear, which breaks joins against other
	// collector metrics such as target_info, and makes "this collector is not
	// control plane managed" indistinguishable from "this collector is not
	// reporting at all".
	state := e.source.ActivePolicySet()
	val, ok := parseRevisionValue(state.Revision)
	if !ok {
		val = -1
		if state.Revision != e.lastWarnedRevision {
			e.logger.Warn("Failed to parse policy set revision as integer for self metric, reporting -1", zap.String("revision", state.Revision))
			e.lastWarnedRevision = state.Revision
		}
	}
	e.metrics.ObservePolicySetActive(obs, val, state.ID)

	return nil
}

// parseRevisionValue parses a policy set revision ID into an integer metric value.
// CCFE 8-hex revision IDs (GENERATE_UUID_8) are parsed as unsigned 32-bit integers.
// If the revision is empty, it returns (-1, true).
// If the revision cannot be parsed, it returns (0, false).
func parseRevisionValue(rev string) (int64, bool) {
	if rev == "" {
		return -1, true
	}
	// CCFE 8-hex revision IDs (GENERATE_UUID_8): parse as unsigned 32-bit int.
	if len(rev) == 8 {
		if val, err := strconv.ParseUint(rev, 16, 32); err == nil {
			return int64(val), true
		}
	}
	// File-based policies with 64-char SHA256 hashes: parse first 8 hex characters.
	if len(rev) == 64 {
		if val, err := strconv.ParseUint(rev[:8], 16, 32); err == nil {
			return int64(val), true
		}
	}
	// Fall back to decimal integer (e.g. in test environments).
	if val, err := strconv.ParseInt(rev, 10, 64); err == nil && val >= 0 {
		return val, true
	}
	return 0, false
}

func (e *controlPlaneExtension) Shutdown(context.Context) error {
	if e.registration == nil {
		return nil
	}
	err := e.registration.Unregister()
	e.registration = nil
	return err
}
