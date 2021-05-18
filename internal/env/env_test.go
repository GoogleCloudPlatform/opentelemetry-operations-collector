// Copyright 2020 Google LLC
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

package env

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Create(t *testing.T) {
	require.NoError(t, Create())

	expectedUserAgentRegex := fmt.Sprintf(`^Google Cloud Metrics Agent/latest \(TargetPlatform=(?i:%v); Framework=OpenTelemetry Collector\) .* \(Cores=\d+; Memory=(?:[0-9]*[.])?[0-9]+GB; Disk=(?:[0-9]*[.])?[0-9]+GB\)$`, runtime.GOOS)
	assert.Regexp(t, expectedUserAgentRegex, os.Getenv("USERAGENT"))
}
