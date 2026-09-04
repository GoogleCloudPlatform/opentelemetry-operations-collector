package gcpdestination

import (
	"context"
	"errors"
	"fmt"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/googleclientauthextension"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configauth"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor/queuebatchprocessor"
)

func init() {
	googlepolicy.RegisterPolicyDriver(PolicyType, &googlepolicy.GenericDriver[*GCPDestinationPolicy]{})
}

const PolicyType = "gcp_destination"

type GCPDestinationPolicy struct {
	Name           string `mapstructure:"name"`
	ProjectID      string `mapstructure:"project_id"`
	UniverseDomain string `mapstructure:"universe_domain"`
	AuthFile       string `mapstructure:"auth_file"`

	exporterIDs           []component.ID `mapstructure:"-"`
	extensionIDs          []component.ID `mapstructure:"-"`
	preprocessorMetricIDs []component.ID `mapstructure:"-"`
	preprocessorLogIDs    []component.ID `mapstructure:"-"`
	preprocessorTraceIDs  []component.ID `mapstructure:"-"`
}

var _ googlepolicy.DestinationPolicy = (*GCPDestinationPolicy)(nil)

func (p *GCPDestinationPolicy) PolicyName() string {
	return p.Name
}

func (p *GCPDestinationPolicy) PolicyType() string {
	return PolicyType
}

func (p *GCPDestinationPolicy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassDestination
}

func (p *GCPDestinationPolicy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	conf := &otelcol.Config{}
	authenticator := googleclientauthextension.NewFactory().CreateDefaultConfig().(*googleclientauthextension.Config)
	authenticator.Config.Project = p.ProjectID
	authType, _ := component.NewType("googleclientauth")
	authID := component.NewIDWithName(authType, p.Name)

	conf.Extensions = map[component.ID]component.Config{
		authID: component.Config(authenticator),
	}

	otlpExporter := &otlpexporter.Config{
		ClientConfig: configgrpc.ClientConfig{
			Endpoint: "telemetry.googleapis.com:443",
			Auth: configoptional.Some(configauth.Config{
				AuthenticatorID: authID,
			}),
		},
	}
	otlpExporterType, _ := component.NewType("otlp_grpc")
	otlpExporterID := component.NewIDWithName(otlpExporterType, p.Name)

	conf.Exporters = map[component.ID]component.Config{
		otlpExporterID: component.Config(otlpExporter),
	}

	queueBatchType, _ := component.NewType("queue_batch")

	queueBatchLog := queuebatchprocessor.NewFactory().CreateDefaultConfig().(*queuebatchprocessor.Config)
	batchSubconfig := queueBatchLog.Batch.GetOrInsertDefault()
	batchSubconfig.MaxSize = 8192
	batchSubconfig.MinSize = 8192
	queueBatchLogsID := component.NewIDWithName(queueBatchType, fmt.Sprintf("%s_batch_logs", p.Name))

	queueBatchMetric := queuebatchprocessor.NewFactory().CreateDefaultConfig().(*queuebatchprocessor.Config)
	batchSubconfig = queueBatchMetric.Batch.GetOrInsertDefault()
	batchSubconfig.MaxSize = 200
	batchSubconfig.MinSize = 200
	queueBatchMetricsID := component.NewIDWithName(queueBatchType, fmt.Sprintf("%s_batch_metrics", p.Name))

	queueBatchTrace := queuebatchprocessor.NewFactory().CreateDefaultConfig().(*queuebatchprocessor.Config)
	batchSubconfig = queueBatchTrace.Batch.GetOrInsertDefault()
	batchSubconfig.MaxSize = 25000
	batchSubconfig.MinSize = 25000
	queueBatchTracesID := component.NewIDWithName(queueBatchType, fmt.Sprintf("%s_batch_traces", p.Name))

	conf.Processors = map[component.ID]component.Config{
		queueBatchLogsID:    component.Config(queueBatchLog),
		queueBatchMetricsID: component.Config(queueBatchMetric),
		queueBatchTracesID:  component.Config(queueBatchTrace),
	}

	p.extensionIDs = []component.ID{authID}
	p.exporterIDs = []component.ID{otlpExporterID}
	p.preprocessorLogIDs = []component.ID{queueBatchLogsID}
	p.preprocessorMetricIDs = []component.ID{queueBatchMetricsID}
	p.preprocessorTraceIDs = []component.ID{queueBatchTracesID}

	cm := confmap.New()
	if err := cm.Marshal(conf); err != nil {
		return nil, fmt.Errorf("policy implementation failure for %s: marshaling config got error '%w'", p.PolicyName(), err)
	}
	return cm, nil
}

func (p *GCPDestinationPolicy) Validate() error {
	if p.Name == "" {
		return errors.New("policy must be named")
	}
	return nil
}

func (p *GCPDestinationPolicy) ExporterIDs() []component.ID {
	return p.exporterIDs
}

func (p *GCPDestinationPolicy) ExtensionIDs() []component.ID {
	return p.extensionIDs
}

func (p *GCPDestinationPolicy) PreProcessMetricIDs() []component.ID {
	return p.preprocessorMetricIDs
}

func (p *GCPDestinationPolicy) PreProcessLogIDs() []component.ID {
	return p.preprocessorLogIDs
}

func (p *GCPDestinationPolicy) PreProcessTraceIDs() []component.ID {
	return p.preprocessorTraceIDs
}
