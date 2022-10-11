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
	expectMetricsLookRight(t, observed, expected)
}

func loadObservedMetrics(t *testing.T, scratchDir string) pmetric.Metrics {
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
	return metrics
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

// ExpectedMetric encodes a series of assertions about what data we expect
// to see in the metrics backend.
type ExpectedMetric struct {
	// The metric name, for example network/tcp_connections.
	Name string `yaml:"name" validate:"required"`
	// The value type, for example INT64.
	// TODO: check the value type.
	ValueType string `yaml:"value_type" validate:"required,oneof=BOOL INT64 DOUBLE STRING DISTRIBUTION"`
	// The kind, for example GAUGE.
	Kind string `yaml:"kind" validate:"required,oneof=GAUGE DELTA CUMULATIVE"`
	// Mapping of expected label keys to value patterns.
	// Patterns are RE2 regular expressions.
	Attributes map[string]string `yaml:"attributes,omitempty" validate:"omitempty,gt=0"`
}

// keys returns a slice containing just the keys from the input map m.
func keys[K comparable, V any](m map[K]V) []K {
	var ks []K
	for k, _ := range m {
		ks = append(ks, k)
	}
	return ks
}

func expectAttributesMatch(t *testing.T, dataPoints pmetric.NumberDataPointSlice, expectedAttrs map[string]string, metricName string) {
	// Map from attribute to set of observed values that that attribute takes on.
	// For example,
	//   map[
	//     cpu_state:map[idle:true interrupt:true]
	//     pid:map[123:true 456:true]
	//   ]
	actualAttributes := make(map[string]map[string]bool)

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

	// Only expected attributes must be present.
	for attribute, actualValues := range actualAttributes {
		if _, ok := expectedAttrs[attribute]; !ok {
			t.Errorf("Unexpected attribute %q with values %v found for metric %q.", attribute, actualValues, metricName)
		}
	}

	// Iterate over expectedAttrs, checking that:
	// 1. Every attribute in expectedAttrs appears in actualAttributes
	// 2. All values in actualAttributes match the regular expressions stored
	//    in expectedAttrs.
	for attribute, expectedPattern := range expectedAttrs {
		if _, ok := actualAttributes[attribute]; !ok {
			t.Errorf("Missing expected attribute %q on metric %q. Found attributes: %v", attribute, metricName, keys(actualAttributes))
			continue
		}

		for actualValue, _ := range actualAttributes[attribute] {
			match, matchErr := regexp.MatchString(fmt.Sprintf("^(?:%s)$", expectedPattern), actualValue)
			if matchErr != nil {
				t.Errorf("Error parsing pattern. metric=%s, attribute=%s, pattern=%s, err=%v",
					metricName,
					attribute,
					expectedPattern,
					matchErr,
				)
			} else if !match {
				t.Errorf("Attribute value does not match pattern. metric=%s, attribute=%s, pattern=%s, value=%s",
					metricName,
					attribute,
					expectedPattern,
					actualValue,
				)
			}
		}
	}
}

func expectMetricsLookRight(t *testing.T, metrics pmetric.Metrics, expectedMetrics map[string]ExpectedMetric) {
	seen := make(map[string]bool)

	resourceMetrics := metrics.ResourceMetrics()
	for i := 0; i < resourceMetrics.Len(); i++ {
		scopeMetrics := resourceMetrics.At(i).ScopeMetrics()
		for i := 0; i < scopeMetrics.Len(); i++ {
			metrics := scopeMetrics.At(i).Metrics()

			for j := 0; j < metrics.Len(); j++ {
				metric := metrics.At(j)

				expectation, ok := expectedMetrics[metric.Name()]
				if !ok {
					// TODO: Probably change to t.Logf once we've figured out why
					// system.disk.average_operation_time is appearing and whether we
					// should add any assertions about it.
					t.Errorf("Unexpected metric with name %q", metric.Name())
					continue
				}

				if seen[metric.Name()] {
					t.Errorf("Saw more than one entry for %q", metric.Name())
				}
				seen[metric.Name()] = true

				if got, want := metric.DataType().String(), expectation.Kind; got != want {
					t.Errorf("Metric %q had unexpected DataType() %q. want %q", metric.Name(), got, want)
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

				expectAttributesMatch(t, dataPoints, expectation.Attributes, metric.Name())
			}
		}
	}

	for expectedName, _ := range expectedMetrics {
		if !seen[expectedName] {
			t.Errorf("Never saw metric with name %q", expectedName)
		}
	}
}
