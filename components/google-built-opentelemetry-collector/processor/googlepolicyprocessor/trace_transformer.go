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
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/tracesampling"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// TransformTraces applies active transformation policies in-place across the Resource -> Scope -> Span hierarchy.
// Dropped spans are pruned, and empty scopes/resources are removed.
func (e *Evaluator) TransformTraces(td ptrace.Traces) {
	if len(e.traceFilterPolicies) == 0 && len(e.traceSamplingPolicies) == 0 {
		return
	}

	td.ResourceSpans().RemoveIf(func(rs ptrace.ResourceSpans) bool {
		resource := rs.Resource()
		resourceSchemaURL := rs.SchemaUrl()

		rs.ScopeSpans().RemoveIf(func(ss ptrace.ScopeSpans) bool {
			scope := ss.Scope()
			scopeSchemaURL := ss.SchemaUrl()

			ss.Spans().RemoveIf(func(span ptrace.Span) bool {
				ctx := TraceContext{
					Span:              span,
					Resource:          resource,
					Scope:             scope,
					ResourceSchemaURL: resourceSchemaURL,
					ScopeSchemaURL:    scopeSchemaURL,
				}

				// Stage 1: Deterministic filter evaluation (TraceFilterPolicy)
				if e.EvalTrace(ctx) {
					return true
				}

				// Stage 2: Probabilistic trace sampling evaluation (TraceSamplingPolicy)
				if e.EvalTraceSampling(ctx) {
					return true
				}

				return false
			})

			return ss.Spans().Len() == 0
		})

		return rs.ScopeSpans().Len() == 0
	})
}

// EvalTrace returns true if the span should be DROPPED, false if KEPT.
// All matching trace filter policies drop the span.
func (e *Evaluator) EvalTrace(ctx TraceContext) bool {
	for _, p := range e.traceFilterPolicies {
		if p.FilterTrace(ctx) == googlepolicy.FilterDrop {
			return true
		}
	}
	return false
}

// EvalTraceSampling evaluates active TraceSamplingPolicy rules against a single trace span
// in equalizing mode.
// Returns true if the span should be DROPPED, false if KEPT.
func (e *Evaluator) EvalTraceSampling(ctx TraceContext) bool {
	if len(e.traceSamplingPolicies) == 0 {
		return false
	}

	// Multi-policy composition: effective sampling rate is min(p_i),
	// which corresponds to the maximum threshold max(T_i).
	// Find the maximum threshold across matching policies in a single pass without heap allocation.
	var matched bool
	var maxThreshold uint64
	for _, p := range e.traceSamplingPolicies {
		if p.MatchesTrace(ctx) {
			if !matched || p.Threshold() > maxThreshold {
				maxThreshold = p.Threshold()
			}
			matched = true
		}
	}

	// Default allow: if no policy matches, pass the span through unsampled.
	if !matched {
		return false
	}

	decision, _, err := tracesampling.EvaluateSampling(ctx.Span, maxThreshold)
	if err != nil {
		// Fail open on unexpected evaluation errors.
		return false
	}
	return decision == tracesampling.DecisionDrop
}
