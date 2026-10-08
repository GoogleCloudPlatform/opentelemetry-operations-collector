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
// Filter policies are evaluated before transforms (Pass 1); surviving log records are evaluated against
// transform policies (Stage 2) in deterministic stage order (rename -> add -> remove -> redact, tie-broken by ID);
// modified log records are then re-evaluated against filter policies (Pass 2).
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

type logFilterPass struct {
	matches []matchedLogPolicy
	hasKeep bool
	hasDrop bool
}

func (e *Evaluator) evalLogFilters(ctx LogContext) logFilterPass {
	var pass logFilterPass
	for _, p := range e.logPolicies {
		evalRes := p.EvaluateLog(ctx)
		if evalRes != googlepolicy.EvalNoMatch {
			pass.matches = append(pass.matches, matchedLogPolicy{
				id:  p.PolicyName(),
				res: evalRes,
			})
			if evalRes == googlepolicy.EvalKeep {
				pass.hasKeep = true
			} else if evalRes == googlepolicy.EvalDrop {
				pass.hasDrop = true
			}
		}
	}
	return pass
}

func appendKeepFilterEvaluations(dst []PolicyEvaluation, matches []matchedLogPolicy) []PolicyEvaluation {
	for _, p := range matches {
		if p.res == googlepolicy.EvalKeep {
			dst = append(dst, PolicyEvaluation{PolicyID: p.id, Result: ResultKept})
		} else {
			dst = append(dst, PolicyEvaluation{PolicyID: p.id, Result: ResultNoMatch})
		}
	}
	return dst
}

func (e *Evaluator) evaluateLog(ctx LogContext) logEvalResult {
	var res logEvalResult

	// Pass 1: Pre-Transform Filter policies.
	var pass1 logFilterPass
	if len(e.logPolicies) > 0 {
		pass1 = e.evalLogFilters(ctx)
		if pass1.hasDrop && !pass1.hasKeep {
			res.Drop = true
			for _, p := range pass1.matches {
				res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultDropped})
			}
			// Dropped records never enter the transform stage.
			return res
		}
	}

	// Stage 2: Transform policies (ordered by rename -> add -> remove -> redact, tie-broken by policy ID).
	for _, tp := range e.logTransformPolicies {
		if tp.TransformLog(ctx) == googlepolicy.TransformModified {
			res.Transformed = true
			res.Evaluations = append(res.Evaluations, PolicyEvaluation{
				PolicyID: tp.PolicyName(),
				Result:   ResultTransformed,
			})
		}
	}

	// Pass 2: Post-Transform Filter policies (only re-evaluated when Stage 2 modified the log record).
	if !res.Transformed {
		if pass1.hasKeep {
			res.Kept = true
			res.Evaluations = appendKeepFilterEvaluations(res.Evaluations, pass1.matches)
		}
		return res
	}

	if len(e.logPolicies) > 0 {
		pass2 := e.evalLogFilters(ctx)
		if pass2.hasDrop && !pass2.hasKeep {
			res.Drop = true
			for _, p := range pass2.matches {
				res.Evaluations = append(res.Evaluations, PolicyEvaluation{PolicyID: p.id, Result: ResultDropped})
			}
			return res
		}
		if pass2.hasKeep {
			res.Kept = true
			res.Evaluations = appendKeepFilterEvaluations(res.Evaluations, pass2.matches)
		} else if pass1.hasKeep {
			res.Kept = true
			res.Evaluations = appendKeepFilterEvaluations(res.Evaluations, pass1.matches)
		}
	}

	return res
}

// EvalLog returns true if the log record should be DROPPED, false if KEPT.
// A matching ACTION_KEEP policy exempts the record outright. Surviving records
// also have any matching LogTransformPolicy rules applied in-place, followed by
// Pass 2 post-transform filtering.
func (e *Evaluator) EvalLog(ctx LogContext) bool {
	return e.evaluateLog(ctx).Drop
}
