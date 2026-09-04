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

func (p *GCPDestinationPolicy) Evaluate(_ context.Context) (*confmap.Retrieved, error) {
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

	if err := confmap.Validate(conf); err != nil {
		return nil, fmt.Errorf("policy implementation failure for %s: validating config got error '%w'", p.PolicyName(), err)
	}

	p.extensionIDs = append(p.extensionIDs, authID)
	p.exporterIDs = append(p.exporterIDs, otlpExporterID)
	p.preprocessorLogIDs = append(p.preprocessorLogIDs, queueBatchLogsID)
	p.preprocessorMetricIDs = append(p.preprocessorMetricIDs, queueBatchMetricsID)
	p.preprocessorTraceIDs = append(p.preprocessorTraceIDs, queueBatchTracesID)

	cm := confmap.New()
	if err := cm.Marshal(conf); err != nil {
		return nil, fmt.Errorf("policy implementation failure for %s: marshaling config got error '%w'", p.PolicyName(), err)
	}
	return confmap.NewRetrieved(cm.ToStringMap())
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
