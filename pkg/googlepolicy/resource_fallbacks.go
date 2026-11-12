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
	"fmt"
	"strconv"
)

// CloudRegionFallbackStatement returns an OTTL resource-context statement that
// sets cloud.region to region only when the resource carries no location at
// all (no cloud.region, cloud.availability_zone or location attribute).
//
// It is meant to run in a transform processor after resource detection, so a
// region or zone detected for the host (e.g. by the gcp detector on Compute
// Engine) is never replaced, while hosts with nothing detected (off Google
// Cloud) still get the location that telemetry.googleapis.com requires.
// It returns "" if region is empty.
func CloudRegionFallbackStatement(region string) string {
	if region == "" {
		return ""
	}
	return fmt.Sprintf(
		`set(attributes["cloud.region"], %s) where attributes["cloud.region"] == nil and attributes["cloud.availability_zone"] == nil and attributes["location"] == nil`,
		strconv.Quote(region),
	)
}
