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
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

var flagOTelVersion = flag.String("otel_version", "", "The OpenTelemetry version to fetch component versions for")

type Versions struct {
	ModuleSets      map[string]ModuleSet `yaml:"module-sets"`
	ExcludedModules []string             `yaml:"excluded-modules"`
}

type ModuleSet struct {
	Version string   `yaml:"version"`
	Modules []string `yaml:"modules"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	flag.Parse()

	if *flagOTelVersion == "" {
		return fmt.Errorf("otel_version flag is required")
	}

	allModules, err := readModulesFromStdin()
	if err != nil {
		return err
	}

	versions, err := fetchVersionsYaml(*flagOTelVersion)
	if err != nil {
		return err
	}

	for _, moduleSet := range versions.ModuleSets {
		for _, module := range allModules {
			if slices.Contains(moduleSet.Modules, module) {
				fmt.Printf("%s@%s\n", module, moduleSet.Version)
			}
		}
	}

	return nil
}

func fetchVersionsYaml(tag string) (*Versions, error) {
	response, err := http.Get(fmt.Sprintf("https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector/refs/tags/%s/versions.yaml", tag))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var versions Versions
	if err := yaml.Unmarshal(content, &versions); err != nil {
		return nil, err
	}
	return &versions, nil
}

func readModulesFromStdin() ([]string, error) {
	s := bufio.NewScanner(os.Stdin)
	var modules []string
	for s.Scan() {
		modules = append(modules, s.Text())
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return modules, nil
}
