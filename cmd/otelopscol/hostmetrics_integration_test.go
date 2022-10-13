// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"gopkg.in/yaml.v2"
)

func TestHostmetrics(t *testing.T) {
	terminationTime := 4 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), terminationTime)
	defer cancel()

	testDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	os.Args = append(os.Args, fmt.Sprintf("--config=%s", filepath.Join(testDir, "config-for-testing.yaml")))

	scratchDir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatalf("Couldn't create scratch directory. err=%v", err)
	}
	os.Chdir(scratchDir)

	// Run the main function of otelopscol.
	// It will self-terminate in terminationTime.
	mainContext(ctx)

	observed := loadObservedMetrics(t, filepath.Join(scratchDir, "metrics.json"))
	expected := loadExpectedMetrics(t, filepath.Join(testDir, "expected-metrics.yaml"))

	expectMetricsMatch(t, observed, expected)
}

// TODO: Implement YAML validation.

// ExpectedMetric encodes a series of assertions about what data we expect
// to see in the metrics backend.
type ExpectedMetric struct {
	// The metric name, for example system.network.connections.
	Name string `yaml:"name" validate:"required"`
	// The value type, for example "Int".
	ValueType string `yaml:"value_type" validate:"required,oneof=Int Double"`
	// The metric data type, for example "Gauge".
	DataType string `yaml:"data_type" validate:"required,oneof=Gauge Sum"`
	// Mapping of expected attribute keys to value patterns.
	// Patterns are RE2 regular expressions.
	Attributes map[string]string `yaml:"attributes,omitempty" validate:"omitempty,gt=0"`
	// Mapping of expected resource attribute keys to value patterns.
	// Patterns are RE2 regular expressions.
	ResourceAttributes map[string]string `yaml:"resource_attributes,omitempty" validate:"omitempty,gt=0"`
}

// Mapping of observed attribute keys to sets of observed values.
// For example,
//
//	map[
//	  cpu_state:map[idle:true interrupt:true]
//	  pid:map[123:true 456:true]
//	]
//
// This would be a map[string]set[string] if go had a type called set[string].
type attributeMap map[string]map[string]bool

type ObservedMetric struct {
	// The value type, for example INT64.
	ValueType string
	// The metric data type, for example "Gauge".
	DataType string
	// Mapping of observed attribute keys to sets of observed values.
	Attributes attributeMap
	// Mapping of resource attribute keys to sets of observed values.
	ResourceAttributes attributeMap
}

// loadExpectedMetrics reads the metrics expectations from the given path.
func loadExpectedMetrics(t *testing.T, expectedMetricsPath string) map[string]ExpectedMetric {
	data, err := os.ReadFile(expectedMetricsPath)
	if err != nil {
		t.Fatal(err)
	}
	var agentMetrics struct {
		ExpectedMetrics []ExpectedMetric `yaml:"expected_metrics" validate:"unique=Name,dive"`
	}

	err = yaml.UnmarshalStrict(data, &agentMetrics)
	if err != nil {
		t.Fatal(err)
	}

	result := make(map[string]ExpectedMetric)
	for _, expect := range agentMetrics.ExpectedMetrics {
		result[expect.Name] = expect
	}

	if len(result) < 2 {
		t.Fatalf("Unreasonably few (<2) expectations found. expectations=%v", result)
	}
	return result
}

// loadObservedMetrics reads the contents of metrics.json produced by
// otelopscol, selects one of the lines from it, and processes the data into
// a form suitable for comparing against the expectations from
// expected-metrics.yaml.
func loadObservedMetrics(t *testing.T, metricsJSONPath string) map[string]ObservedMetric {
	data, err := os.ReadFile(metricsJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Found %v bytes of data at %s", len(data), metricsJSONPath)

	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		t.Fatalf("Only found %v lines in %q, need at least 2", len(lines), metricsJSONPath)
	}

	// Take the second batch of data exported by otelopscol. Picking the first
	// batch can hit problems with certain metrics like system.cpu.utilization,
	// which don't appear in the first batch.
	secondBatch := lines[1]
	metrics, err := pmetric.NewJSONUnmarshaler().UnmarshalMetrics([]byte(secondBatch))
	if err != nil {
		t.Fatal(err)
	}
	// Convert the pmetric.Metrics into a more usable data structure.
	return consolidateObservedMetrics(t, metrics)
}

// consolidateObservedMetrics collects all data from the given pmetric.Metrics
// into a simple map of metric name -> ObservedMetric. The pmetric.Metrics
// is awkward to use directly because
// TODO: probably we should be using it directly...
func consolidateObservedMetrics(t *testing.T, metrics pmetric.Metrics) map[string]ObservedMetric {
	// Map from metric name to details about that metric.
	observedMetrics := make(map[string]ObservedMetric)

	resourceMetrics := metrics.ResourceMetrics()
	for i := 0; i < resourceMetrics.Len(); i++ {
		scopeMetrics := resourceMetrics.At(i).ScopeMetrics()
		resourceAttributes := resourceMetrics.At(i).Resource().Attributes() // Used below.
		for j := 0; j < scopeMetrics.Len(); j++ {
			metrics := scopeMetrics.At(j).Metrics()

			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)

				// Initialize an ObservedMetric for our current metric if there isn't
				// one already stored in observedMetrics.
				if _, ok := observedMetrics[metric.Name()]; !ok {
					observedMetrics[metric.Name()] = ObservedMetric{
						Attributes:         make(attributeMap),
						ResourceAttributes: make(attributeMap),
					}
				}
				observed := observedMetrics[metric.Name()]

				var dataPoints pmetric.NumberDataPointSlice
				switch metric.DataType() {
				case pmetric.MetricDataTypeGauge:
					dataPoints = metric.Gauge().DataPoints()
				case pmetric.MetricDataTypeSum:
					dataPoints = metric.Sum().DataPoints()
				default:
					t.Errorf("Unimplemented handling for metric type %v", metric.DataType())
				}

				mergeAttributes(observed.Attributes, extractAttributes(dataPoints))
				mergeAttributes(observed.ResourceAttributes, extractResourceAttributes(resourceAttributes))

				newDataType := metric.DataType().String()
				if observed.DataType == "" {
					observed.DataType = newDataType
				} else if observed.DataType != newDataType {
					t.Errorf("Metric %q had conflicting DataTypes (%q and %q)",
						metric.Name(),
						observed.DataType,
						newDataType,
					)
				}

				newValueType := extractValueType(t, dataPoints, metric.Name())
				if observed.ValueType == "" {
					observed.ValueType = newValueType
				} else if observed.ValueType != newValueType {
					t.Errorf("Metric %q had conflicting ValueTypes (%q and %q)",
						metric.Name(),
						observed.ValueType,
						newValueType,
					)
				}

				observedMetrics[metric.Name()] = observed
			}
		}
	}
	return observedMetrics
}

func extractValueType(t *testing.T, dataPoints pmetric.NumberDataPointSlice, metricName string) string {
	valueType := ""
	for i := 0; i < dataPoints.Len(); i++ {
		dataPoint := dataPoints.At(i)

		newValueType := dataPoint.ValueType().String()
		if valueType == "" {
			valueType = newValueType
		} else if valueType != newValueType {
			t.Errorf("Metric %q had conflicting ValueTypes (%v and %v)",
				metricName,
				valueType,
				newValueType,
			)
		}
	}
	return valueType
}

// mergeAttributes modifies the first argument by merging in all entries
// from the second argument.
func mergeAttributes(attributes attributeMap, newAttributes attributeMap) {
	for attribute, values := range newAttributes {
		if _, ok := attributes[attribute]; !ok {
			attributes[attribute] = make(map[string]bool)
		}
		for value, _ := range values {
			attributes[attribute][value] = true
		}
	}
}

// extractAttributes takes the union of all attributes in the given list
// of data points.
func extractAttributes(dataPoints pmetric.NumberDataPointSlice) attributeMap {
	attributes := make(attributeMap)

	// Collect all the attributes across all data points into the
	// `attributes` map.
	for i := 0; i < dataPoints.Len(); i++ {
		dataPoint := dataPoints.At(i)
		dataPoint.Attributes().Range(func(k string, v pcommon.Value) bool {
			if _, ok := attributes[k]; !ok {
				attributes[k] = make(map[string]bool)
			}
			attributes[k][v.AsString()] = true
			return true // Tell Range() to keep going.
		})
	}
	return attributes
}

// extractResourceAttributes converts the given pcommon.Map into an
// attributeMap.
func extractResourceAttributes(attributes pcommon.Map) attributeMap {
	result := make(attributeMap)

	attributes.Range(func(k string, v pcommon.Value) bool {
		if _, ok := result[k]; !ok {
			result[k] = make(map[string]bool)
		}
		result[k][v.AsString()] = true
		return true // Tell Range() to keep going.
	})
	return result
}

// expectMetricsMatch checks that all the metrics in observedMetrics match
// expectations in expectedMetrics, and that all expectations in
// expectedMetrics are fulfilled.
func expectMetricsMatch(t *testing.T, observedMetrics map[string]ObservedMetric, expectedMetrics map[string]ExpectedMetric) {
	for name, observed := range observedMetrics {
		expected, ok := expectedMetrics[name]
		if !ok {
			// It's debatable whether a new metric being introduced should cause
			// this test to fail. We decided to fail the test in this case because
			// otherwise, there's no incentive to add coverage for new metrics as
			// they appear. If it proves onerous to keep fixing this test, we can
			// remove this check and come up with some other way to keep this test
			// up to date.
			t.Errorf("Unexpected metric %q observed. details=%#v", name, observed)
			continue
		}

		expectMetricMatches(t, observed, expected)
	}

	for name, _ := range expectedMetrics {
		if _, ok := observedMetrics[name]; !ok {
			t.Errorf("Never saw metric with name %q", name)
		}
	}
}

// expectMetricMatches checks that the given ObservedMetric looks right
// based on the given expectations for that metric.
func expectMetricMatches(t *testing.T, observed ObservedMetric, expected ExpectedMetric) {
	expectAttributesMatch(t, observed.Attributes, expected.Attributes, expected.Name, "attribute")
	expectAttributesMatch(t, observed.ResourceAttributes, expected.ResourceAttributes, expected.Name, "resource attribute")

	if observed.DataType != expected.DataType {
		t.Errorf("For metric %q, observed.DataType=%v, want %v",
			expected.Name,
			observed.DataType,
			expected.DataType,
		)
	}
	if observed.ValueType != expected.ValueType {
		t.Errorf("For metric %q, observed.ValueType=%v, want %v",
			expected.Name,
			observed.ValueType,
			expected.ValueType,
		)
	}
}

// expectAttributesMatch checks that the given attributeMap matches
// the attributes and value regexes from expectedAttributes. Specifically,
//  1. observedAttributes and expectedAttributes must have the same exact
//     set of keys, and
//  2. all values in observedAttributes[attr] must match the regex in
//     expectedAttributes[attr].
func expectAttributesMatch(t *testing.T, observedAttributes attributeMap, expectedAttributes map[string]string, metricName, kind string) {
	// Only expected attributes must be present.
	for attribute, observedValues := range observedAttributes {
		if _, ok := expectedAttributes[attribute]; !ok {
			t.Errorf("Unexpected %s %q with values %v found for metric %q.", kind, attribute, observedValues, metricName)
		}
	}

	// Iterate over expectedAttributes, checking that:
	// 1. Every attribute in expectedAttributes appears in observedAttributes
	// 2. All values in observedAttributes match the regular expressions stored
	//    in expectedAttributes.
	for attribute, expectedPattern := range expectedAttributes {
		if _, ok := observedAttributes[attribute]; !ok {
			t.Errorf("Missing expected %s %q on metric %q. Found: %v", kind, attribute, metricName, keys(observedAttributes))
			continue
		}

		for observedValue, _ := range observedAttributes[attribute] {
			match, matchErr := regexp.MatchString(fmt.Sprintf("^(?:%s)$", expectedPattern), observedValue)
			if matchErr != nil {
				t.Errorf("Error parsing pattern. metric=%s, attribute=%s, pattern=%s, err=%v",
					metricName,
					attribute,
					expectedPattern,
					matchErr,
				)
			} else if !match {
				t.Errorf("Value does not match pattern. metric=%s, %s=%s, pattern=%s, value=%s",
					metricName,
					kind,
					attribute,
					expectedPattern,
					observedValue,
				)
			}
		}
	}
}

// keys returns a slice containing just the keys from the input map m.
func keys[K comparable, V any](m map[K]V) []K {
	ks := make([]K, len(m))
	i := 0
	for k := range m {
		ks[i] = k
		i++
	}
	return ks
}
