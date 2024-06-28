// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dcgmreceiver

import (
	"time"

	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/receiver/scraperhelper"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/receiver/dcgmreceiver/internal/metadata"
)

const defaultEndpoint = "localhost:5555"
const defaultCollectionInterval = 20 * time.Second

type Config struct {
	scraperhelper.ControllerConfig `mapstructure:",squash"`
	confignet.TCPAddrConfig        `mapstructure:",squash"`
	Metrics                        metadata.MetricsConfig `mapstructure:"metrics"`
	retryBlankValues               bool
	maxRetries                     int
}
