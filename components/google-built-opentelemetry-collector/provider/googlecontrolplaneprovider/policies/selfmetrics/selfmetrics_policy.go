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
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service"
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

func (p *SelfMetricsPolicy) Evaluate(ctx context.Context) (*confmap.Retrieved, error) {
	if err := collectorid.GenerateCollectorID(); err != nil {
		return nil, fmt.Errorf("failed to generate collector ID: %w", err)
	}

	collectorID := ctx.Value(contextKeyCollectorID)
	if collectorID == "" {
		return nil, ErrNoCollectorID
	}
	fleetID := ctx.Value(contextKeyFleetID)
	if fleetID == "" {
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

	return confmap.NewRetrieved(cm.ToStringMap())
}

func (p *SelfMetricsPolicy) LogsPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Retrieved, error) {
	panic("unimplemented")
}

func (p *SelfMetricsPolicy) MetricsPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Retrieved, error) {
	panic("unimplemented")
}

func (p *SelfMetricsPolicy) TracesPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Retrieved, error) {
	panic("unimplemented")
}

func (p *SelfMetricsPolicy) ContextSetup(ctx context.Context, collectorID, fleetID string) context.Context {
	ctx = context.WithValue(ctx, contextKeyCollectorID, collectorID)
	ctx = context.WithValue(ctx, contextKeyFleetID, fleetID)
	return ctx
}
