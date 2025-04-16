// Copyright 2025 Google LLC
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

package main

import (
	"bufio"
	"errors"
	"flag"
	"log"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	flagSpec       = flag.String("spec", "", "distrogen spec file path")
	flagComponents = flag.String("components", "", "component list file path, a text list of go module URLs for components")

	componentTypeNames = []string{"receiver", "processor", "exporter", "extension", "provider", "connector"}
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	components, err := readComponents()
	if err != nil {
		return err
	}
	spec, err := readSpec()
	if err != nil {
		return err
	}

	for _, component := range components {
		foundComponent := false
		for _, componentType := range componentTypeNames {
			componentListAny := spec["components"].(map[string]any)[componentType+"s"].([]any)
			var componentList []string
			for _, componentAny := range componentListAny {
				componentList = append(componentList, componentAny.(string))
			}
			if slices.Contains(componentList, component) {
				foundComponent = true
				break
			}
		}
		if !foundComponent {
			log.Println("Component not found in spec:", component)
		}
	}

	return nil
}

func readComponents() ([]string, error) {
	if *flagComponents == "" {
		return nil, errors.New("missing -components flag")
	}

	// read file from path in -components flag line by line
	file, err := os.Open(*flagComponents)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var components []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		modUrlParts := strings.Split(scanner.Text(), "/")
		componentName := modUrlParts[len(modUrlParts)-1]
		for _, suffix := range componentTypeNames {
			componentName = strings.ReplaceAll(componentName, suffix, "")
		}
		componentName = strings.ReplaceAll(componentName, `"`, "")
		components = append(components, componentName)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return components, nil
}

func readSpec() (map[string]any, error) {
	// Read spec from path in -spec flag and yaml unmarshal into map[string]any
	if *flagSpec == "" {
		return nil, errors.New("missing -spec flag")
	}

	specBytes, err := os.ReadFile(*flagSpec)
	if err != nil {
		return nil, err
	}
	var spec map[string]any
	if err := yaml.Unmarshal(specBytes, &spec); err != nil {
		return nil, err
	}

	return spec, nil
}
