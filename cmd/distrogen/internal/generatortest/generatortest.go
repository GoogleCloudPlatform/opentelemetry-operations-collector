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

package generatortest

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

var goldenDirName = "golden"

// GeneratorFunc is a function type that represents what is
// run within a test directory, returning the path
// to find the generated files.
type GeneratorFunc func(t *testing.T) (generatePath string)

type GeneratorTester struct {
	testdataSubpath string
	generatorFunc   GeneratorFunc
}

// NewGeneratorTester creates a new GeneratorTester.
func NewGeneratorTester(testdataPath string, generatorFunc GeneratorFunc) *GeneratorTester {
	return &GeneratorTester{
		testdataSubpath: testdataPath,
		generatorFunc:   generatorFunc,
	}
}

// TestGenerator runs a generator test case.
func (gt *GeneratorTester) Run(t *testing.T) {
	// We change our working directory to the test directory
	// so that paths in configs and test code can assume the
	// generation is occurring relative to the test directory
	// itself.
	popd, err := os.Getwd()
	assert.NilError(t, err)
	testFolder := filepath.Join("testdata", gt.testdataSubpath)
	err = os.Chdir(testFolder)
	assert.NilError(t, err)

	// Run the requested generator function.
	generatedPath := gt.generatorFunc(t)

	// We now change back to the initial working directory. This is
	// because gotesttools.Golden will force looking in a directory
	// called "testdata" for the golden files, meaning we can't do
	// the assertion from within the test directory.
	assert.NilError(t, os.Chdir(popd))

	// The goldenPath will be the path to the golden directory without "testdata". This
	// is because gotesttools.Golden adds the "testdata" itself, so our path needs to be
	// relative to it.
	goldenPath := filepath.Join(gt.testdataSubpath, goldenDirName)
	assertGoldenFiles(t, generatedPath, goldenPath)
}

// assertGoldenFiles compares files in a generated directory against a golden directory.
// It fails the test if the file sets or their contents do not match.
// It handles the creation of golden files when the -update flag is used.
func assertGoldenFiles(t *testing.T, generatedPath, goldenPath string) {
	t.Helper()

	generatedSet, err := filesInDirAsSet(generatedPath)
	assert.NilError(t, err)

	// In update mode, `golden.Assert` will create/update the golden files.
	// In verify mode, it compares generated content against existing golden files.
	for file := range generatedSet {
		generatedContent, err := os.ReadFile(filepath.Join(generatedPath, file))
		assert.NilError(t, err)
		golden.Assert(t, string(generatedContent), filepath.Join(goldenPath, file))
	}

	// In verify mode, we must also ensure the set of generated files is identical
	// to the set of golden files.
	if !golden.FlagUpdate() {
		// gotesttools.Golden automatically searched in "testdata", so the provided
		// goldenPath doesn't contain this. We need to add it here to open it ourselves.
		fullGoldenPath := filepath.Join("testdata", goldenPath)
		goldenSet, err := filesInDirAsSet(fullGoldenPath)
		if os.IsNotExist(err) {
			// Golden directory doesn't exist; treat it as an empty set.
			goldenSet = make(map[string]struct{})
		} else {
			assert.NilError(t, err)
		}

		// This provides a clean diff if the sets of files are not identical.
		assert.DeepEqual(t, goldenSet, generatedSet)
	}
}

// filesInDirAsSet returns the set of file names within a given directory.
func filesInDirAsSet(dir string) (map[string]struct{}, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		// Let the caller handle os.IsNotExist if they choose to.
		return nil, err
	}

	fileSet := make(map[string]struct{})
	for _, file := range files {
		if file.IsDir() {
			nestedFileSet, err := filesInDirAsSet(filepath.Join(dir, file.Name()))
			if err != nil {
				return nil, err
			}
			for f := range nestedFileSet {
				fileSet[filepath.Join(file.Name(), f)] = struct{}{}
			}
		} else {
			fileSet[file.Name()] = struct{}{}
		}
	}
	return fileSet, nil
}
