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
// Dropped spans are pruned, and empty scopes/resources are removed.
func (e *Evaluator) TransformTraces(td ptrace.Traces) {
	if len(e.tracePolicies) == 0 {
		return
	}

	td.ResourceSpans().RemoveIf(func(rs ptrace.ResourceSpans) bool {
		resource := rs.Resource()
		resourceSchemaURL := rs.SchemaUrl()

		rs.ScopeSpans().RemoveIf(func(ss ptrace.ScopeSpans) bool {
			scope := ss.Scope()
			scopeSchemaURL := ss.SchemaUrl()

			ss.Spans().RemoveIf(func(span ptrace.Span) bool {
				return e.EvalTrace(TraceContext{
					Span:              span,
					Resource:          resource,
					Scope:             scope,
					ResourceSchemaURL: resourceSchemaURL,
					ScopeSchemaURL:    scopeSchemaURL,
				})
			})

			return ss.Spans().Len() == 0
		})

		return rs.ScopeSpans().Len() == 0
	})
}

// EvalTrace returns true if the span should be DROPPED, false if KEPT.
// A matching ACTION_KEEP policy exempts the span outright.
func (e *Evaluator) EvalTrace(ctx TraceContext) bool {
	var hasDrop bool
	for _, p := range e.tracePolicies {
		switch p.EvaluateTrace(ctx) {
		case googlepolicy.EvalKeep:
			return false
		case googlepolicy.EvalDrop:
			hasDrop = true
		}
	}
	return hasDrop
}
