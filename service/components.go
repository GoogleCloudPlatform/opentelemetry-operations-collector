// Copyright 2020 Google LLC
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

package service

import (
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/mongodbreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/fileexporter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/googlecloudexporter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/googlemanagedprometheusexporter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/groupbyattrsprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/metricstransformprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourcedetectionprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/resourceprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/activedirectorydsreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/aerospikereceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/apachereceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/couchdbreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/elasticsearchreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/flinkmetricsreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/iisreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/jmxreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/memcachedreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/mysqlreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/nginxreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusexecreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/rabbitmqreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/redisreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/saphanareceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/sqlqueryreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/sqlserverreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/windowsperfcountersreceiver"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/zookeeperreceiver"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/loggingexporter"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/exporter/otlphttpexporter"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/extension/ballastextension"
	"go.opentelemetry.io/collector/extension/zpagesextension"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/processor/memorylimiterprocessor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.uber.org/multierr"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/processor/agentmetricsprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/processor/casttosumprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/processor/normalizesumsprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/nvmlreceiver"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/varnishreceiver"
)

func components() (component.Factories, error) {
	errs := []error{}
	factories, err := Components()
	if err != nil {
		return component.Factories{}, err
	}

	extensions := []extension.Factory{}
	for _, ext := range factories.Extensions {
		extensions = append(extensions, ext)
	}
	factories.Extensions, err = extension.MakeFactoryMap(extensions...)
	if err != nil {
		errs = append(errs, err)
	}

	receivers := []receiver.Factory{
		activedirectorydsreceiver.NewFactory(),
		aerospikereceiver.NewFactory(),
		hostmetricsreceiver.NewFactory(),
		prometheusreceiver.NewFactory(),
		prometheusexecreceiver.NewFactory(),
		nginxreceiver.NewFactory(),
		jmxreceiver.NewFactory(),
		windowsperfcountersreceiver.NewFactory(),
		iisreceiver.NewFactory(),
		sqlserverreceiver.NewFactory(),
		redisreceiver.NewFactory(),
		mysqlreceiver.NewFactory(),
		apachereceiver.NewFactory(),
		memcachedreceiver.NewFactory(),
		mongodbreceiver.NewFactory(),
		dcgmreceiver.NewFactory(),
		nvmlreceiver.NewFactory(),
		postgresqlreceiver.NewFactory(),
		elasticsearchreceiver.NewFactory(),
		flinkmetricsreceiver.NewFactory(),
		couchdbreceiver.NewFactory(),
		zookeeperreceiver.NewFactory(),
		rabbitmqreceiver.NewFactory(),
		varnishreceiver.NewFactory(),
		saphanareceiver.NewFactory(),
		sqlqueryreceiver.NewFactory(),
	}
	for _, rcv := range factories.Receivers {
		receivers = append(receivers, rcv)
	}
	factories.Receivers, err = receiver.MakeFactoryMap(receivers...)
	if err != nil {
		errs = append(errs, err)
	}

	exporters := []exporter.Factory{
		fileexporter.NewFactory(),
		googlecloudexporter.NewFactory(),
		googlemanagedprometheusexporter.NewFactory(),
	}
	for _, exp := range factories.Exporters {
		exporters = append(exporters, exp)
	}
	factories.Exporters, err = exporter.MakeFactoryMap(exporters...)
	if err != nil {
		errs = append(errs, err)
	}

	processors := []component.ProcessorFactory{
		agentmetricsprocessor.NewFactory(),
		casttosumprocessor.NewFactory(),
		filterprocessor.NewFactory(),
		normalizesumsprocessor.NewFactory(),
		metricstransformprocessor.NewFactory(),
		resourcedetectionprocessor.NewFactory(),
		resourceprocessor.NewFactory(),
		transformprocessor.NewFactory(),
		groupbyattrsprocessor.NewFactory(),
	}
	for _, pr := range factories.Processors {
		processors = append(processors, pr)
	}
	factories.Processors, err = component.MakeProcessorFactoryMap(processors...)
	if err != nil {
		errs = append(errs, err)
	}

	return factories, multierr.Combine(errs...)
}

func Components() (
	component.Factories,
	error,
) {
	var errs error

	extensions, err := extension.MakeFactoryMap(
		zpagesextension.NewFactory(),
		ballastextension.NewFactory(),
	)
	errs = multierr.Append(errs, err)

	receivers, err := receiver.MakeFactoryMap(
		otlpreceiver.NewFactory(),
	)
	errs = multierr.Append(errs, err)

	exporters, err := exporter.MakeFactoryMap(
		loggingexporter.NewFactory(),
		otlpexporter.NewFactory(),
		otlphttpexporter.NewFactory(),
	)
	errs = multierr.Append(errs, err)

	processors, err := component.MakeProcessorFactoryMap(
		batchprocessor.NewFactory(),
		memorylimiterprocessor.NewFactory(),
	)
	errs = multierr.Append(errs, err)

	factories := component.Factories{
		Extensions: extensions,
		Receivers:  receivers,
		Processors: processors,
		Exporters:  exporters,
	}

	return factories, errs
}
