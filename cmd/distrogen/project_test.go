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
	"os"
	"path/filepath"
	"testing"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/cmd/distrogen/internal/generatortest"
	"gotest.tools/v3/assert"
)

var (
	testdataProjectSubpath = filepath.Join("generator", "project")
)

func TestProjectTemplateGeneration(t *testing.T) {
	generatorTester := generatortest.NewGeneratorTester(
		testdataProjectSubpath,
		runProjectGenerator,
	)

	generatorTester.Run(t)
}

func runProjectGenerator(t *testing.T) string {
	specPath := "spec.yaml"
	d, err := NewDistributionSpec(specPath)
	assert.NilError(t, err)

	p, err := NewProjectGenerator(d)
	assert.NilError(t, err)

	// In this case we want the generated project to end up in a directory called
	// "golden".
	wd, err := os.Getwd()
	assert.NilError(t, err)
	p.GeneratePath = filepath.Join(wd, "golden")

	err = p.Generate()
	assert.NilError(t, err)

	return p.GeneratePath
}
