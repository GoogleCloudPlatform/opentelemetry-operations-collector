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
	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type compiledTracePolicy struct {
	id       string
	action   policyv1alpha1.Action
	matchers []*compiledTraceMatcher
}

type compiledTraceMatcher struct {
	target *policyv1alpha1.TraceFieldSelector
	pred   matcherPredicate
}

func compileTracePolicy(p *policyv1alpha1.TraceFilterPolicy) (*compiledTracePolicy, error) {
	cp := &compiledTracePolicy{
		id:     p.GetId(),
		action: p.GetAction(),
	}
	for _, m := range p.GetMatches() {
		pred, err := compilePredicate(m.GetPredicate(), m.GetNegate())
		if err != nil {
			return nil, err
		}
		cp.matchers = append(cp.matchers, &compiledTraceMatcher{
			target: m.GetTarget(),
			pred:   pred,
		})
	}
	return cp, nil
}

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
				ctx := TraceContext{
					Span:              span,
					Resource:          resource,
					Scope:             scope,
					ResourceSchemaURL: resourceSchemaURL,
					ScopeSchemaURL:    scopeSchemaURL,
				}
				return e.EvalTrace(ctx)
			})

			return ss.Spans().Len() == 0
		})

		return rs.ScopeSpans().Len() == 0
	})
}

// EvalTrace returns true if the span should be DROPPED, false if KEPT.
func (e *Evaluator) EvalTrace(ctx TraceContext) bool {
	if len(e.tracePolicies) == 0 {
		return false
	}

	var hasKeep, hasDrop bool
	for _, p := range e.tracePolicies {
		if p.matches(ctx) {
			if p.action == policyv1alpha1.Action_ACTION_KEEP {
				hasKeep = true
			} else if p.action == policyv1alpha1.Action_ACTION_DROP {
				hasDrop = true
			}
		}
	}

	if hasKeep {
		return false
	}
	if hasDrop {
		return true
	}
	return false
}

func (p *compiledTracePolicy) matches(ctx TraceContext) bool {
	for _, cm := range p.matchers {
		val, exists := extractTraceTarget(ctx, cm.target)
		if !cm.pred.evaluate(val, exists) {
			return false
		}
	}
	return true
}

func extractTraceTarget(ctx TraceContext, target *policyv1alpha1.TraceFieldSelector) (any, bool) {
	if target == nil {
		return nil, false
	}
	switch t := target.Target.(type) {
	case *policyv1alpha1.TraceFieldSelector_RecordField:
		switch t.RecordField {
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME:
			s := ctx.Span.Name()
			return s, s != ""
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID:
			tid := ctx.Span.TraceID()
			if tid.IsEmpty() {
				return nil, false
			}
			return tid.String(), true
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID:
			sid := ctx.Span.SpanID()
			if sid.IsEmpty() {
				return nil, false
			}
			return sid.String(), true
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID:
			psid := ctx.Span.ParentSpanID()
			if psid.IsEmpty() {
				return nil, false
			}
			return psid.String(), true
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE:
			s := ctx.Span.Status().Message()
			return s, s != ""
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND:
			return spanKindToString(ctx.Span.Kind()), true
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE:
			return spanStatusToString(ctx.Span.Status().Code()), true
		default:
			return nil, false
		}
	case *policyv1alpha1.TraceFieldSelector_SpanAttribute:
		return lookupPath(ctx.Span.Attributes(), t.SpanAttribute.GetPath())
	case *policyv1alpha1.TraceFieldSelector_ResourceAttribute:
		return lookupPath(ctx.Resource.Attributes(), t.ResourceAttribute.GetPath())
	case *policyv1alpha1.TraceFieldSelector_ScopeAttribute:
		return lookupPath(ctx.Scope.Attributes(), t.ScopeAttribute.GetPath())
	case *policyv1alpha1.TraceFieldSelector_ScopeField:
		switch t.ScopeField {
		case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
			s := ctx.Scope.Name()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
			s := ctx.Scope.Version()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
			return ctx.ScopeSchemaURL, ctx.ScopeSchemaURL != ""
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

func spanKindToString(k ptrace.SpanKind) string {
	switch k {
	case ptrace.SpanKindInternal:
		return "INTERNAL"
	case ptrace.SpanKindServer:
		return "SERVER"
	case ptrace.SpanKindClient:
		return "CLIENT"
	case ptrace.SpanKindProducer:
		return "PRODUCER"
	case ptrace.SpanKindConsumer:
		return "CONSUMER"
	default:
		return "INTERNAL"
	}
}

func spanStatusToString(c ptrace.StatusCode) string {
	switch c {
	case ptrace.StatusCodeOk:
		return "OK"
	case ptrace.StatusCodeError:
		return "ERROR"
	default:
		return "UNSET"
	}
}
