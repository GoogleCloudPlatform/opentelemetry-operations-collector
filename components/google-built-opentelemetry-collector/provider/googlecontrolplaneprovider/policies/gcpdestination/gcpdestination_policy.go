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

	// The endpoint is a single service behind Google's front ends, so the
	// default round_robin balancer buys nothing and, on IPv4-only hosts,
	// keeps retrying the AAAA record and logging a gRPC warning on every
	// attempt. pick_first sticks to the one address that works.
	otlpExporter.ClientConfig.BalancerName = "pick_first"

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

	const (
		stage1QueueSizeBytes = 32 * 1024 * 1024    // 32 MiB (33,554,432 B)
		stage1MinBatchBytes  = 7 * 1024 * 1024 / 2 // 3.5 MiB (3,670,016 B)
		stage1MaxBatchBytes  = 4 * 1024 * 1024     // 4 MiB (4,194,304 B), leaving 1 MiB headroom below UTR's 5 MiB limit
		utrMaxLogItems       = 8192                // UTR kMaxLogCountPerBatch
		utrMaxMetricItems    = 200                 // UTR kMaxPointsPerRequest
		utrMaxTraceItems     = 8192                // UTR kMaxSpanCountPerBatch (#710)
	)

	newStage1ByteBatchConfig := func() *queuebatchprocessor.Config {
		cfg := queuebatchprocessor.NewFactory().CreateDefaultConfig().(*queuebatchprocessor.Config)
		cfg.Sizer = exporterhelper.RequestSizerTypeBytes
		cfg.QueueSize = stage1QueueSizeBytes
		batchCfg := cfg.Batch.GetOrInsertDefault()
		batchCfg.Sizer = exporterhelper.RequestSizerTypeBytes
		batchCfg.MinSize = stage1MinBatchBytes
		batchCfg.MaxSize = stage1MaxBatchBytes
		return cfg
	}

	newStage2ItemSplitConfig := func(maxItems int64) *queuebatchprocessor.Config {
		cfg := queuebatchprocessor.NewFactory().CreateDefaultConfig().(*queuebatchprocessor.Config)
		cfg.Sizer = exporterhelper.RequestSizerTypeRequests
		cfg.QueueSize = 10
		batchCfg := cfg.Batch.GetOrInsertDefault()
		batchCfg.Sizer = exporterhelper.RequestSizerTypeItems
		batchCfg.MinSize = 0
		batchCfg.MaxSize = maxItems
		return cfg
	}

	queueBatchLogsID := component.NewIDWithName(queueBatchType, fmt.Sprintf("%s_batch_logs", p.Name))
	queueSplitLogsID := component.NewIDWithName(queueBatchType, fmt.Sprintf("%s_split_logs", p.Name))

	queueBatchMetricsID := component.NewIDWithName(queueBatchType, fmt.Sprintf("%s_batch_metrics", p.Name))
	queueSplitMetricsID := component.NewIDWithName(queueBatchType, fmt.Sprintf("%s_split_metrics", p.Name))

	queueBatchTracesID := component.NewIDWithName(queueBatchType, fmt.Sprintf("%s_batch_traces", p.Name))
	queueSplitTracesID := component.NewIDWithName(queueBatchType, fmt.Sprintf("%s_split_traces", p.Name))

	queueBatchLog := newStage1ByteBatchConfig()
	queueBatchLog.StorageID = &googlepolicy.StorageExtensionID
	queueBatchLog.NumConsumers = 10
	queueBatchLog.BlockOnOverflow = true

	queueBatchMetric := newStage1ByteBatchConfig()
	queueBatchTrace := newStage1ByteBatchConfig()

	queueSplitLog := newStage2ItemSplitConfig(utrMaxLogItems)
	queueSplitMetric := newStage2ItemSplitConfig(utrMaxMetricItems)
	queueSplitTrace := newStage2ItemSplitConfig(utrMaxTraceItems)

	conf.Processors = map[component.ID]component.Config{
		queueBatchLogsID:    component.Config(queueBatchLog),
		queueSplitLogsID:    component.Config(queueSplitLog),
		queueBatchMetricsID: component.Config(queueBatchMetric),
		queueSplitMetricsID: component.Config(queueSplitMetric),
		queueBatchTracesID:  component.Config(queueBatchTrace),
		queueSplitTracesID:  component.Config(queueSplitTrace),
	}

	p.extensionIDs = []component.ID{authID}
	p.exporterIDs = []component.ID{otlpExporterID}
	p.preprocessorLogIDs = []component.ID{queueBatchLogsID, queueSplitLogsID}
	p.preprocessorMetricIDs = []component.ID{queueBatchMetricsID, queueSplitMetricsID}
	p.preprocessorTraceIDs = []component.ID{queueBatchTracesID, queueSplitTracesID}

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

	// confmap marshals the queue and batch sizers as empty maps: SizerType
	// implements TextMarshaler on a pointer receiver and the marshaled structs
	// are not addressable, so the collector would fall back to the factory
	// default sizer on load. Write the sizer names explicitly.
	for id, cfg := range map[component.ID]*queuebatchprocessor.Config{
		queueBatchLogsID:    queueBatchLog,
		queueSplitLogsID:    queueSplitLog,
		queueBatchMetricsID: queueBatchMetric,
		queueSplitMetricsID: queueSplitMetric,
		queueBatchTracesID:  queueBatchTrace,
		queueSplitTracesID:  queueSplitTrace,
	} {
		sizers, err := queueBatchSizers(cfg)
		if err != nil {
			return nil, fmt.Errorf("policy implementation failure for %s: marshaling sizers of %s got error '%w'", p.PolicyName(), id, err)
		}
		if err := cm.Merge(confmap.NewFromStringMap(map[string]any{
			"processors": map[string]any{id.String(): sizers},
		})); err != nil {
			return nil, fmt.Errorf("policy implementation failure for %s: setting sizers of %s got error '%w'", p.PolicyName(), id, err)
		}
	}

	return cm, nil
}

// queueBatchSizers returns the queue and batch sizer names of cfg in the shape
// of the generated processor config.
func queueBatchSizers(cfg *queuebatchprocessor.Config) (map[string]any, error) {
	queueSizer, err := cfg.Sizer.MarshalText()
	if err != nil {
		return nil, err
	}
	sizers := map[string]any{"sizer": string(queueSizer)}
	if cfg.Batch.HasValue() {
		batchSizer, err := cfg.Batch.Get().Sizer.MarshalText()
		if err != nil {
			return nil, err
		}
		sizers["batch"] = map[string]any{"sizer": string(batchSizer)}
	}
	return sizers, nil
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
