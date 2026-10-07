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
	"go.opentelemetry.io/collector/pdata/plog"
)

// TransformLogs applies active transformation policies in-place across the Resource -> Scope -> Record hierarchy.
// Filter policies are evaluated first (Stage 1); surviving log records are then evaluated against
// transform policies (Stage 2) in deterministic policy ID order.
// Dropped records are pruned, empty scopes/resources are removed, and batch transformation stats are returned.
func (e *Evaluator) TransformLogs(ld plog.Logs) TransformStats {
	stats := newTransformStats()
	if len(e.logPolicies) == 0 && len(e.logTransformPolicies) == 0 {
		return stats
	}

	ld.ResourceLogs().RemoveIf(func(rl plog.ResourceLogs) bool {
		resource := rl.Resource()
		resourceSchemaURL := rl.SchemaUrl()

		rl.ScopeLogs().RemoveIf(func(sl plog.ScopeLogs) bool {
			scope := sl.Scope()
			scopeSchemaURL := sl.SchemaUrl()

			var ctx LogContext
			ctx.Resource = resource
			ctx.Scope = scope
			ctx.ResourceSchemaURL = resourceSchemaURL
			ctx.ScopeSchemaURL = scopeSchemaURL

			sl.LogRecords().RemoveIf(func(lr plog.LogRecord) bool {
				ctx.Record = lr
				res := e.evaluateLog(ctx)
				if res.Drop {
					stats.Dropped++
				} else if res.Transformed {
					stats.Transformed++
				} else if res.Kept {
					stats.Kept++
				} else {
					stats.NoMatch++
				}
				for _, ev := range res.Evaluations {
					stats.recordPolicy(ev.PolicyID, ev.Result)
				}
				return res.Drop
			})

			return sl.LogRecords().Len() == 0
		})

		return rl.ScopeLogs().Len() == 0
	})

	return stats
}

type logEvalResult struct {
	Drop        bool
	Kept        bool
	Transformed bool
	Evaluations []PolicyEvaluation
}

type matchedLogPolicy struct {
	id  string
	res googlepolicy.EvalResult
}

func (e *Evaluator) evaluateLog(ctx LogContext) logEvalResult {
	var res logEvalResult

	// Stage 1: Filter policies.
	if len(e.logPolicies) > 0 {
		var matchingPolicies []matchedLogPolicy
		var hasKeep, hasDrop bool
		for _, p := range e.logPolicies {
			evalRes := p.EvaluateLog(ctx)
			if evalRes != googlepolicy.EvalNoMatch {
				matchingPolicies = append(matchingPolicies, matchedLogPolicy{
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

		if hasKeep {
			res.Drop = false
			res.Kept = true
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
			// Dropped records never enter the transform stage.
			return res
		}
	}

	// Stage 2: Transform policies (only evaluated on surviving log records).
	for _, tp := range e.logTransformPolicies {
		if tp.TransformLog(ctx) == googlepolicy.TransformModified {
			res.Transformed = true
			res.Evaluations = append(res.Evaluations, PolicyEvaluation{
				PolicyID: tp.PolicyName(),
				Result:   ResultTransformed,
			})
		}
	}

	return res
}

// EvalLog returns true if the log record should be DROPPED, false if KEPT.
// A matching ACTION_KEEP policy exempts the record outright. Surviving records
// also have any matching LogTransformPolicy rules applied in-place.
func (e *Evaluator) EvalLog(ctx LogContext) bool {
	return e.evaluateLog(ctx).Drop
}
