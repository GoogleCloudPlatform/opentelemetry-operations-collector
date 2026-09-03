// Copyright 2026 Google LLC
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

package googlecontrolplaneprovider

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "update golden files")

func TestPolicySets(t *testing.T) {
	testdataDir := filepath.Join("testdata", "policysets")
	entries, err := os.ReadDir(testdataDir)
	require.NoError(t, err)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			resetGlobals()
			CollectorID = "76728b45-8fcf-4cc8-a09e-c67283bfa79c"
			t.Cleanup(func() {
				resetGlobals()
			})
			t.Setenv("FLEET_ID", "1234")

			policiesDir := filepath.Join(testdataDir, entry.Name(), "policies")
			require.DirExists(t, policiesDir)

			absPoliciesDir, err := filepath.Abs(policiesDir)
			require.NoError(t, err)

			p := createProvider()
			t.Cleanup(func() {
				assert.NoError(t, p.Shutdown(context.Background()))
				for googlepolicy.ActivePolicySet() != nil {
					googlepolicy.RollbackActivePolicySet()
				}
			})

			uri := fmt.Sprintf("%s:%s://%s", schemeName, innerSchemeFile, filepath.ToSlash(absPoliciesDir))
			ret, err := p.Retrieve(context.Background(), uri, nil)
			require.NoError(t, err)
			require.NotNil(t, ret)

			conf, err := ret.AsConf()
			require.NoError(t, err)

			actualYAML, err := yaml.Marshal(conf.ToStringMap())
			require.NoError(t, err)

			resultPath := filepath.Join(testdataDir, entry.Name(), "result.yaml")

			if *update || os.Getenv("UPDATE_GOLDEN") == "true" {
				require.NoError(t, os.WriteFile(resultPath, actualYAML, 0644))
				return
			}

			expectedYAML, err := os.ReadFile(resultPath)
			if err != nil || len(expectedYAML) == 0 {
				require.NoError(t, os.WriteFile(resultPath, actualYAML, 0644))
				expectedYAML = actualYAML
			}

			assert.Equal(t, string(expectedYAML), string(actualYAML))
		})
	}
}
