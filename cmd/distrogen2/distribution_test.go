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
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

var testdataSubpath = "generator"
var testdataFullPath = filepath.Join("testdata", "generator")

func TestTemplateGenerationFromSpec(t *testing.T) {
	registry, err := LoadEmbeddedRegistry()
	assert.NilError(t, err)

	testDirs, err := os.ReadDir(testdataFullPath)
	assert.NilError(t, err)
	for _, d := range testDirs {
		if !d.IsDir() {
			continue
		}

		name := d.Name()
		t.Run(name, func(t *testing.T) {
			testGeneratorCase(t, registry, name)
		})
	}
}

func testGeneratorCase(t *testing.T, registry *Registry, testFolder string) {
	specPath := filepath.Join(testdataFullPath, testFolder, "spec.yaml")

	d, err := NewDistributionSpec(specPath)
	assert.NilError(t, err)

	g, err := NewDistributionGenerator(d, registry, true)
	assert.NilError(t, err)

	// Ensure the template directory exists before generation.
	customTemplates := filepath.Join(testdataFullPath, testFolder, "templates")
	_, err = os.Stat(customTemplates)
	if err == nil {
		g.CustomTemplatesDir = os.DirFS(customTemplates)
	}
	err = g.Generate()
	assert.NilError(t, err)
	t.Cleanup(func() {
		g.Clean()
	})

	generatedFiles, err := filesInDirAsSet(g.GeneratePath)
	assert.NilError(t, err)
getGoldenFiles:
	goldenFiles, err := filesInDirAsSet(filepath.Join(testdataFullPath, testFolder, "golden"))
	if os.IsNotExist(err) {
		t.Logf("golden folder not found in %s, creating it.", testFolder)
		err := os.Mkdir(filepath.Join(testdataFullPath, testFolder, "golden"), 0755)
		assert.NilError(t, err)
		goto getGoldenFiles
	}

	testFailed := false
	for generatedFile := range generatedFiles {
		_, foundFile := goldenFiles[generatedFile]
		if !golden.FlagUpdate() {
			foundCheck := assert.Check(t, foundFile, "generated file not found in golden folder: %s", generatedFile)
			if !foundCheck {
				testFailed = true
				continue
			}
		}
		goldenFiles[generatedFile] = true
		generatedFiles[generatedFile] = true
		generatedContent, err := os.ReadFile(filepath.Join(g.GeneratePath, generatedFile))
		assert.NilError(t, err)
		golden.Assert(t, string(generatedContent), filepath.Join(testdataSubpath, testFolder, "golden", generatedFile))
	}
	assert.Assert(t, !testFailed, "golden check failed, generation did not equal golden files")

	for file, found := range goldenFiles {
		assert.Check(t, found, "golden file %s not found in generated folder", file)
	}
	for file, found := range generatedFiles {
		assert.Check(t, found, "generated file %s not found in golden folder", file)
	}
}

func filesInDirAsSet(dir string) (map[string]bool, error) {
	fileSet := make(map[string]bool)
	err := fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		fileSet[path] = false
		return nil
	})
	return fileSet, err
}

func TestSpecQuery(t *testing.T) {
	otelVer := "v0.124.0"
	spec := &DistributionSpec{
		OpenTelemetryVersion: otelVer,
	}
	val, err := spec.Query("opentelemetry_version")
	assert.NilError(t, err)
	assert.Equal(t, val, otelVer)
}

func TestSpecQueryNotFound(t *testing.T) {
	spec := &DistributionSpec{}
	_, err := spec.Query("random_field_name")
	assert.ErrorIs(t, err, ErrQueryValueNotFound)
}
