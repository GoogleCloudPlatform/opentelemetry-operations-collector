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
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// TransformTraces applies active transformation policies in-place across the Resource -> Scope -> Span hierarchy.
// Dropped spans are pruned, empty scopes/resources are removed, and batch transformation stats are returned.
func (e *Evaluator) TransformTraces(td ptrace.Traces) TransformStats {
	stats := newTransformStats()
	if len(e.tracePolicies) == 0 {
		return stats
	}

	td.ResourceSpans().RemoveIf(func(rs ptrace.ResourceSpans) bool {
		resource := rs.Resource()
		resourceSchemaURL := rs.SchemaUrl()

		rs.ScopeSpans().RemoveIf(func(ss ptrace.ScopeSpans) bool {
			scope := ss.Scope()
			scopeSchemaURL := ss.SchemaUrl()

			var ctx TraceContext
			ctx.Resource = resource
			ctx.Scope = scope
			ctx.ResourceSchemaURL = resourceSchemaURL
			ctx.ScopeSchemaURL = scopeSchemaURL

			ss.Spans().RemoveIf(func(span ptrace.Span) bool {
				ctx.Span = span
				res := e.evaluateTrace(ctx)
				if res.Drop {
					stats.Dropped++
				} else if len(res.Evaluations) > 0 {
					stats.Kept++
				} else {
					stats.NoMatch++
				}
				for _, ev := range res.Evaluations {
					stats.recordPolicy(ev.PolicyID, ev.Result)
				}
				return res.Drop
			})

			return ss.Spans().Len() == 0
		})

		return rs.ScopeSpans().Len() == 0
	})

	return stats
}

type traceEvalResult struct {
	Drop        bool
	Evaluations []PolicyEvaluation
}

type matchedTracePolicy struct {
	id  string
	res googlepolicy.EvalResult
}

func (e *Evaluator) evaluateTrace(ctx TraceContext) traceEvalResult {
	if len(e.tracePolicies) == 0 {
		return traceEvalResult{Drop: false}
	}

	var matchingPolicies []matchedTracePolicy
	var hasKeep, hasDrop bool
	for _, p := range e.tracePolicies {
		evalRes := p.EvaluateTrace(ctx)
		if evalRes != googlepolicy.EvalNoMatch {
			matchingPolicies = append(matchingPolicies, matchedTracePolicy{
				id:  p.PolicyName(),
				res: evalRes,
			})
			if evalRes == googlepolicy.EvalKeep {
				hasKeep = true
			} else if evalRes == googlepolicy.EvalDrop {
				hasDrop = true
			}
		}
	}

	var res traceEvalResult
	if hasKeep {
		res.Drop = false
		for _, p := range matchingPolicies {
			if p.res == googlepolicy.EvalKeep {
				res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultKept})
			} else {
				res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultNoMatch})
			}
		}
	} else if hasDrop {
		res.Drop = true
		for _, p := range matchingPolicies {
			res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultDropped})
		}
	} else {
		res.Drop = false
	}

	return res
}

// EvalTrace returns true if the span should be DROPPED, false if KEPT.
// A matching ACTION_KEEP policy exempts the span outright.
func (e *Evaluator) EvalTrace(ctx TraceContext) bool {
	return e.evaluateTrace(ctx).Drop
}
