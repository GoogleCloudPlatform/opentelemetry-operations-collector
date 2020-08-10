// Copyright 2020, Google Inc.
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
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/consumer/pdata"
	"go.opentelemetry.io/collector/translator/conventions"
)

// The following code performs a translation from metrics that store process
// information as resources to metrics that store process information as labels.
//
// Starting format:
//
//    ResourceMetrics         ResourceMetrics               ResourceMetrics
// +-------------------+ +-----------------------+     +-----------------------+
// |  Resource: Empty  | |  Resource: Process 1  |     |  Resource: Process X  |
// +-------+---+-------+ +---------+---+---------+ ... +---------+---+---------+
// |Metric1|...|MetricN| |MetricN+1|...|MetricN+M|     |MetricN+1|...|MetricN+M|
// +-------+---+-------+ +---------+---+---------+     +---------+---+---------+
//
// Converted format:
//
//                             ResourceMetrics
// +---------------------------------------------------------------------+
// |                           Resource: Empty                           |
// +-------+---+-------+----------------------+---+----------------------+
// |Metric1|...|MetricN|MetricN+1{P1, ..., PX}|...|MetricN+M{P1, ..., PX}|
// +-------+---+-------+----------------------+---+----------------------+
//
// Assumptions:
// * There is at most one resource metrics slice without process resource info (will raise error if not)
// * There is no other resource info supplied that needs to be retained (info may be silently lost if it exists)

func combineProcessMetrics(rms pdata.ResourceMetricsSlice) error {
	resultMetrics := pdata.NewResourceMetrics()
	resultMetrics.InitEmpty()
	ilms := resultMetrics.InstrumentationLibraryMetrics()
	ilms.Resize(1)
	ilms.At(0).InitEmpty()

	// create collection of combined process metrics, disregarding any ResourceMetrics
	// with no process resource attributes as "otherMetrics"
	processMetrics, otherMetrics, err := createProcessMetrics(rms)
	if err != nil {
		return err
	}

	// if non-process specific metrics were supplied, initialize the result
	// with those metrics
	if !otherMetrics.IsNil() {
		resultMetrics = otherMetrics
	}

	// append all of the process metrics
	metrics := resultMetrics.InstrumentationLibraryMetrics().At(0).Metrics()
	// ideally, we would Resize & Set, but a Set function is not available
	// at this time
	for _, metric := range processMetrics {
		metrics.Append(&metric.Metric)
	}

	// TODO: This is super inefficient. Instead, we should just return a new
	// data.MetricData struct, but currently blocked as it is internal
	rms.Resize(1)
	resultMetrics.CopyTo(rms.At(0))
	return nil
}

func createProcessMetrics(rms pdata.ResourceMetricsSlice) (processMetrics convertedMetrics, otherMetrics pdata.ResourceMetrics, err error) {
	processMetrics = convertedMetrics{}
	otherMetrics = pdata.NewResourceMetrics()

	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		resource := rm.Resource()

		// if these ResourceMetrics do not contain process resource attributes,
		// these must be the "other" non-process metrics
		if !includesProcessAttributes(resource) {
			if !otherMetrics.IsNil() {
				err = errors.New("unexpectedly received multiple Resource Metrics without process attributes")
				return
			}

			otherMetrics = rm
			continue
		}

		// combine all metrics into the process metrics map by appending
		// the data points
		ilms := rm.InstrumentationLibraryMetrics()
		for j := 0; j < ilms.Len(); j++ {
			metrics := ilms.At(j).Metrics()
			for k := 0; k < metrics.Len(); k++ {
				err = processMetrics.append(metrics.At(k), resource)
				if err != nil {
					return
				}
			}
		}
	}

	return
}

const processAttributePrefix = "process."

// includesProcessAttributes returns true if the resource includes
// any attributes with a "process." prefix
func includesProcessAttributes(resource pdata.Resource) bool {
	if resource.IsNil() {
		return false
	}

	includesProcessAttributes := false
	resource.Attributes().ForEach(func(k string, _ pdata.AttributeValue) {
		if strings.HasPrefix(k, processAttributePrefix) {
			includesProcessAttributes = true
		}
	})
	return includesProcessAttributes
}

// convertedMetrics stores a map of metric names to converted metrics
// where convertedMetrics have process information stored as labels.
type convertedMetrics map[string]*convertedMetric

// append appends the data points associated with the provided metric to the
// associated converted metric (creating this metric if it doesn't exist yet),
// and appends the provided resource attributes as labels against these
// data points.
func (cms convertedMetrics) append(metric pdata.Metric, resource pdata.Resource) error {
	cm := cms.getOrCreate(metric)
	return cm.append(metric, resource)
}

// getOrCreate returns the converted metric associated with a given metric
// name (creating this metric if it doesn't exist yet).
func (cms convertedMetrics) getOrCreate(metric pdata.Metric) *convertedMetric {
	// if we have an existing converted metric, return this
	metricName := metric.MetricDescriptor().Name()
	if cm, ok := cms[metricName]; ok {
		return cm
	}

	// if there is no existing converted metric, create one using the
	// descriptor from the provided metric
	cm := newConvertedMetric(metric.MetricDescriptor())
	cms[metricName] = cm
	return cm
}

// convertedMetric is a pdata.Metric with process information stored as labels.
type convertedMetric struct {
	pdata.Metric
}

// newConvertedMetric creates a new convertedMetric with no data points
// using the provided descriptor
func newConvertedMetric(descriptor pdata.MetricDescriptor) *convertedMetric {
	cm := &convertedMetric{pdata.NewMetric()}
	cm.InitEmpty()
	descriptor.CopyTo(cm.MetricDescriptor())
	return cm
}

// append appends the data points associated with the provided metric to the
// converted metric and appends the provided resource attributes as labels
// against these data points.
func (cm convertedMetric) append(metric pdata.Metric, resource pdata.Resource) error {
	// int64 data points
	idps := metric.Int64DataPoints()
	for i := 0; i < idps.Len(); i++ {
		err := appendAttributesToLabels(idps.At(i).LabelsMap(), resource.Attributes())
		if err != nil {
			return err
		}
	}
	idps.MoveAndAppendTo(cm.Int64DataPoints())

	// double data points
	ddps := metric.DoubleDataPoints()
	for i := 0; i < ddps.Len(); i++ {
		err := appendAttributesToLabels(ddps.At(i).LabelsMap(), resource.Attributes())
		if err != nil {
			return err
		}
	}
	ddps.MoveAndAppendTo(cm.DoubleDataPoints())

	return nil
}

// appendAttributesToLabels appends the provided attributes to the provided labels map.
// This requires converting the attributes to string format.
func appendAttributesToLabels(labels pdata.StringMap, attributes pdata.AttributeMap) error {
	var err error
	attributes.ForEach(func(k string, v pdata.AttributeValue) {
		// break if error has occurred in previous iteration
		if err != nil {
			return
		}

		key := toCloudMonitoringLabel(k)
		// ignore attributes that do not map to a cloud ops label
		if key == "" {
			return
		}

		var value string
		value, err = stringValue(v)
		// break if error
		if err != nil {
			return
		}

		labels.Insert(key, value)
	})
	return err
}

func toCloudMonitoringLabel(resourceAttributeKey string) string {
	// see https://cloud.google.com/monitoring/api/metrics_agent#agent-processes
	switch resourceAttributeKey {
	case conventions.AttributeProcessID:
		return "pid"
	case conventions.AttributeProcessExecutableName:
		return "command"
	case conventions.AttributeProcessCommandLine:
		return "command_line"
	case conventions.AttributeProcessUsername:
		return "owner"
	default:
		return ""
	}
}

func stringValue(attributeValue pdata.AttributeValue) (string, error) {
	var stringValue string
	switch t := attributeValue.Type(); t {
	case pdata.AttributeValueBOOL:
		stringValue = strconv.FormatBool(attributeValue.BoolVal())
	case pdata.AttributeValueINT:
		stringValue = strconv.FormatInt(attributeValue.IntVal(), 10)
	case pdata.AttributeValueDOUBLE:
		stringValue = strconv.FormatFloat(attributeValue.DoubleVal(), 'f', -1, 64)
	case pdata.AttributeValueSTRING:
		stringValue = attributeValue.StringVal()
	default:
		return "", fmt.Errorf("unexpected attribute type: %v", t)
	}

	// cloud operations has a maximum label value length of 1024
	if len(stringValue) > 1024 {
		return stringValue[:1024], nil
	}

	return stringValue, nil
}
