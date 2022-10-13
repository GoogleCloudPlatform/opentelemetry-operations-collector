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

	observed := loadObservedMetrics(t, scratchDir)
	expected := loadExpectedMetrics(t, testDir)

	expectMetricsMatch(t, observed, expected)
}

// TODO: revisit all this YAML validation. does it work? what does it do?

// ExpectedMetric encodes a series of assertions about what data we expect
// to see in the metrics backend.
type ExpectedMetric struct {
	// The metric name, for example network/tcp_connections.
	Name string `yaml:"name" validate:"required"`
	// The value type, for example INT64.
	ValueType string `yaml:"value_type" validate:"required,oneof=BOOL INT64 DOUBLE STRING DISTRIBUTION"`
	// The metric data type, for example GAUGE.
	DataType string `yaml:"data_type" validate:"required,oneof=GAUGE DELTA CUMULATIVE"`
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
type attributeMap map[string]map[string]bool

type ObservedMetric struct {
	// The value type, for example INT64.
	// TODO: check the value type.
	ValueType string
	// The metric data type, for example GAUGE.
	DataType string
	// Mapping of observed attribute keys to sets of observed values.
	Attributes attributeMap
	// Mapping of resource attribute keys to sets of observed values.
	ResourceAttributes attributeMap
}

func loadExpectedMetrics(t *testing.T, testDir string) map[string]ExpectedMetric {
	data, err := os.ReadFile(filepath.Join(testDir, "expected-metrics.yaml"))
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
	return result
}

func loadObservedMetrics(t *testing.T, scratchDir string) map[string]ObservedMetric {
	metricsJSONPath := filepath.Join(scratchDir, "metrics.json")
	data, err := os.ReadFile(metricsJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Found %v bytes of data at %s", len(data), metricsJSONPath)

	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		t.Fatalf("Only found %v lines in %q, need at least 2", len(lines), metricsJSONPath)
	}

	// Take the second batch of data exported by otelopscol, because the
	// first batch doesn't include any data for system.cpu.utilization.
	secondBatch := lines[1]
	metrics, err := pmetric.NewJSONUnmarshaler().UnmarshalMetrics([]byte(secondBatch))
	if err != nil {
		t.Fatal(err)
	}
	return consolidateObservedMetrics(t, metrics)
}

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

				if _, ok := observedMetrics[metric.Name()]; !ok {
					observedMetrics[metric.Name()] = ObservedMetric{
						Attributes:         make(attributeMap),
						ResourceAttributes: make(attributeMap),
					}
				}
				observed := observedMetrics[metric.Name()]

				if observed.DataType == "" {
					observed.DataType = metric.DataType().String()
				} else if observed.DataType != metric.DataType().String() {
					t.Errorf("Metric %q had two different DataTypes: %q and %q",
						metric.Name(),
						observed.DataType,
						metric.DataType().String(),
					)
				}

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

				observedMetrics[metric.Name()] = observed
			}
		}
	}
	return observedMetrics
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

// TODO: comment this function and others in this file
func extractAttributes(dataPoints pmetric.NumberDataPointSlice) attributeMap {
	actualAttributes := make(attributeMap)

	// Collect all the attributes across all data points into the
	// `actualAttributes` map.
	for i := 0; i < dataPoints.Len(); i++ {
		dataPoint := dataPoints.At(i)
		attributes := dataPoint.Attributes()
		attributes.Range(func(k string, v pcommon.Value) bool {
			if _, ok := actualAttributes[k]; !ok {
				actualAttributes[k] = make(map[string]bool)
			}
			actualAttributes[k][v.AsString()] = true
			return true // Tell Range() to keep going.
		})
	}
	return actualAttributes
}

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

func expectMetricsMatch(t *testing.T, observedMetrics map[string]ObservedMetric, expectedMetrics map[string]ExpectedMetric) {
	for name, observed := range observedMetrics {
		expected, ok := expectedMetrics[name]
		if !ok {
			// TODO: Probably change to t.Logf once we've figured out why
			// system.disk.average_operation_time is appearing and whether we
			// should add any assertions about it.
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

func expectAttributesMatch(t *testing.T, observedAttributes attributeMap, expectedAttributes map[string]string, metricName, kind string) {
	// Only expected attributes must be present.
	for attribute, actualValues := range observedAttributes {
		if _, ok := expectedAttributes[attribute]; !ok {
			t.Errorf("Unexpected %s %q with values %v found for metric %q.", kind, attribute, actualValues, metricName)
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

		for actualValue, _ := range observedAttributes[attribute] {
			match, matchErr := regexp.MatchString(fmt.Sprintf("^(?:%s)$", expectedPattern), actualValue)
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
					actualValue,
				)
			}
		}
	}
}

// keys returns a slice containing just the keys from the input map m.
func keys[K comparable, V any](m map[K]V) []K {
	var ks []K
	for k, _ := range m {
		ks = append(ks, k)
	}
	return ks
}
