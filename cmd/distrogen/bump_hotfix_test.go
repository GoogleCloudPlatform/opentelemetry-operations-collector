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

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
	"gotest.tools/v3/assert"
)

type bumpHotfixTestCase struct {
	Name               string `yaml:"name"`
	IdentifierOverride string `yaml:"identifier_override,omitempty"`
	InitialContent     string `yaml:"initial_content"`
	ExpectedOutput     string `yaml:"expected_output"`
	ExpectedError      string `yaml:"expected_error,omitempty"`
}

func TestBumpHotfix(t *testing.T) {
	testdataDir := filepath.Join("testdata", "bump_hotfix")
	files, err := os.ReadDir(testdataDir)
	assert.NilError(t, err)

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".yaml" {
			continue
		}

		filePath := filepath.Join(testdataDir, file.Name())
		content, err := os.ReadFile(filePath)
		assert.NilError(t, err)

		var tc bumpHotfixTestCase
		err = yaml.Unmarshal(content, &tc)
		assert.NilError(t, err)

		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			tempDir := t.TempDir()
			specPath := filepath.Join(tempDir, "spec.yaml")

			err := os.WriteFile(specPath, []byte(tc.InitialContent), 0644)
			assert.NilError(t, err)

			err = BumpHotfix(specPath, tc.IdentifierOverride)
			if tc.ExpectedError != "" {
				assert.ErrorContains(t, err, tc.ExpectedError)
				return
			}

			assert.NilError(t, err)
			updatedContent, err := os.ReadFile(specPath)
			assert.NilError(t, err)

			if diff := cmp.Diff(tc.ExpectedOutput, string(updatedContent)); diff != "" {
				t.Errorf("BumpHotfix() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
