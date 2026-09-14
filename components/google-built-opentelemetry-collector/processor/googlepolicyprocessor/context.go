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

package googlepolicyprocessor

import (
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
)

// The evaluation contexts are owned by pkg/googlepolicy, where the filter
// policies that consume them live. These aliases keep the processor's own call
// sites readable without introducing a second definition.
type (
	// LogContext holds the contextual information needed to evaluate a single log record.
	LogContext = googlepolicy.LogContext
	// MetricContext holds the contextual information needed to evaluate a single metric datapoint.
	MetricContext = googlepolicy.MetricContext
	// TraceContext holds the contextual information needed to evaluate a single trace span.
	TraceContext = googlepolicy.TraceContext
)

// PolicyResult represents the outcome of evaluating a telemetry item.
type PolicyResult string

const (
	ResultDropped     PolicyResult = "dropped"
	ResultKept        PolicyResult = "kept"
	ResultNoMatch     PolicyResult = "no_match"
	ResultTransformed PolicyResult = "transformed"
)

// PolicyEvaluation tracks the evaluation outcome for a specific policy.
type PolicyEvaluation struct {
	PolicyID string
	Result   PolicyResult
}

// TransformStats collects telemetry metrics during a batch transformation pass.
type TransformStats struct {
	Dropped     int64
	Kept        int64
	NoMatch     int64
	Transformed int64

	// PolicyRecords maps policyID -> outcome -> count
	PolicyRecords map[string]map[PolicyResult]int64
}

func newTransformStats() TransformStats {
	return TransformStats{
		PolicyRecords: make(map[string]map[PolicyResult]int64),
	}
}

func (s *TransformStats) recordPolicy(policyID string, res PolicyResult) {
	if s.PolicyRecords[policyID] == nil {
		s.PolicyRecords[policyID] = make(map[PolicyResult]int64)
	}
	s.PolicyRecords[policyID][res]++
}
