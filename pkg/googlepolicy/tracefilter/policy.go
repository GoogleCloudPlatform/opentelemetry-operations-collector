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

// Package tracefilter compiles and evaluates TraceFilterPolicy configurations.
// It is the single authoritative implementation for the trace signal: the
// collector processor walks the pdata tree and delegates every matching
// decision here.
package tracefilter

import (
	"encoding/json"
	"errors"
	"fmt"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/matcher"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for trace filter policies.
const PolicyType = "trace_filter"

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, &Driver{}); err != nil {
		panic(err)
	}
}

// Driver implements googlepolicy.PolicyDriver for loading TraceFilterPolicy configurations.
type Driver struct{}

var _ googlepolicy.ProtoPolicyDriver = (*Driver)(nil)

// PolicyName returns the registered policy type handled by this driver.
func (d *Driver) PolicyName() string {
	return PolicyType
}

// PolicyProto returns the proto message this driver loads from. The registry
// uses its descriptor to route policies that arrive as a bare proto with no
// explicit policy type in the body.
func (d *Driver) PolicyProto() proto.Message {
	return &policyv1alpha1.TraceFilterPolicy{}
}

// LoadPolicy unmarshals a raw policy configuration map into a compiled *Policy.
func (d *Driver) LoadPolicy(raw map[string]any) (googlepolicy.Policy, error) {
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal raw policy map to json: %w", err)
	}

	pb := &policyv1alpha1.TraceFilterPolicy{}
	unmarshaler := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := unmarshaler.Unmarshal(jsonBytes, pb); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into TraceFilterPolicy proto: %w", err)
	}

	return NewPolicyFromProto(pb)
}

var (
	// ErrNilProto is returned when a nil TraceFilterPolicy protobuf is provided.
	ErrNilProto = errors.New("trace filter policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("trace filter policy id cannot be empty")
	// ErrMissingAction is returned when the policy action is unspecified or invalid.
	ErrMissingAction = errors.New("trace filter policy must specify a valid action (ACTION_KEEP or ACTION_DROP)")
	// ErrMissingMatchers is returned when the policy contains no matchers.
	ErrMissingMatchers = errors.New("trace filter policy must contain at least one matcher")
	// ErrMissingTarget is returned when a matcher does not specify a target field selector.
	ErrMissingTarget = errors.New("trace matcher must specify a target field selector")
	// ErrMissingPredicate is returned when a matcher does not specify a predicate.
	ErrMissingPredicate = matcher.ErrMissingPredicate
)

// Policy represents a compiled, validated TraceFilterPolicy ready for hot-loop evaluation.
type Policy struct {
	proto    *policyv1alpha1.TraceFilterPolicy
	matchers []compiledMatcher
}

var _ googlepolicy.TransformationPolicy = (*Policy)(nil)
var _ googlepolicy.TracePolicyEvaluator = (*Policy)(nil)

// NewPolicyFromProto validates and compiles a TraceFilterPolicy protobuf into a Policy.
func NewPolicyFromProto(pb *policyv1alpha1.TraceFilterPolicy) (*Policy, error) {
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

// PolicyType returns the type identifier of this policy ("trace_filter").
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns PolicyClassTransformation for trace filter policies.
func (p *Policy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}

// TargetSignals returns the telemetry signals targeted by this policy (SignalTraces).
func (p *Policy) TargetSignals() []googlepolicy.Signal {
	return []googlepolicy.Signal{googlepolicy.SignalTraces}
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

// EvaluateTrace evaluates the policy against the given TraceContext and returns
// EvalKeep, EvalDrop, or EvalNoMatch.
func (p *Policy) EvaluateTrace(ctx googlepolicy.TraceContext) googlepolicy.EvalResult {
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
// TraceContext. Matchers are AND-ed.
func (p *Policy) matchesContext(ctx googlepolicy.TraceContext) bool {
	for i := range p.matchers {
		if !p.matchers[i].eval(ctx) {
			return false
		}
	}
	return true
}

// targetExtractor pulls the value a matcher targets out of a TraceContext.
type targetExtractor func(ctx googlepolicy.TraceContext) (val any, exists bool)

type compiledMatcher struct {
	extract   targetExtractor
	predicate matcher.Predicate
}

func (cm *compiledMatcher) eval(ctx googlepolicy.TraceContext) bool {
	return cm.predicate(cm.extract(ctx))
}

func compileMatcher(m *policyv1alpha1.TraceMatcher) (compiledMatcher, error) {
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

func compileExtractor(target *policyv1alpha1.TraceFieldSelector) (targetExtractor, error) {
	if target == nil || target.Target == nil {
		return nil, ErrMissingTarget
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.TraceFieldSelector_RecordField:
		return compileRecordFieldExtractor(t.RecordField)

	case *policyv1alpha1.TraceFieldSelector_SpanAttribute:
		steps, err := matcher.CompilePath(t.SpanAttribute, "span")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Span == (ptrace.Span{}) {
				return nil, false
			}
			return matcher.EvalPath(ctx.Span.Attributes(), steps)
		}, nil

	case *policyv1alpha1.TraceFieldSelector_ResourceAttribute:
		steps, err := matcher.CompilePath(t.ResourceAttribute, "resource")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Resource == (pcommon.Resource{}) {
				return nil, false
			}
			return matcher.EvalPath(ctx.Resource.Attributes(), steps)
		}, nil

	case *policyv1alpha1.TraceFieldSelector_ScopeAttribute:
		steps, err := matcher.CompilePath(t.ScopeAttribute, "scope")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.TraceContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			return matcher.EvalPath(ctx.Scope.Attributes(), steps)
		}, nil

	case *policyv1alpha1.TraceFieldSelector_ScopeField:
		return compileScopeFieldExtractor(t.ScopeField)

	default:
		return nil, ErrMissingTarget
	}
}

func compileRecordFieldExtractor(field policyv1alpha1.SpanRecordField) (targetExtractor, error) {
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

func compileScopeFieldExtractor(field policyv1alpha1.ScopeField) (targetExtractor, error) {
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
