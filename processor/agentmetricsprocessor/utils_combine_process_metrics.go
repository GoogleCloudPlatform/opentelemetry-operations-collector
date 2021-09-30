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
	"fmt"
	"strconv"
	"strings"

	"go.opentelemetry.io/collector/model/pdata"
	conventions "go.opentelemetry.io/collector/model/semconv/v1.5.0"
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
	ilms := resultMetrics.InstrumentationLibraryMetrics()
	ilms.Resize(1)
	ilms.At(0)

	// create collection of combined process metrics, disregarding any ResourceMetrics
	// with no process resource attributes as "otherMetrics"
	processMetrics, otherMetrics, err := createProcessMetrics(rms)
	if err != nil {
		return err
	}

	// if non-process specific metrics were supplied, initialize the result
	// with those metrics
	resultMetrics = otherMetrics

	// append all of the process metrics
	metrics := resultMetrics.InstrumentationLibraryMetrics().At(0).Metrics()
	// ideally, we would Resize & Set, but a Set function is not available
	// at this time
	for _, metric := range processMetrics {
		metrics.Append(metric.Metric)
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
	includesProcessAttributes := false
	resource.Attributes().Range(func(k string, _ pdata.AttributeValue) bool {
		if strings.HasPrefix(k, processAttributePrefix) {
			includesProcessAttributes = true
			return false
		}
		return true
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
	metricName := metric.Name()
	if cm, ok := cms[metricName]; ok {
		return cm
	}

	// if there is no existing converted metric, create one using the
	// descriptor info from the provided metric
	cm := &convertedMetric{newMetric(metric)}
	cms[metricName] = cm
	return cm
}

// convertedMetric is a pdata.Metric with process information stored as labels.
type convertedMetric struct {
	pdata.Metric
}

// append appends the data points associated with the provided metric to the
// converted metric and appends the provided resource attributes as labels
// against these data points.
func (cm convertedMetric) append(metric pdata.Metric, resource pdata.Resource) error {
	var err error

	switch t := metric.DataType(); t {
	case pdata.MetricDataTypeIntSum:
		err = appendIntDataSlice(metric.IntSum().DataPoints(), cm.IntSum().DataPoints(), resource)
	case pdata.MetricDataTypeIntGauge:
		err = appendIntDataSlice(metric.IntGauge().DataPoints(), cm.IntGauge().DataPoints(), resource)
	case pdata.MetricDataTypeDoubleSum:
		err = appendDoubleDataSlice(metric.DoubleSum().DataPoints(), cm.DoubleSum().DataPoints(), resource)
	case pdata.MetricDataTypeDoubleGauge:
		err = appendDoubleDataSlice(metric.DoubleGauge().DataPoints(), cm.DoubleGauge().DataPoints(), resource)
	}

	return err
}

func appendIntDataSlice(idps, converted pdata.IntDataPointSlice, resource pdata.Resource) error {
	for i := 0; i < idps.Len(); i++ {
		err := appendAttributesToLabels(idps.At(i).Attributes(), resource.Attributes())
		if err != nil {
			return err
		}
	}
	idps.MoveAndAppendTo(converted)
	return nil
}

func appendDoubleDataSlice(ddps, converted pdata.DoubleDataPointSlice, resource pdata.Resource) error {
	for i := 0; i < ddps.Len(); i++ {
		err := appendAttributesToLabels(ddps.At(i).Attributes(), resource.Attributes())
		if err != nil {
			return err
		}
	}
	ddps.MoveAndAppendTo(converted)
	return nil
}

// appendAttributesToLabels appends the provided attributes to the provided labels map.
// This requires converting the attributes to string format.
func appendAttributesToLabels(labels pdata.StringMap, attributes pdata.AttributeMap) error {
	var err error
	attributes.Range(func(k string, v pdata.AttributeValue) bool {
		// break if error has occurred in previous iteration
		if err != nil {
			return false
		}

		key := toCloudMonitoringLabel(k)
		// ignore attributes that do not map to a cloud ops label
		if key == "" {
			return true
		}

		var value string
		value, err = stringValue(v)
		// break if error
		if err != nil {
			return false
		}

		labels.Insert(key, value)
		return true
	})
	return err
}

func toCloudMonitoringLabel(resourceAttributeKey string) string {
	// see https://cloud.google.com/monitoring/api/metrics_agent#agent-processes
	switch resourceAttributeKey {
	case conventions.AttributeProcessPID:
		return "pid"
	case conventions.AttributeProcessExecutableName:
		return "command"
	case conventions.AttributeProcessCommandLine:
		return "command_line"
	case conventions.AttributeProcessOwner:
		return "owner"
	default:
		return ""
	}
}

func stringValue(attributeValue pdata.AttributeValue) (string, error) {
	var stringValue string
	switch t := attributeValue.Type(); t {
	case pdata.AttributeValueTypeBool:
		stringValue = strconv.FormatBool(attributeValue.BoolVal())
	case pdata.AttributeValueTypeInt:
		stringValue = strconv.FormatInt(attributeValue.IntVal(), 10)
	case pdata.AttributeValueTypeDouble:
		stringValue = strconv.FormatFloat(attributeValue.DoubleVal(), 'f', -1, 64)
	case pdata.AttributeValueTypeString:
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
