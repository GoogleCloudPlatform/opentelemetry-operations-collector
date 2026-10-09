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
	"go.opentelemetry.io/collector/pdata/plog"
)

// ErrMissingLogTarget is returned when a log matcher does not specify a target
// field selector.
var ErrMissingLogTarget = errors.New("log matcher must specify a target field selector")

// LogTargetExtractor pulls the value a log matcher targets out of a LogContext.
type LogTargetExtractor func(ctx googlepolicy.LogContext) (val any, exists bool)

// CompiledLogMatcher is a validated, pre-compiled LogMatcher ready for
// hot-loop evaluation across log policies (e.g., logfilter and logtransform).
type CompiledLogMatcher struct {
	extract   LogTargetExtractor
	predicate Predicate
}

// Eval reports whether the matcher matches the given LogContext.
func (cm *CompiledLogMatcher) Eval(ctx googlepolicy.LogContext) bool {
	return cm.predicate(cm.extract(ctx))
}

// CompileLogMatcher validates and compiles a LogMatcher protobuf into a
// CompiledLogMatcher.
func CompileLogMatcher(m *policyv1alpha1.LogMatcher) (CompiledLogMatcher, error) {
	extract, err := CompileLogExtractor(m.GetTarget())
	if err != nil {
		return CompiledLogMatcher{}, err
	}
	predicate, err := CompilePredicate(m.GetPredicate(), m.GetNegate())
	if err != nil {
		return CompiledLogMatcher{}, err
	}
	return CompiledLogMatcher{extract: extract, predicate: predicate}, nil
}

// CompileLogExtractor validates and compiles a LogFieldSelector into a
// LogTargetExtractor.
func CompileLogExtractor(target *policyv1alpha1.LogFieldSelector) (LogTargetExtractor, error) {
	if target == nil || target.Target == nil {
		return nil, ErrMissingLogTarget
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.LogFieldSelector_RecordField:
		return compileLogRecordFieldExtractor(t.RecordField)

	case *policyv1alpha1.LogFieldSelector_LogAttribute:
		steps, err := CompilePath(t.LogAttribute, "log")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Record == (plog.LogRecord{}) {
				return nil, false
			}
			return EvalPath(ctx.Record.Attributes(), steps)
		}, nil

	case *policyv1alpha1.LogFieldSelector_ResourceAttribute:
		steps, err := CompilePath(t.ResourceAttribute, "resource")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Resource == (pcommon.Resource{}) {
				return nil, false
			}
			return EvalPath(ctx.Resource.Attributes(), steps)
		}, nil

	case *policyv1alpha1.LogFieldSelector_ScopeAttribute:
		steps, err := CompilePath(t.ScopeAttribute, "scope")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			return EvalPath(ctx.Scope.Attributes(), steps)
		}, nil

	case *policyv1alpha1.LogFieldSelector_ScopeField:
		return compileLogScopeFieldExtractor(t.ScopeField)

	default:
		return nil, ErrMissingLogTarget
	}
}

func compileLogRecordFieldExtractor(field policyv1alpha1.LogRecordField) (LogTargetExtractor, error) {
	switch field {
	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY:
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Record == (plog.LogRecord{}) || ctx.Record.Body().Type() == pcommon.ValueTypeEmpty {
				return nil, false
			}
			val := ValueToAny(ctx.Record.Body())
			if s, ok := val.(string); ok {
				if s == "" {
					return nil, false
				}
				return s, true
			}
			return val, true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT:
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Record == (plog.LogRecord{}) {
				return nil, false
			}
			s := ctx.Record.SeverityText()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER:
		// An unspecified severity number (0) is truly absent rather than numeric
		// zero: returning (nil, false) prevents range rules like `lt: 9` (drop
		// debug/trace logs) from falsely dropping logs that have no severity set.
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Record == (plog.LogRecord{}) {
				return nil, false
			}
			sev := ctx.Record.SeverityNumber()
			if sev == plog.SeverityNumberUnspecified {
				return nil, false
			}
			return int64(sev), true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID:
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Record == (plog.LogRecord{}) {
				return nil, false
			}
			tid := ctx.Record.TraceID()
			if tid.IsEmpty() {
				return nil, false
			}
			return tid, true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Record == (plog.LogRecord{}) {
				return nil, false
			}
			sid := ctx.Record.SpanID()
			if sid.IsEmpty() {
				return nil, false
			}
			return sid, true
		}, nil

	default:
		return nil, errors.New("log record field cannot be unspecified")
	}
}

func compileLogScopeFieldExtractor(field policyv1alpha1.ScopeField) (LogTargetExtractor, error) {
	switch field {
	case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
		return func(ctx googlepolicy.LogContext) (any, bool) {
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
		return func(ctx googlepolicy.LogContext) (any, bool) {
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
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.ScopeSchemaURL == "" {
				return nil, false
			}
			return ctx.ScopeSchemaURL, true
		}, nil

	default:
		return nil, errors.New("scope field cannot be unspecified")
	}
}
