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

package collectorid

import (
	"fmt"
	"os"

	"github.com/google/uuid"
)

var CollectorUUIDNamespace = "5AFFCE1A-3D0A-4220-8EA1-7D64A842BEB4"

var CollectorID string
var CollectorName string

type hostnameFunc func() (string, error)

var getHostname hostnameFunc = os.Hostname

func GenerateCollectorID() error {
	// If there is already a CollectorID, no need to generate one.
	if CollectorID != "" {
		return nil
	}

	// Check COLLECTOR_NAME environment variable. If there's a value,
	// set CollectorName global to that.
	if envName := os.Getenv("COLLECTOR_NAME"); envName != "" {
		CollectorName = envName
	} else {
		// Otherwise, try to detect a hostname from the environment and set the
		// CollectorName to that.
		var err error
		CollectorName, err = getHostname()
		if err != nil {
			return err
		}
	}

	// Set CollectorID to a UUID v5 hash using CollectorUUIDNamespace and
	// the CollectorName.
	namespace, err := uuid.Parse(CollectorUUIDNamespace)
	if err != nil {
		panic(fmt.Sprintf("Should be an impossible code state. Failed to parse Collector UUID Namespace: %v", err))
	}

	CollectorID = uuid.NewSHA1(namespace, []byte(CollectorName)).String()

	return nil
}
