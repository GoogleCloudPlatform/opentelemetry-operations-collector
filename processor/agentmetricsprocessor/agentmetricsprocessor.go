// Copyright 2020 Google LLC
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

package agentmetricsprocessor

import (
	"context"
	"regexp"
	"sync"

	"go.opentelemetry.io/collector/model/pdata"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/multierr"
	"go.uber.org/zap"
)

// matches the string after the last "." (or the whole string if no ".")
var metricPostfixRegex = regexp.MustCompile(`([^.]*$)`)

type opKey struct {
	device, direction string
}

type opData struct {
	operations pdata.NumberDataPoint
	time       pdata.NumberDataPoint
	cumAvgTime float64
}

type agentMetricsProcessor struct {
	logger *zap.Logger
	cfg    *Config

	mutex             sync.Mutex
	prevCPUTimeValues map[string]float64
	prevOp            map[opKey]opData
}

func newAgentMetricsProcessor(logger *zap.Logger, cfg *Config) *agentMetricsProcessor {
	return &agentMetricsProcessor{
		logger: logger,
		cfg:    cfg,
		prevOp: make(map[opKey]opData),
	}
}

// ProcessMetrics implements the MProcessor interface.
func (mtp *agentMetricsProcessor) ProcessMetrics(ctx context.Context, metrics pdata.Metrics) (pdata.Metrics, error) {
	convertNonMonotonicSumsToGauges(metrics.ResourceMetrics())
	removeVersionAttribute(metrics.ResourceMetrics())

	var errors []error
	if err := combineProcessMetrics(metrics.ResourceMetrics()); err != nil {
		errors = append(errors, err)
	}

	if err := splitReadWriteBytesMetrics(metrics.ResourceMetrics()); err != nil {
		errors = append(errors, err)
	}

	if err := mtp.appendUtilizationMetrics(metrics.ResourceMetrics()); err != nil {
		errors = append(errors, err)
	}

	if err := cleanCPUNumber(metrics.ResourceMetrics()); err != nil {
		errors = append(errors, err)
	}

	if err := mtp.appendAverageDiskMetrics(metrics.ResourceMetrics()); err != nil {
		errors = append(errors, err)
	}

	// Add blank labels last so they can also be applied to metrics added by agentmetricsprocessor.
	if err := mtp.addBlankLabel(metrics.ResourceMetrics()); err != nil {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return metrics, multierr.Combine(errors...)
	}

	return metrics, nil
}

// newMetric creates a new metric with no data points using the provided descriptor info
func newMetric(metric pdata.Metric) pdata.Metric {
	return newMetricWithName(metric, "")
}

// newMetric creates a new metric with no data points using the provided descriptor info
// and overrides the name with the supplied value
func newMetricWithName(metric pdata.Metric, name string) pdata.Metric {
	new := pdata.NewMetric()

	if name != "" {
		new.SetName(name)
	} else {
		new.SetName(metric.Name())
	}

	new.SetDescription(metric.Description())
	new.SetUnit(metric.Unit())
	new.SetDataType(metric.DataType())

	switch t := metric.DataType(); t {
	case pmetric.MetricDataTypeSum:
		sum := new.Sum()
		sum.SetIsMonotonic(metric.Sum().IsMonotonic())
		sum.SetAggregationTemporality(metric.Sum().AggregationTemporality())
	case pmetric.MetricDataTypeGauge:
		new.Gauge()
	}

	return new
}
