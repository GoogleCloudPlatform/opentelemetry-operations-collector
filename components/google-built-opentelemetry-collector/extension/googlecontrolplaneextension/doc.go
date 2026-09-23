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

//go:generate mdatagen metadata.yaml

// Package googlecontrolplaneextension reports one self-observability metric
// about the collector's control plane state: whether a policy set is currently
// active, and which one.
//
// Per-policy state is deliberately not reported here. Whether an individual
// policy applied, and why it failed if it did not, is recorded as an OTLP event
// at the point the policy is applied. See policystate.go for why an event fits
// that better than a time series.
//
// This lives in an extension rather than in a pipeline component because the
// facts it reports are process-scoped: there is exactly one active policy set
// per collector, regardless of how many pipelines exist. An extension is
// instantiated exactly once, which gives a single stable time series per
// collector and avoids the cross-pipeline deduplication that a processor-based
// implementation would require.
package googlecontrolplaneextension // import "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/extension/googlecontrolplaneextension"
