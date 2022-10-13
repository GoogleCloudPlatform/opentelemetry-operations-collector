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
	"fmt"
	"os"
	"sort"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestFoo(t *testing.T) {
	return
	allMetrics := make(map[string]Metric)

	scrapers := []string{"cpu", "disk", "filesystem", "load", "memory", "network", "paging", "processes", "process"}

	pattern := "/usr/local/google/home/martijnvs/opentelemetry-collector-contrib/receiver/hostmetricsreceiver/internal/scraper/%sscraper/metadata.yaml"
	for _, scraper := range scrapers {
		data, err := os.ReadFile(fmt.Sprintf(pattern, scraper))
		if err != nil {
			t.Fatal(err)
		}

		var agentMetrics struct {
			Metrics map[string]Metric
		}

		err = yaml.Unmarshal([]byte(data), &agentMetrics)
		if err != nil {
			t.Fatal(err)
		}

		for k, v := range agentMetrics.Metrics {
			allMetrics[k] = v
		}
	}

	metrics2 := convert(t, allMetrics)
	t.Error(len(metrics2))

	sort.Slice(metrics2, func(i, j int) bool {
		return metrics2[i].Name < metrics2[j].Name
	})
	data, err := yaml.Marshal(metrics2)
	if err != nil {
		t.Fatal(err)
	}
	t.Error(string(data))
}

func convert(t *testing.T, orig map[string]Metric) []Metric2 {
	var result []Metric2
	for k, v := range orig {
		m := Metric2{
			Name:       k,
			Attributes: make(map[string]string),
		}

		for _, attr := range v.Attributes {
			m.Attributes[attr] = "54321"
		}

		blank := Details{}
		if v.Sum != blank {
			m.Kind = "Sum"
			m.Value_type = v.Sum.Value_type
		} else if v.Gauge != blank {
			m.Kind = "Sum"
			m.Value_type = v.Sum.Value_type
		} else {
			t.Fatalf("unsupported type: k=%v,v=%v", k, v)
		}

		result = append(result, m)
	}
	return result
}

type Details struct {
	Value_type  string
	Aggregation string
	Monotonic   bool
}

type Metric struct {
	Attributes []string
	Sum        Details
	Gauge      Details
}

type Metric2 struct {
	Name       string
	Value_type string
	Kind       string
	Attributes map[string]string
}
