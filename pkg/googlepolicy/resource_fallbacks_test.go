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

package googlepolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCloudRegionFallbackStatement(t *testing.T) {
	assert.Empty(t, CloudRegionFallbackStatement(""))
	assert.Equal(t,
		`set(attributes["cloud.region"], "us-east1") where attributes["cloud.region"] == nil and attributes["cloud.availability_zone"] == nil and attributes["location"] == nil`,
		CloudRegionFallbackStatement("us-east1"))
	// Quotes are escaped so the value stays a single OTTL string literal.
	assert.Contains(t, CloudRegionFallbackStatement(`a"b`), `"a\"b"`)
}
