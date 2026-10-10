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

package matcher

import (
	"errors"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// ErrMissingTraceTarget is returned when a trace matcher does not specify a
// target field selector.
var ErrMissingTraceTarget = errors.New("trace matcher must specify a target field selector")

// TraceTargetExtractor pulls the value a trace matcher targets out of a
// TraceContext.
type TraceTargetExtractor func(ctx googlepolicy.TraceContext) (val any, exists bool)

// CompiledTraceMatcher is a validated, pre-compiled TraceMatcher ready for
// hot-loop evaluation across trace policies (e.g., tracefilter and
// tracetransform).
type CompiledTraceMatcher struct {
	extract   TraceTargetExtractor
	predicate Predicate
}

// Eval reports whether the matcher matches the given TraceContext.
func (cm *CompiledTraceMatcher) Eval(ctx googlepolicy.TraceContext) bool {
	return cm.predicate(cm.extract(ctx))
}

// CompileTraceMatcher validates and compiles a TraceMatcher protobuf into a
// CompiledTraceMatcher.
func CompileTraceMatcher(m *policyv1alpha1.TraceMatcher) (CompiledTraceMatcher, error) {
	extract, err := CompileTraceExtractor(m.GetTarget())
	if err != nil {
		return CompiledTraceMatcher{}, err
	}
	predicate, err := CompilePredicate(m.GetPredicate(), m.GetNegate())
	if err != nil {
		return CompiledTraceMatcher{}, err
	}
	return CompiledTraceMatcher{extract: extract, predicate: predicate}, nil
}

// CompileTraceExtractor validates and compiles a TraceFieldSelector into a
// TraceTargetExtractor.
func CompileTraceExtractor(target *policyv1alpha1.TraceFieldSelector) (TraceTargetExtractor, error) {
	if target == nil || target.Target == nil {
		return nil, ErrMissingTraceTarget
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.TraceFieldSelector_RecordField:
		return compileSpanRecordFieldExtractor(t.RecordField)

	case *policyv1alpha1.TraceFieldSelector_SpanAttribute:
		steps, err := CompilePath(t.SpanAttribute, "span")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Span == (ptrace.Span{}) {
				return nil, false
			}
			return EvalPath(ctx.Span.Attributes(), steps)
		}, nil

	case *policyv1alpha1.TraceFieldSelector_ResourceAttribute:
		steps, err := CompilePath(t.ResourceAttribute, "resource")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Resource == (pcommon.Resource{}) {
				return nil, false
			}
			return EvalPath(ctx.Resource.Attributes(), steps)
		}, nil

	case *policyv1alpha1.TraceFieldSelector_ScopeAttribute:
		steps, err := CompilePath(t.ScopeAttribute, "scope")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			return EvalPath(ctx.Scope.Attributes(), steps)
		}, nil

	case *policyv1alpha1.TraceFieldSelector_ScopeField:
		return compileTraceScopeFieldExtractor(t.ScopeField)

	default:
		return nil, ErrMissingTraceTarget
	}
}

func compileSpanRecordFieldExtractor(field policyv1alpha1.SpanRecordField) (TraceTargetExtractor, error) {
	switch field {
	case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME:
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Span == (ptrace.Span{}) {
				return nil, false
			}
			s := ctx.Span.Name()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID:
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Span == (ptrace.Span{}) {
				return nil, false
			}
			tid := ctx.Span.TraceID()
			if tid.IsEmpty() {
				return nil, false
			}
			return tid, true
		}, nil

	case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID:
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Span == (ptrace.Span{}) {
				return nil, false
			}
			sid := ctx.Span.SpanID()
			if sid.IsEmpty() {
				return nil, false
			}
			return sid, true
		}, nil

	case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID:
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Span == (ptrace.Span{}) {
				return nil, false
			}
			psid := ctx.Span.ParentSpanID()
			if psid.IsEmpty() {
				// A root span has no parent. The proto requires `exists` to
				// report false here, yet string predicates must still see the
				// empty string, so return a present-but-empty value. The
				// distinction is carried by the value being non-nil while
				// exists is false.
				return "", false
			}
			return psid, true
		}, nil

	case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE:
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Span == (ptrace.Span{}) {
				return nil, false
			}
			s := ctx.Span.Status().Message()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND:
		// Unlike the other first-class fields, an unset kind is NOT reported as
		// absent: trace_filter_policy.proto states "if span kind is unset or
		// SPAN_KIND_UNSPECIFIED, implementations treat it as INTERNAL", so
		// INTERNAL is a real value rather than a default placeholder.
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Span == (ptrace.Span{}) {
				return nil, false
			}
			return SpanKindString(ctx.Span.Kind()), true
		}, nil

	case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE:
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Span == (ptrace.Span{}) {
				return nil, false
			}
			code := ctx.Span.Status().Code()
			return SpanStatusString(code), code != ptrace.StatusCodeUnset
		}, nil

	default:
		return nil, errors.New("span record field cannot be unspecified")
	}
}

func compileTraceScopeFieldExtractor(field policyv1alpha1.ScopeField) (TraceTargetExtractor, error) {
	switch field {
	case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			s := ctx.Scope.Name()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			s := ctx.Scope.Version()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.ScopeSchemaURL == "" {
				return nil, false
			}
			return ctx.ScopeSchemaURL, true
		}, nil

	default:
		return nil, errors.New("scope field cannot be unspecified")
	}
}

// SpanKindString renders a span kind using the spelling that policies match
// against. Unset kinds report as INTERNAL, matching the OTLP default.
func SpanKindString(k ptrace.SpanKind) string {
	switch k {
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

// SpanStatusString renders a span status code using the spelling that policies
// match against.
func SpanStatusString(c ptrace.StatusCode) string {
	switch c {
	case ptrace.StatusCodeOk:
		return "OK"
	case ptrace.StatusCodeError:
		return "ERROR"
	default:
		return "UNSET"
	}
}
