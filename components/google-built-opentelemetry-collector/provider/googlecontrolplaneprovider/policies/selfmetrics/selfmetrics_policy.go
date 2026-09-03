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

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configtelemetry"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/pipeline"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service"
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

func (p *SelfMetricsPolicy) Validate() error {
	panic("unimplemented")
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

	conf := &otelcol.Config{}

	endpoint := fmt.Sprintf("http://localhost:%d", p.Port)
	protocol := "grpc"
	insecure := true
	interval := 5000
	timeout := 30000

	conf.Service = service.Config{
		Telemetry: &otelconftelemetry.Config{
			Resource: otelconftelemetry.ResourceConfig{
				Resource: config.Resource{
					Attributes: []config.AttributeNameValue{
						{
							Name:  "service.instance.id",
							Value: collectorID,
						},
						{
							Name:  "gcp.fleet_id",
							Value: fleetID,
						},
					},
				},
			},
			Logs: otelconftelemetry.LogsConfig{
				Level: zapcore.InfoLevel,
				Processors: []config.LogRecordProcessor{
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
				},
			},
			Metrics: otelconftelemetry.MetricsConfig{
				Level: configtelemetry.LevelNormal,
				MeterProvider: config.MeterProvider{
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
				},
			},
		},
	}

	otlpReceiver := otlpreceiver.NewFactory().CreateDefaultConfig().(*otlpreceiver.Config)
	otlpReceiver.Protocols.HTTP = configoptional.None[otlpreceiver.HTTPConfig]()
	grpcConfig := otlpReceiver.Protocols.GRPC.GetOrInsertDefault()
	grpcConfig.NetAddr.Endpoint = fmt.Sprintf("localhost:%d", p.Port)

	otlpReceiverType, _ := component.NewType("otlp")
	otlpReceiverID := component.NewIDWithName(otlpReceiverType, p.Name)

	conf.Receivers = map[component.ID]component.Config{
		otlpReceiverID: component.Config(otlpReceiver),
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

	pipeID := pipeline.NewIDWithName(signal, p.Name)
	conf := &otelcol.Config{
		Service: service.Config{
			Pipelines: pipelines.Config{
				pipeID: &pipelines.PipelineConfig{
					Receivers:  []component.ID{otlpReceiverID},
					Processors: preExportProcessors,
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
