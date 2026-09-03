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

module github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider

go 1.26.0

require (
	github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy v0.0.0
	github.com/fsnotify/fsnotify v1.10.1
	github.com/stretchr/testify v1.11.1
	go.opentelemetry.io/collector/confmap v1.65.0
	go.uber.org/goleak v1.3.0
	go.uber.org/zap v1.28.0
)

replace github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy => ../../../../pkg/googlepolicy
