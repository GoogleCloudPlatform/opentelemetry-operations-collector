// Copyright 2025 Google LLC
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

package self_metrics

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/confgenerator"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	meterName                      = "ops_agent"
	enabledReceiversInstrumentName = "ops_agent_enabled_receivers"
	featureTrackingInstrumentName  = "ops_agent_feature_tracking"
)

type EnabledReceivers struct {
	MetricsReceiverCountsByType map[string]int
	LogsReceiverCountsByType    map[string]int
}

var (
	mu                      sync.RWMutex
	currentEnabledReceivers EnabledReceivers
	currentFeatures         []confgenerator.Feature
)

func CountEnabledReceivers(ctx context.Context, uc *confgenerator.UnifiedConfig) (EnabledReceivers, error) {
	eR := EnabledReceivers{
		MetricsReceiverCountsByType: make(map[string]int),
		LogsReceiverCountsByType:    make(map[string]int),
	}
	pipelines, err := uc.Pipelines(ctx)
	if err != nil {
		return eR, err
	}
	for _, p := range pipelines {
		pipelineType, receiverType := p.Types()
		if pipelineType == "metrics" {
			eR.MetricsReceiverCountsByType[receiverType] += 1
		} else if pipelineType == "logs" {
			eR.LogsReceiverCountsByType[receiverType] += 1
		}
	}

	return eR, nil
}

// SetSelfMetrics extracts enabled_receivers and feature_tracking metrics from userUc and mergedUc
// and stores them in memory for registration with the collector's MeterProvider.
func SetSelfMetrics(ctx context.Context, userUc, mergedUc *confgenerator.UnifiedConfig) error {
	eR, err := CountEnabledReceivers(ctx, mergedUc)
	if err != nil {
		return fmt.Errorf("failed to count enabled receivers: %w", err)
	}
	features, err := confgenerator.ExtractFeatures(userUc)
	if err != nil {
		return fmt.Errorf("failed to extract features: %w", err)
	}

	mu.Lock()
	defer mu.Unlock()
	currentEnabledReceivers = eR
	currentFeatures = features
	return nil
}

// RegisterSelfMetrics registers the enabled_receivers and feature_tracking observable gauges
// on the provided OpenTelemetry MeterProvider.
func RegisterSelfMetrics(meterProvider metric.MeterProvider) error {
	if meterProvider == nil {
		return nil
	}
	meter := meterProvider.Meter(meterName)

	_, err := meter.Int64ObservableGauge(
		enabledReceiversInstrumentName,
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			mu.RLock()
			eR := currentEnabledReceivers
			mu.RUnlock()

			for _, rType := range confgenerator.GetSortedKeys(eR.MetricsReceiverCountsByType) {
				count := eR.MetricsReceiverCountsByType[rType]
				observer.Observe(int64(count),
					metric.WithAttributes(
						attribute.String("telemetry_type", "metrics"),
						attribute.String("receiver_type", rType),
					),
				)
			}
			for _, rType := range confgenerator.GetSortedKeys(eR.LogsReceiverCountsByType) {
				count := eR.LogsReceiverCountsByType[rType]
				observer.Observe(int64(count),
					metric.WithAttributes(
						attribute.String("telemetry_type", "logs"),
						attribute.String("receiver_type", rType),
					),
				)
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s gauge: %w", enabledReceiversInstrumentName, err)
	}

	_, err = meter.Int64ObservableGauge(
		featureTrackingInstrumentName,
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			mu.RLock()
			features := currentFeatures
			mu.RUnlock()

			for _, f := range features {
				observer.Observe(1,
					metric.WithAttributes(
						attribute.String("module", f.Module),
						attribute.String("feature", fmt.Sprintf("%s:%s", f.Kind, f.Type)),
						attribute.String("key", strings.Join(f.Key, ".")),
						attribute.String("value", f.Value),
					),
				)
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to register %s gauge: %w", featureTrackingInstrumentName, err)
	}

	return nil
}
