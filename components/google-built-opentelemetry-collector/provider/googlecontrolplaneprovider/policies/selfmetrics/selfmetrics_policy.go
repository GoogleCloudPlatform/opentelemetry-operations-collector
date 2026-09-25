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

package selfmetrics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/pipeline"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service"
	"go.opentelemetry.io/collector/service/extensions"
	"go.opentelemetry.io/collector/service/pipelines"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"
	config "go.opentelemetry.io/contrib/otelconf/v0.3.0"
	"go.uber.org/zap/zapcore"
)

func init() {
	googlepolicy.RegisterPolicyDriver(PolicyType, &googlepolicy.GenericDriver[*SelfMetricsPolicy]{})
}

const PolicyType = "self_metrics"

const contextKeyCollectorID = "COLLECTOR_ID"
const contextKeyFleetID = "FLEET_ID"
const contextKeyProjectID = "PROJECT_ID"

type SelfMetricsPolicy struct {
	Name string `mapstructure:"name"`
	Port int    `mapstructure:"port"`
}

var _ googlepolicy.SourcePolicy = (*SelfMetricsPolicy)(nil)

var (
	ErrNoCollectorID = errors.New("no collector ID found")
	ErrNoFleetID     = errors.New("no fleet ID found")
)

func (p *SelfMetricsPolicy) PolicyName() string {
	return p.Name
}

func (p *SelfMetricsPolicy) PolicyType() string {
	return PolicyType
}

func (p *SelfMetricsPolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}

// Validate checks the fields Evaluate depends on. LoadPolicy calls this for
// every policy that comes through the registry, including ones sent by a
// control plane, so it must report an error rather than panic: a panic here
// takes down the whole collector process on nothing more than a malformed
// remote policy.
func (p *SelfMetricsPolicy) Validate() error {
	if p.Name == "" {
		return errors.New("policy must be named")
	}
	// Evaluate builds a receiver endpoint and the matching telemetry exporter
	// endpoint from this port. Port 0 would bind an arbitrary free port that
	// the exporter's "localhost:0" endpoint could never reach.
	if p.Port < 1 || p.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", p.Port)
	}
	return nil
}

func (p *SelfMetricsPolicy) Evaluate(ctx context.Context) (*confmap.Conf, error) {
	collectorID := ctx.Value(contextKeyCollectorID)
	if collectorID == nil || collectorID == "" {
		return nil, ErrNoCollectorID
	}
	fleetID := ctx.Value(contextKeyFleetID)
	if fleetID == nil || fleetID == "" {
		return nil, ErrNoFleetID
	}

	attrs := []config.AttributeNameValue{
		{
			Name:  "service.instance.id",
			Value: collectorID,
		},
		{
			Name:  "gcp.fleet_id",
			Value: fleetID,
		},
	}
	if v := ctx.Value(contextKeyProjectID); v != nil && v != "" {
		attrs = append(attrs, config.AttributeNameValue{
			Name:  "gcp.project_id",
			Value: v,
		})
	}

	conf := &otelcol.Config{}

	endpoint := fmt.Sprintf("http://localhost:%d", p.Port)
	protocol := "grpc"
	insecure := true
	interval := 5000
	timeout := 30000
	// Start from the telemetry factory's default config and override only what
	// this policy actually cares about. Building otelconftelemetry.Config as a
	// literal silently drops every default the factory supplies -- most
	// importantly Logs.Encoding, whose zero value makes the collector fail at
	// startup with "failed to create logger: no encoder name specified", but
	// also log sampling and the stderr output paths.
	telemetryCfg := otelconftelemetry.NewFactory().CreateDefaultConfig().(*otelconftelemetry.Config)

	telemetryCfg.Resource = otelconftelemetry.ResourceConfig{
		Resource: config.Resource{
			Attributes: attrs,
		},
	}

	telemetryCfg.Logs.Level = zapcore.InfoLevel
	telemetryCfg.Logs.Processors = []config.LogRecordProcessor{
		{
			Batch: &config.BatchLogRecordProcessor{
				Exporter: config.LogRecordExporter{
					OTLP: &config.OTLP{
						Endpoint: &endpoint,
						Protocol: &protocol,
						Insecure: &insecure,
					},
				},
			},
		},
	}

	telemetryCfg.Metrics.Level = configtelemetry.LevelNormal
	// Replaces the factory's default Prometheus pull reader on :8888, which
	// this collector does not expose.
	telemetryCfg.Metrics.MeterProvider = config.MeterProvider{
		Readers: []config.MetricReader{
			{
				Periodic: &config.PeriodicMetricReader{
					Interval: &interval,
					Timeout:  &timeout,
					Exporter: config.PushMetricExporter{
						OTLP: &config.OTLPMetric{
							Endpoint: &endpoint,
							Protocol: &protocol,
							Insecure: &insecure,
						},
					},
				},
			},
		},
	}

	conf.Service = service.Config{
		Telemetry: telemetryCfg,
	}

	// The control plane extension reports policy set state on the collector's
	// internal MeterProvider, which the periodic reader configured above pushes
	// into this policy's own OTLP receiver. It is enabled here rather than in
	// the distribution's static config because it only reports anything
	// meaningful when the control plane provider is driving the collector,
	// which is exactly when this policy is evaluated.
	//
	// Configured as a raw map for the same reason resourcedetection and
	// transform below are: it keeps the provider from taking a module
	// dependency on the extension. The extension's Config is intentionally
	// empty, since policy set identity is derived from the active policy set
	// rather than configured.
	//
	// The ID is deliberately unnamed, unlike googleclientauth/<policy> in the
	// destination policy. The extension reads process wide state, has no
	// per-policy config and is not referenced by ID from any pipeline, so a
	// single googlecontrolplane instance per collector is the right shape, the
	// same way the googlepolicy processor is registered.
	cpExtType, _ := component.NewType("googlecontrolplane")
	cpExtID := component.NewID(cpExtType)

	conf.Extensions = map[component.ID]component.Config{
		cpExtID: component.Config(map[string]any{}),
	}

	// Declaring the extension above only defines it; the collector instantiates
	// nothing that is not also listed under service::extensions.
	conf.Service.Extensions = extensions.Config{cpExtID}

	otlpReceiver := otlpreceiver.NewFactory().CreateDefaultConfig().(*otlpreceiver.Config)
	otlpReceiver.Protocols.HTTP = configoptional.None[otlpreceiver.HTTPConfig]()
	grpcConfig := otlpReceiver.Protocols.GRPC.GetOrInsertDefault()
	grpcConfig.NetAddr.Endpoint = fmt.Sprintf("localhost:%d", p.Port)

	otlpReceiverType, _ := component.NewType("otlp")
	otlpReceiverID := component.NewIDWithName(otlpReceiverType, p.Name)

	conf.Receivers = map[component.ID]component.Config{
		otlpReceiverID: component.Config(otlpReceiver),
	}

	rdType, _ := component.NewType("resourcedetection")
	rdID := component.NewIDWithName(rdType, p.Name)
	rdCfg := map[string]any{
		"detectors": []string{"gcp"},
		"override":  false,
		"timeout":   10 * time.Second,
	}

	transformType, _ := component.NewType("transform")
	transformID := component.NewIDWithName(transformType, p.Name)
	transformCfg := map[string]any{
		"error_mode": "ignore",
		"log_statements": []map[string]any{
			{
				"context": "resource",
				"statements": []string{
					`set(attributes["gcp.project_id"], attributes["cloud.account.id"]) where attributes["gcp.project_id"] == nil and attributes["cloud.account.id"] != nil`,
				},
			},
		},
		"metric_statements": []map[string]any{
			{
				"context": "resource",
				"statements": []string{
					`set(attributes["gcp.project_id"], attributes["cloud.account.id"]) where attributes["gcp.project_id"] == nil and attributes["cloud.account.id"] != nil`,
				},
			},
		},
	}

	conf.Processors = map[component.ID]component.Config{
		rdID:        component.Config(rdCfg),
		transformID: component.Config(transformCfg),
	}

	cm := confmap.New()
	if err := cm.Marshal(conf); err != nil {
		return nil, fmt.Errorf("policy implementation failure for %s: marshaling config got error '%w'", p.PolicyName(), err)
	}

	return cm, nil
}

func (p *SelfMetricsPolicy) LogsPipelines(preExportProcessors []component.ID, exporters []component.ID, _ []component.ID) (*confmap.Conf, error) {
	return p.createPipeline(pipeline.SignalLogs, preExportProcessors, exporters)
}

func (p *SelfMetricsPolicy) MetricsPipelines(preExportProcessors []component.ID, exporters []component.ID, _ []component.ID) (*confmap.Conf, error) {
	return p.createPipeline(pipeline.SignalMetrics, preExportProcessors, exporters)
}

func (p *SelfMetricsPolicy) TracesPipelines(_ []component.ID, _ []component.ID, _ []component.ID) (*confmap.Conf, error) {
	return nil, nil
}

func (p *SelfMetricsPolicy) createPipeline(signal pipeline.Signal, preExportProcessors []component.ID, exporters []component.ID) (*confmap.Conf, error) {
	otlpReceiverType, _ := component.NewType("otlp")
	otlpReceiverID := component.NewIDWithName(otlpReceiverType, p.Name)

	rdType, _ := component.NewType("resourcedetection")
	rdID := component.NewIDWithName(rdType, p.Name)

	transformType, _ := component.NewType("transform")
	transformID := component.NewIDWithName(transformType, p.Name)

	processors := append([]component.ID{rdID, transformID}, preExportProcessors...)

	pipeID := pipeline.NewIDWithName(signal, p.Name)
	conf := &otelcol.Config{
		Service: service.Config{
			Pipelines: pipelines.Config{
				pipeID: &pipelines.PipelineConfig{
					Receivers:  []component.ID{otlpReceiverID},
					Processors: processors,
					Exporters:  exporters,
				},
			},
		},
	}
	cm := confmap.New()
	if err := cm.Marshal(conf); err != nil {
		return nil, fmt.Errorf("policy implementation failure for %s: marshaling %s pipeline got error '%w'", p.PolicyName(), signal, err)
	}
	return cm, nil
}

func (p *SelfMetricsPolicy) ContextSetup(ctx context.Context, collectorID, fleetID string) context.Context {
	ctx = context.WithValue(ctx, contextKeyCollectorID, collectorID)
	ctx = context.WithValue(ctx, contextKeyFleetID, fleetID)
	return ctx
}
