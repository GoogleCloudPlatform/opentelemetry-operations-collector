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
// Dropped records are pruned, and empty scopes/resources are removed.
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
// A matching ACTION_KEEP policy exempts the record outright. Surviving records
// also have any matching LogTransformPolicy rules applied in-place.
func (e *Evaluator) EvalLog(ctx LogContext) bool {
	// Stage 1: Filter policies.
	var hasDrop, hasKeep bool
	for _, p := range e.logFilterPolicies {
		switch p.FilterLog(ctx) {
		case googlepolicy.FilterKeep:
			hasKeep = true
		case googlepolicy.FilterDrop:
			hasDrop = true
		}
	}

	if hasDrop && !hasKeep {
		// Dropped records never enter Stage 2.
		return true
	}

	// Stage 2: Transform policies (only evaluated on surviving log records).
	for _, tp := range e.logTransformPolicies {
		tp.TransformLog(ctx)
	}

	return false
}
