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
// Filter policies are evaluated first (Pass 1); surviving log records are then evaluated against
// transform policies in deterministic stage order (rename -> add -> remove -> redact). If any transform modifies a record,
// filter policies are re-evaluated (Pass 2). Dropped records are pruned, and empty scopes/resources are removed.
func (e *Evaluator) TransformLogs(ld plog.Logs) {
	if len(e.logFilterPolicies) == 0 && len(e.logTransformPolicies) == 0 {
		return
	}

	ld.ResourceLogs().RemoveIf(func(rl plog.ResourceLogs) bool {
		resource := rl.Resource()
		resourceSchemaURL := rl.SchemaUrl()

		rl.ScopeLogs().RemoveIf(func(sl plog.ScopeLogs) bool {
			scope := sl.Scope()
			scopeSchemaURL := sl.SchemaUrl()

			sl.LogRecords().RemoveIf(func(lr plog.LogRecord) bool {
				ctx := LogContext{
					Record:            lr,
					Resource:          resource,
					Scope:             scope,
					ResourceSchemaURL: resourceSchemaURL,
					ScopeSchemaURL:    scopeSchemaURL,
				}
				return e.EvalLog(ctx)
			})

			return sl.LogRecords().Len() == 0
		})

		return rl.ScopeLogs().Len() == 0
	})
}

// EvalLog returns true if the log record should be DROPPED, false if KEPT.
// Filter policies are evaluated first (Pass 1); dropped records never enter transformation.
// Surviving records evaluate each LogTransformPolicy's matchers once against the
// pre-transform record, then execute matching policies' operations in global stage
// order (rename -> add -> remove -> redact) with ascending PolicyName() tie-breaking.
// If any transform policy modified the record, filter policies are re-evaluated
// (Pass 2) against the post-transform record before egress.
func (e *Evaluator) EvalLog(ctx LogContext) bool {
	// Pass 1: Pre-transform filter policies.
	if e.evalLogFilters(ctx) {
		return true
	}

	if len(e.logTransformPolicies) == 0 {
		return false
	}

	// Step 1: Pre-transform matcher evaluation across all active transform policies
	// (sorted by ascending PolicyName() in NewEvaluator) before any stage mutates ctx.
	var matchedBuf [16]googlepolicy.LogTransformPolicyEvaluator
	matched := matchedBuf[:0]
	for _, tp := range e.logTransformPolicies {
		if tp.MatchesLog(ctx) {
			matched = append(matched, tp)
		}
	}
	if len(matched) == 0 {
		return false
	}

	// Step 2: Execute operations across matching policies in global stage order:
	// Rename -> Add -> Remove -> Redact, preserving ascending PolicyName() tie-breaking.
	var modified bool
	for _, stage := range [...]googlepolicy.TransformStage{
		googlepolicy.TransformStageRename,
		googlepolicy.TransformStageAdd,
		googlepolicy.TransformStageRemove,
		googlepolicy.TransformStageRedact,
	} {
		for _, tp := range matched {
			if tp.TransformLogStage(stage, ctx) == googlepolicy.TransformModified {
				modified = true
			}
		}
	}

	// Pass 2: Post-transform filter policies (only re-evaluated if a transform modified the record).
	if modified && len(e.logFilterPolicies) > 0 {
		if e.evalLogFilters(ctx) {
			return true
		}
	}

	return false
}

// evalLogFilters evaluates all active LogFilterPolicies against ctx.
// Returns true if the record matches at least one ACTION_DROP policy and zero ACTION_KEEP policies.
func (e *Evaluator) evalLogFilters(ctx LogContext) bool {
	var hasDrop bool
	for _, p := range e.logFilterPolicies {
		switch p.FilterLog(ctx) {
		case googlepolicy.FilterKeep:
			return false
		case googlepolicy.FilterDrop:
			hasDrop = true
		}
	}
	return hasDrop
}
