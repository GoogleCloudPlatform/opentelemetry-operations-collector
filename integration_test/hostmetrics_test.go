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
package service_test

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

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/service"
)

func TestHostmetrics(t *testing.T) {
	testDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	os.Args = append(os.Args, fmt.Sprintf("--config=%s", filepath.Join(testDir, "hostmetrics-config.yaml")))

	// Make a scratch directory and cd there so that otelopscol will write
	// metrics.json to a scratch directory instead of into source control.
	scratchDir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(scratchDir); err != nil {
		t.Fatal(err)
	}

	terminationTime := 4 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), terminationTime)
	defer cancel()

	// Run the main function of otelopscol.
	// It will self-terminate in terminationTime.
	service.MainContext(ctx)

	observed := loadObservedMetrics(t, filepath.Join(scratchDir, "metrics.json"))
	expected := loadExpectedMetrics(t, filepath.Join(testDir, "expected-metrics.yaml"))

	expectMetricsMatch(t, observed, expected)
}

// ExpectedMetric encodes a series of assertions about what data we expect
// to see in the metrics backend.
type ExpectedMetric struct {
	// The metric name, for example system.network.connections.
	Name string `yaml:"name"`
	// The value type, for example "Int".
	ValueType string `yaml:"value_type"`
	// The metric type, for example "Gauge".
	Type string `yaml:"type"`
	// Mapping of expected attribute keys to value patterns.
	// Patterns are RE2 regular expressions.
	Attributes map[string]string `yaml:"attributes"`
	// Mapping of expected resource attribute keys to value patterns.
	// Patterns are RE2 regular expressions.
	ResourceAttributes map[string]string `yaml:"resource_attributes"`
}

// loadExpectedMetrics reads the metrics expectations from the given path.
func loadExpectedMetrics(t *testing.T, expectedMetricsPath string) map[string]ExpectedMetric {
	data, err := os.ReadFile(expectedMetricsPath)
	if err != nil {
		t.Fatal(err)
	}
	var agentMetrics struct {
		ExpectedMetrics []ExpectedMetric `yaml:"expected_metrics"`
	}

	if err := yaml.UnmarshalStrict(data, &agentMetrics); err != nil {
		t.Fatal(err)
	}

	result := make(map[string]ExpectedMetric)
	for i, expect := range agentMetrics.ExpectedMetrics {
		if expect.Name == "" {
			t.Fatalf("ExpectedMetrics[%v] missing required field 'Name'.", i)
		}
		if _, ok := result[expect.Name]; ok {
			t.Fatalf("Found multiple ExpectedMetric entries with Name=%q", expect.Name)
		}
		result[expect.Name] = expect
	}

	t.Logf("Loaded %v metrics expectations from %s", len(result), expectedMetricsPath)

	if len(result) < 2 {
		t.Fatalf("Unreasonably few (<2) expectations found. expectations=%v", result)
	}
	return result
}

// loadObservedMetrics reads the contents of metrics.json produced by
// otelopscol and selects one of the lines from it, corresponding to a single
// batch of exported metrics.
func loadObservedMetrics(t *testing.T, metricsJSONPath string) pmetric.Metrics {
	data, err := os.ReadFile(metricsJSONPath)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 {
		t.Fatalf("Only found %v lines in %q, need at least 2", len(lines), metricsJSONPath)
	}

	// Take the second batch of data exported by otelopscol. Picking the first
	// batch can hit problems with certain metrics like system.cpu.utilization,
	// which don't appear in the first batch.
	secondBatch := []byte(lines[1])

	t.Logf("Found %v bytes of data at %s, selecting %v bytes", len(data), metricsJSONPath, len(secondBatch))

	metrics, err := pmetric.NewJSONUnmarshaler().UnmarshalMetrics([]byte(secondBatch))
	if err != nil {
		t.Fatal(err)
	}
	return metrics
}

// expectMetricsMatch checks that all the data in `observedMetrics` matches
// the expectations configured in `expectedMetrics`.
// Note that an individual metric can appear many times in `observedMetrics`
// under different values of its resource attributes. In particular, the
// process.* metrics have resource attributes and so they will appear N times,
// where N is the number of process resources that were detected.
func expectMetricsMatch(t *testing.T, observedMetrics pmetric.Metrics, expectedMetrics map[string]ExpectedMetric) {
	// Holds the set of metrics that were seen somewhere in observedMetrics.
	seen := make(map[string]bool)

	resourceMetrics := observedMetrics.ResourceMetrics()
	for i := 0; i < resourceMetrics.Len(); i++ {
		scopeMetrics := resourceMetrics.At(i).ScopeMetrics()

		resourceAttributes := resourceMetrics.At(i).Resource().Attributes() // Used below.
		for j := 0; j < scopeMetrics.Len(); j++ {
			metrics := scopeMetrics.At(j).Metrics()

			for k := 0; k < metrics.Len(); k++ {
				metric := metrics.At(k)

				name := metric.Name()

				seen[name] = true

				expected, ok := expectedMetrics[name]
				if !ok {
					// It's debatable whether a new metric being introduced should cause
					// this test to fail. We decided to fail the test in this case because
					// otherwise, there's no incentive to add coverage for new metrics as
					// they appear. If it proves onerous to keep fixing this test, we can
					// remove this check and come up with some other way to keep this test
					// up to date.
					t.Errorf("Unexpected metric %q observed.", name)
					continue
				}

				if metric.Type().String() != expected.Type {
					t.Errorf("For metric %q, Type()=%v, want %v",
						name,
						metric.Type().String(),
						expected.Type,
					)
				}

				expectAttributesMatch(t, resourceAttributes, expected.ResourceAttributes, name, "resource attribute")

				var dataPoints pmetric.NumberDataPointSlice
				switch metric.Type() {
				case pmetric.MetricTypeGauge:
					dataPoints = metric.Gauge().DataPoints()
				case pmetric.MetricTypeSum:
					dataPoints = metric.Sum().DataPoints()
				default:
					t.Errorf("Unimplemented handling for metric type %v", metric.Type())
				}

				for i := 0; i < dataPoints.Len(); i++ {
					dataPoint := dataPoints.At(i)
					expectAttributesMatch(t, dataPoint.Attributes(), expected.Attributes, name, "attribute")
				}

				expectValueTypesMatch(t, dataPoints, expected.ValueType, name)
			}
		}
	}

	// Don't forget to check that we saw all the metrics we expected!
	for name, _ := range expectedMetrics {
		if _, ok := seen[name]; !ok {
			t.Errorf("Never saw metric with name %q", name)
		}
	}
}

// expectValueTypesMatch checks that all data points in `dataPoints` have the
// expected ValueType().
func expectValueTypesMatch(t *testing.T, dataPoints pmetric.NumberDataPointSlice, expectedValueType, metricName string) {
	for i := 0; i < dataPoints.Len(); i++ {
		dataPoint := dataPoints.At(i)

		newValueType := dataPoint.ValueType().String()
		if newValueType != expectedValueType {
			t.Errorf("For metric %q, ValueType()=%v, want %v",
				metricName,
				newValueType,
				expectedValueType,
			)
		}
	}
}

// expectAttributesMatch checks that the given pcommon.Map matches
// the attributes and value regexes from expectedAttributes. Specifically,
//  1. observedAttributes and expectedAttributes must have the same exact
//     set of keys, and
//  2. all values in observedAttributes[attr] must match the regex in
//     expectedAttributes[attr].
//
// The `kind` argument should either be "attribute" or "resource attribute"
// and is only used to generate appropriate error messages about what kind
// of attribute is not matching.
func expectAttributesMatch(t *testing.T, observedAttributes pcommon.Map, expectedAttributes map[string]string, metricName, kind string) {
	// Iterate over observedAttributes, checking that:
	// 1. Every attribute in observedAttributes is expected
	// 2. Every attribute values in observedAttributes matches the regular
	//    expression stored in expectedAttributes.
	observedAttributes.Range(func(attribute string, pValue pcommon.Value) bool {
		observedValue := pValue.AsString()
		expectedPattern, ok := expectedAttributes[attribute]
		if !ok {
			t.Errorf("For metric %q, unexpected %s %q with value=%q",
				metricName,
				kind,
				attribute,
				observedValue,
			)
			return true // Tell Range() to keep going.
		}
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
		return true // Tell Range() to keep going.
	})

	// Check that all expected attributes actually apper in observedAttributes.
	for attribute, _ := range expectedAttributes {
		if _, ok := observedAttributes.Get(attribute); !ok {
			t.Errorf("For metric %q, missing expected %s %q. Found: %v",
				metricName,
				kind,
				attribute,
				keys(observedAttributes))
			continue
		}
	}
}

// keys returns a slice containing just the keys from the input pcommon.Map m.
func keys(m pcommon.Map) []string {
	ks := make([]string, m.Len())
	i := 0
	m.Range(func(k string, _ pcommon.Value) bool {
		ks[i] = k
		i++
		return true // Tell Range() to keep going.
	})
	return ks
}
