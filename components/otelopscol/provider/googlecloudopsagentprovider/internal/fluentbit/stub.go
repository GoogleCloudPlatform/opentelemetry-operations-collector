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

package fluentbit

type Component struct {
	Kind          string
	Config        map[string]string
	OrderedConfig [][2]string
}

type ModularConfig struct {
	Variables  map[string]string
	Components []Component
}

func (c ModularConfig) Generate() (map[string]string, error) {
	return nil, nil
}

const MetricsPort = 20202

func MetricsInputComponent() Component {
	return Component{}
}

func MetricsOutputComponent(port int) Component {
	return Component{}
}

const (
	outputFileKind     = "OPSAGENTOUTPUTFILE"
	outputFileName     = "filename"
	outputFileContents = "contents"
)

func outputFileComponent(name, contents string) Component {
	return Component{
		Kind: outputFileKind,
		Config: map[string]string{
			outputFileName:     name,
			outputFileContents: contents,
		},
	}
}
