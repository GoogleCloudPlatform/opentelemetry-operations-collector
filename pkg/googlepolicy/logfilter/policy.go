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

// Package logfilter compiles and evaluates LogFilterPolicy configurations. It
// is the single authoritative implementation for the log signal: the collector
// processor walks the pdata tree and delegates every matching decision here.
package logfilter

import (
	"errors"
	"fmt"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/matcher"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for log filter policies.
const PolicyType = "log_filter"

// driver loads a LogFilterPolicy from either shape it can arrive in: the proto
// a control plane sends, or the map an authored config decodes to.
//
// Exposed to the package (rather than constructed inline in init) so tests
// exercise the very value that is registered.
var driver = googlepolicy.ProtoDriver[*policyv1alpha1.LogFilterPolicy]{
	New: func(pb *policyv1alpha1.LogFilterPolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(err)
	}
}

var (
	// ErrNilProto is returned when a nil LogFilterPolicy protobuf is provided.
	ErrNilProto = errors.New("log filter policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("log filter policy id cannot be empty")
	// ErrMissingAction is returned when the policy action is unspecified or invalid.
	ErrMissingAction = errors.New("log filter policy must specify a valid action (ACTION_KEEP or ACTION_DROP)")
	// ErrMissingMatchers is returned when the policy contains no matchers.
	ErrMissingMatchers = errors.New("log filter policy must contain at least one matcher")
	// ErrMissingTarget is returned when a matcher does not specify a target field selector.
	ErrMissingTarget = errors.New("log matcher must specify a target field selector")
	// ErrMissingPredicate is returned when a matcher does not specify a predicate.
	ErrMissingPredicate = matcher.ErrMissingPredicate
)

// Policy represents a compiled, validated LogFilterPolicy ready for hot-loop evaluation.
type Policy struct {
	proto    *policyv1alpha1.LogFilterPolicy
	matchers []compiledMatcher
}

var _ googlepolicy.TransformationPolicy = (*Policy)(nil)
var _ googlepolicy.LogPolicyEvaluator = (*Policy)(nil)

// NewPolicyFromProto validates and compiles a LogFilterPolicy protobuf into a Policy.
func NewPolicyFromProto(pb *policyv1alpha1.LogFilterPolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}
	if pb.GetId() == "" {
		return nil, ErrMissingID
	}

	switch pb.GetAction() {
	case policyv1alpha1.Action_ACTION_KEEP, policyv1alpha1.Action_ACTION_DROP:
	default:
		return nil, ErrMissingAction
	}

	if len(pb.GetMatches()) == 0 {
		return nil, ErrMissingMatchers
	}

	matchers := make([]compiledMatcher, 0, len(pb.GetMatches()))
	for i, m := range pb.GetMatches() {
		cm, err := compileMatcher(m)
		if err != nil {
			return nil, fmt.Errorf("matcher[%d]: %w", i, err)
		}
		matchers = append(matchers, cm)
	}

	return &Policy{
		proto:    pb,
		matchers: matchers,
	}, nil
}

// PolicyName returns the unique identifier of this policy instance.
func (p *Policy) PolicyName() string {
	return p.proto.GetId()
}

// PolicyType returns the type identifier of this policy ("log_filter").
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns PolicyClassTransformation for log filter policies.
func (p *Policy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}

// TargetSignals returns the telemetry signals targeted by this policy (SignalLogs).
func (p *Policy) TargetSignals() []googlepolicy.Signal {
	return []googlepolicy.Signal{googlepolicy.SignalLogs}
}

// Validate checks whether the compiled policy is valid.
func (p *Policy) Validate() error {
	if p.proto == nil {
		return ErrNilProto
	}
	return nil
}

// Action returns the configured policyv1alpha1.Action (ACTION_KEEP or ACTION_DROP).
func (p *Policy) Action() policyv1alpha1.Action {
	return p.proto.GetAction()
}

// Proto returns the underlying protobuf message for this policy.
func (p *Policy) Proto() proto.Message {
	return p.proto
}

// EvaluateLog evaluates the policy against the given LogContext and returns
// EvalKeep, EvalDrop, or EvalNoMatch.
func (p *Policy) EvaluateLog(ctx googlepolicy.LogContext) googlepolicy.EvalResult {
	if !p.matchesContext(ctx) {
		return googlepolicy.EvalNoMatch
	}
	switch p.Action() {
	case policyv1alpha1.Action_ACTION_KEEP:
		return googlepolicy.EvalKeep
	case policyv1alpha1.Action_ACTION_DROP:
		return googlepolicy.EvalDrop
	default:
		return googlepolicy.EvalNoMatch
	}
}

// matchesContext reports whether every matcher in the policy matches the given
// LogContext. Matchers are AND-ed.
func (p *Policy) matchesContext(ctx googlepolicy.LogContext) bool {
	for i := range p.matchers {
		if !p.matchers[i].eval(ctx) {
			return false
		}
	}
	return true
}

// targetExtractor pulls the value a matcher targets out of a LogContext.
type targetExtractor func(ctx googlepolicy.LogContext) (val any, exists bool)

type compiledMatcher struct {
	extract   targetExtractor
	predicate matcher.Predicate
}

func (cm *compiledMatcher) eval(ctx googlepolicy.LogContext) bool {
	return cm.predicate(cm.extract(ctx))
}

func compileMatcher(m *policyv1alpha1.LogMatcher) (compiledMatcher, error) {
	extract, err := compileExtractor(m.GetTarget())
	if err != nil {
		return compiledMatcher{}, err
	}
	predicate, err := matcher.CompilePredicate(m.GetPredicate(), m.GetNegate())
	if err != nil {
		return compiledMatcher{}, err
	}
	return compiledMatcher{extract: extract, predicate: predicate}, nil
}

func compileExtractor(target *policyv1alpha1.LogFieldSelector) (targetExtractor, error) {
	if target == nil || target.Target == nil {
		return nil, ErrMissingTarget
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.LogFieldSelector_RecordField:
		return compileRecordFieldExtractor(t.RecordField)

	case *policyv1alpha1.LogFieldSelector_LogAttribute:
		steps, err := matcher.CompilePath(t.LogAttribute, "log")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Record == (plog.LogRecord{}) {
				return nil, false
			}
			return matcher.EvalPath(ctx.Record.Attributes(), steps)
		}, nil

	case *policyv1alpha1.LogFieldSelector_ResourceAttribute:
		steps, err := matcher.CompilePath(t.ResourceAttribute, "resource")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Resource == (pcommon.Resource{}) {
				return nil, false
			}
			return matcher.EvalPath(ctx.Resource.Attributes(), steps)
		}, nil

	case *policyv1alpha1.LogFieldSelector_ScopeAttribute:
		steps, err := matcher.CompilePath(t.ScopeAttribute, "scope")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			return matcher.EvalPath(ctx.Scope.Attributes(), steps)
		}, nil

	case *policyv1alpha1.LogFieldSelector_ScopeField:
		return compileScopeFieldExtractor(t.ScopeField)

	default:
		return nil, ErrMissingTarget
	}
}

func compileRecordFieldExtractor(field policyv1alpha1.LogRecordField) (targetExtractor, error) {
	switch field {
	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY:
		return func(ctx googlepolicy.LogContext) (any, bool) {
			if ctx.Record == (plog.LogRecord{}) || ctx.Record.Body().Type() == pcommon.ValueTypeEmpty {
				return nil, false
			}
			val := matcher.ValueToAny(ctx.Record.Body())
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

func compileScopeFieldExtractor(field policyv1alpha1.ScopeField) (targetExtractor, error) {
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
