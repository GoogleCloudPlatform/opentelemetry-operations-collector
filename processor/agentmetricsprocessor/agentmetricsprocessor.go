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

	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/consumer/pdata"
	"go.uber.org/zap"
)

// matches the string after the last "." (or the whole string if no ".")
var metricPostfixRegex = regexp.MustCompile(`([^.]*$)`)

type agentMetricsProcessor struct {
	logger *zap.Logger

	mutex             sync.Mutex
	prevCPUTimeValues map[string]float64
}

func newAgentMetricsProcessor(logger *zap.Logger) *agentMetricsProcessor {
	return &agentMetricsProcessor{logger: logger}
}

// ProcessMetrics implements the MProcessor interface.
func (mtp *agentMetricsProcessor) ProcessMetrics(ctx context.Context, metrics pdata.Metrics) (pdata.Metrics, error) {
	convertNonMonotonicSumsToGauges(metrics.ResourceMetrics())

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

	if len(errors) > 0 {
		return metrics, consumererror.Combine(errors)
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
	case pdata.MetricDataTypeIntSum:
		sum := new.IntSum()
		sum.SetIsMonotonic(metric.IntSum().IsMonotonic())
		sum.SetAggregationTemporality(metric.IntSum().AggregationTemporality())
	case pdata.MetricDataTypeDoubleSum:
		sum := new.DoubleSum()
		sum.SetIsMonotonic(metric.DoubleSum().IsMonotonic())
		sum.SetAggregationTemporality(metric.DoubleSum().AggregationTemporality())
	case pdata.MetricDataTypeIntGauge:
		new.IntGauge()
	case pdata.MetricDataTypeDoubleGauge:
		new.DoubleGauge()
	}

	return new
}
