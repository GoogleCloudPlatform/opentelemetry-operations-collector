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

package gcpdestination

import (
	"context"
	"errors"
	"fmt"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/googleclientauthextension"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configauth"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor/queuebatchprocessor"
	"go.opentelemetry.io/collector/service/extensions"
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

	// Declaring the extension above only defines it; the collector instantiates
	// nothing that is not also listed under service::extensions. Without this
	// the exporter's authenticator reference below cannot be resolved and
	// startup fails with "authenticator not found".
	conf.Service.Extensions = extensions.Config{authID}

	// Same reasoning as the queue_batch processors below: start from the
	// factory default and override only the endpoint and auth. A bare
	// &otlpexporter.Config{} literal silently discarded every default the
	// factory supplies -- it disabled retry_on_failure, dropped the sending
	// queue entirely, turned off gzip compression and left timeout at 0s.
	otlpExporter := otlpexporter.NewFactory().CreateDefaultConfig().(*otlpexporter.Config)
	otlpExporter.ClientConfig.Endpoint = "telemetry.googleapis.com:443"
	otlpExporter.ClientConfig.Auth = configoptional.Some(configauth.Config{
		AuthenticatorID: authID,
	})

	// Every pipeline feeding this exporter goes through a queuebatch processor
	// below, which already provides the queue and the batching. Leaving the
	// exporter's own sending queue enabled would stack a second, redundant
	// buffer in front of the same export path.
	//
	// Setting None here is only half the job: a None Optional marshals to nil,
	// the nil key is then dropped from the generated config, and a config that
	// simply omits `sending_queue` gets the factory default (queue enabled)
	// back when the collector loads it. The disable has to be written out
	// explicitly, which is done after marshaling below.
	otlpExporter.QueueConfig = configoptional.None[exporterhelper.QueueBatchConfig]()

	otlpExporterType, _ := component.NewType("otlp_grpc")
	otlpExporterID := component.NewIDWithName(otlpExporterType, p.Name)

	conf.Exporters = map[component.ID]component.Config{
		otlpExporterID: component.Config(otlpExporter),
	}

	// Taken from the factory rather than spelled out, so the generated config
	// cannot drift from the type the processor actually registers under. A
	// hardcoded "queue_batch" here silently produced a config that failed to
	// unmarshal once the component settled on "queuebatch".
	queueBatchType := queuebatchprocessor.NewFactory().Type()

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

	// See the QueueConfig comment above. configoptional reads `enabled` on
	// unmarshal and turns the section into None, but it never writes that key
	// on marshal, so the disable has to be added to the generated config here.
	disableSendingQueue := confmap.NewFromStringMap(map[string]any{
		"exporters": map[string]any{
			otlpExporterID.String(): map[string]any{
				"sending_queue": map[string]any{
					"enabled": false,
				},
			},
		},
	})
	if err := cm.Merge(disableSendingQueue); err != nil {
		return nil, fmt.Errorf("policy implementation failure for %s: disabling exporter sending queue got error '%w'", p.PolicyName(), err)
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
