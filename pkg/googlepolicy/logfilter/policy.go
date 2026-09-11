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

package logfilter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	PolicyTypeShort     = "log_filter"
	PolicyTypeProto     = "google.telemetry.policy.v1alpha1.LogFilterPolicy"
	PolicyTypeCanonical = "type.googleapis.com/google.telemetry.policy.v1alpha1.LogFilterPolicy"
)

func init() {
	_ = googlepolicy.RegisterPolicyDriver(PolicyTypeShort, &Driver{})
	_ = googlepolicy.RegisterPolicyDriver(PolicyTypeProto, &Driver{})
	_ = googlepolicy.RegisterPolicyDriver(PolicyTypeCanonical, &Driver{})
}

var (
	ErrMissingAction    = errors.New("policy action must be specified (ACTION_KEEP or ACTION_DROP)")
	ErrMissingMatches   = errors.New("policy must contain at least one matcher")
	ErrMissingTarget    = errors.New("log matcher must specify a target selector")
	ErrMissingPredicate = errors.New("log matcher must specify a predicate")
)

// Driver loads and validates LogFilterPolicy objects from raw configurations.
type Driver struct{}

var _ googlepolicy.PolicyDriver = (*Driver)(nil)

// LoadPolicy parses a raw JSON/map configuration into a compiled Policy.
func (d *Driver) LoadPolicy(raw map[string]any) (googlepolicy.Policy, error) {
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal raw policy: %w", err)
	}

	pbPolicy := &policyv1alpha1.LogFilterPolicy{}
	unmarshalOpts := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := unmarshalOpts.Unmarshal(jsonBytes, pbPolicy); err != nil {
		return nil, fmt.Errorf("failed to unmarshal LogFilterPolicy: %w", err)
	}

	return NewPolicyFromProto(pbPolicy)
}

// Policy is the in-memory compiled representation of a LogFilterPolicy.
type Policy struct {
	proto    *policyv1alpha1.LogFilterPolicy
	matchers []compiledMatcher
}

var (
	_ googlepolicy.Policy               = (*Policy)(nil)
	_ googlepolicy.TransformationPolicy = (*Policy)(nil)
	_ googlepolicy.LogRecordFilter      = (*Policy)(nil)
)

// NewPolicyFromProto validates and compiles a protobuf LogFilterPolicy into an evaluatable Policy.
func NewPolicyFromProto(pb *policyv1alpha1.LogFilterPolicy) (*Policy, error) {
	if pb == nil {
		return nil, errors.New("nil LogFilterPolicy proto")
	}

	if pb.GetAction() == policyv1alpha1.Action_ACTION_UNSPECIFIED {
		return nil, ErrMissingAction
	}

	rawMatches := pb.GetMatches()
	if len(rawMatches) == 0 {
		return nil, ErrMissingMatches
	}

	matchers := make([]compiledMatcher, 0, len(rawMatches))
	for i, m := range rawMatches {
		cm, err := compileMatcher(m)
		if err != nil {
			return nil, fmt.Errorf("matcher at index %d invalid: %w", i, err)
		}
		matchers = append(matchers, cm)
	}

	return &Policy{
		proto:    pb,
		matchers: matchers,
	}, nil
}

func (p *Policy) PolicyName() string {
	return p.proto.GetId()
}

func (p *Policy) PolicyType() string {
	return PolicyTypeShort
}

func (p *Policy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}

func (p *Policy) TargetSignals() []googlepolicy.Signal {
	return []googlepolicy.Signal{googlepolicy.SignalLogs}
}

func (p *Policy) Validate() error {
	if p.proto.GetAction() == policyv1alpha1.Action_ACTION_UNSPECIFIED {
		return ErrMissingAction
	}
	if len(p.matchers) == 0 {
		return ErrMissingMatches
	}
	return nil
}

// Action returns the configured filtering action (KEEP or DROP).
func (p *Policy) Action() policyv1alpha1.Action {
	return p.proto.GetAction()
}

// Proto returns the underlying protobuf message.
func (p *Policy) Proto() *policyv1alpha1.LogFilterPolicy {
	return p.proto
}

// Matches evaluates all matchers in the policy against the given log record, scope, and resource.
// Per specification, all matchers are ANDed together: all must evaluate to true for the policy to match.
func (p *Policy) Matches(log plog.LogRecord, scope plog.ScopeLogs, res plog.ResourceLogs) bool {
	for _, m := range p.matchers {
		if !m.eval(log, scope, res) {
			return false
		}
	}
	return true
}

// ShouldDropLog returns true if the given log record should be dropped according to this policy.
// For ACTION_DROP: returns true if the log matches all conditions.
// For ACTION_KEEP: returns true if the log does NOT match all conditions (i.e. not kept).
func (p *Policy) ShouldDropLog(log plog.LogRecord, scope plog.ScopeLogs, res plog.ResourceLogs) bool {
	matches := p.Matches(log, scope, res)
	switch p.Action() {
	case policyv1alpha1.Action_ACTION_DROP:
		return matches
	case policyv1alpha1.Action_ACTION_KEEP:
		return !matches
	default:
		return false
	}
}

type compiledMatcher struct {
	target    *policyv1alpha1.LogFieldSelector
	predicate func(val pcommon.Value, exists bool) bool
	negate    bool
}

func (cm *compiledMatcher) eval(log plog.LogRecord, scope plog.ScopeLogs, res plog.ResourceLogs) bool {
	val, exists := extractTargetValue(cm.target, log, scope, res)
	result := cm.predicate(val, exists)
	if cm.negate {
		return !result
	}
	return result
}

func compileMatcher(m *policyv1alpha1.LogMatcher) (compiledMatcher, error) {
	if m.GetTarget() == nil || m.GetTarget().Target == nil {
		return compiledMatcher{}, ErrMissingTarget
	}

	switch t := m.GetTarget().Target.(type) {
	case *policyv1alpha1.LogFieldSelector_LogAttribute:
		if t.LogAttribute == nil || len(t.LogAttribute.GetPath()) == 0 {
			return compiledMatcher{}, errors.New("log attribute path cannot be empty")
		}
	case *policyv1alpha1.LogFieldSelector_ResourceAttribute:
		if t.ResourceAttribute == nil || len(t.ResourceAttribute.GetPath()) == 0 {
			return compiledMatcher{}, errors.New("resource attribute path cannot be empty")
		}
	case *policyv1alpha1.LogFieldSelector_ScopeAttribute:
		if t.ScopeAttribute == nil || len(t.ScopeAttribute.GetPath()) == 0 {
			return compiledMatcher{}, errors.New("scope attribute path cannot be empty")
		}
	}

	if m.GetPredicate() == nil {
		return compiledMatcher{}, ErrMissingPredicate
	}

	var predFunc func(val pcommon.Value, exists bool) bool

	switch p := m.GetPredicate().(type) {
	case *policyv1alpha1.LogMatcher_Exists:
		predFunc = func(val pcommon.Value, exists bool) bool {
			return exists && val.Type() != pcommon.ValueTypeEmpty
		}

	case *policyv1alpha1.LogMatcher_Equals:
		if p.Equals == nil || p.Equals.GetValue() == nil {
			return compiledMatcher{}, ErrMissingPredicate
		}
		expected := p.Equals
		predFunc = func(val pcommon.Value, exists bool) bool {
			return exists && matchValueEquals(val, expected)
		}

	case *policyv1alpha1.LogMatcher_Regex:
		if p.Regex == "" {
			return compiledMatcher{}, errors.New("regex predicate cannot be empty")
		}
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return compiledMatcher{}, fmt.Errorf("invalid regex %q: %w", p.Regex, err)
		}
		predFunc = func(val pcommon.Value, exists bool) bool {
			if !exists {
				return false
			}
			return matchRegex(val, re)
		}

	case *policyv1alpha1.LogMatcher_Gt:
		if p.Gt == nil || p.Gt.GetValue() == nil {
			return compiledMatcher{}, ErrMissingPredicate
		}
		num := p.Gt
		predFunc = func(val pcommon.Value, exists bool) bool {
			return exists && compareNumeric(val, num) == 1
		}

	case *policyv1alpha1.LogMatcher_Gte:
		if p.Gte == nil || p.Gte.GetValue() == nil {
			return compiledMatcher{}, ErrMissingPredicate
		}
		num := p.Gte
		predFunc = func(val pcommon.Value, exists bool) bool {
			if !exists {
				return false
			}
			cmp := compareNumeric(val, num)
			return cmp == 1 || cmp == 0
		}

	case *policyv1alpha1.LogMatcher_Lt:
		if p.Lt == nil || p.Lt.GetValue() == nil {
			return compiledMatcher{}, ErrMissingPredicate
		}
		num := p.Lt
		predFunc = func(val pcommon.Value, exists bool) bool {
			return exists && compareNumeric(val, num) == -1
		}

	case *policyv1alpha1.LogMatcher_Lte:
		if p.Lte == nil || p.Lte.GetValue() == nil {
			return compiledMatcher{}, ErrMissingPredicate
		}
		num := p.Lte
		predFunc = func(val pcommon.Value, exists bool) bool {
			if !exists {
				return false
			}
			cmp := compareNumeric(val, num)
			return cmp == -1 || cmp == 0
		}

	case *policyv1alpha1.LogMatcher_Contains:
		if p.Contains == nil || p.Contains.GetValue() == nil {
			return compiledMatcher{}, ErrMissingPredicate
		}
		needle := p.Contains
		predFunc = func(val pcommon.Value, exists bool) bool {
			return exists && matchValueContains(val, needle)
		}

	default:
		return compiledMatcher{}, ErrMissingPredicate
	}

	return compiledMatcher{
		target:    m.GetTarget(),
		predicate: predFunc,
		negate:    m.GetNegate(),
	}, nil
}

func extractTargetValue(
	target *policyv1alpha1.LogFieldSelector,
	log plog.LogRecord,
	scope plog.ScopeLogs,
	res plog.ResourceLogs,
) (pcommon.Value, bool) {
	if target == nil || target.Target == nil {
		return pcommon.Value{}, false
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.LogFieldSelector_RecordField:
		switch t.RecordField {
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY:
			exists := log.Body().Type() != pcommon.ValueTypeEmpty
			return log.Body(), exists
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT:
			val := log.SeverityText()
			if val == "" {
				return pcommon.Value{}, false
			}
			return pcommon.NewValueStr(val), true
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER:
			num := log.SeverityNumber()
			if num == plog.SeverityNumberUnspecified {
				return pcommon.Value{}, false
			}
			return pcommon.NewValueInt(int64(num)), true
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID:
			tid := log.TraceID()
			if tid.IsEmpty() {
				return pcommon.Value{}, false
			}
			return pcommon.NewValueStr(tid.String()), true
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
			sid := log.SpanID()
			if sid.IsEmpty() {
				return pcommon.Value{}, false
			}
			return pcommon.NewValueStr(sid.String()), true
		default:
			return pcommon.Value{}, false
		}

	case *policyv1alpha1.LogFieldSelector_LogAttribute:
		return extractAttribute(log.Attributes(), t.LogAttribute)

	case *policyv1alpha1.LogFieldSelector_ResourceAttribute:
		return extractAttribute(res.Resource().Attributes(), t.ResourceAttribute)

	case *policyv1alpha1.LogFieldSelector_ScopeAttribute:
		return extractAttribute(scope.Scope().Attributes(), t.ScopeAttribute)

	case *policyv1alpha1.LogFieldSelector_ScopeField:
		switch t.ScopeField {
		case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
			name := scope.Scope().Name()
			if name == "" {
				return pcommon.Value{}, false
			}
			return pcommon.NewValueStr(name), true
		case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
			ver := scope.Scope().Version()
			if ver == "" {
				return pcommon.Value{}, false
			}
			return pcommon.NewValueStr(ver), true
		case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
			url := scope.SchemaUrl()
			if url == "" {
				return pcommon.Value{}, false
			}
			return pcommon.NewValueStr(url), true
		default:
			return pcommon.Value{}, false
		}

	default:
		return pcommon.Value{}, false
	}
}

func extractAttribute(attrs pcommon.Map, attrPath *policyv1alpha1.AttributePath) (pcommon.Value, bool) {
	if attrPath == nil || len(attrPath.GetPath()) == 0 {
		return pcommon.Value{}, false
	}
	segments := attrPath.GetPath()
	val, ok := attrs.Get(segments[0])
	if !ok {
		return pcommon.Value{}, false
	}
	for _, seg := range segments[1:] {
		switch val.Type() {
		case pcommon.ValueTypeMap:
			child, found := val.Map().Get(seg)
			if !found {
				return pcommon.Value{}, false
			}
			val = child
		case pcommon.ValueTypeSlice:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= val.Slice().Len() {
				return pcommon.Value{}, false
			}
			val = val.Slice().At(idx)
		default:
			return pcommon.Value{}, false
		}
	}
	return val, true
}

func matchValueEquals(val pcommon.Value, expected *policyv1alpha1.Value) bool {
	switch exp := expected.GetValue().(type) {
	case *policyv1alpha1.Value_StringValue:
		if val.Type() == pcommon.ValueTypeStr {
			return val.Str() == exp.StringValue
		}
		return false
	case *policyv1alpha1.Value_IntValue:
		if val.Type() == pcommon.ValueTypeInt {
			return val.Int() == exp.IntValue
		}
		if val.Type() == pcommon.ValueTypeDouble {
			return val.Double() == float64(exp.IntValue)
		}
		return false
	case *policyv1alpha1.Value_BoolValue:
		if val.Type() == pcommon.ValueTypeBool {
			return val.Bool() == exp.BoolValue
		}
		return false
	case *policyv1alpha1.Value_DoubleValue:
		if val.Type() == pcommon.ValueTypeDouble {
			return val.Double() == exp.DoubleValue
		}
		if val.Type() == pcommon.ValueTypeInt {
			return float64(val.Int()) == exp.DoubleValue
		}
		return false
	case *policyv1alpha1.Value_BytesValue:
		if val.Type() == pcommon.ValueTypeBytes {
			return bytes.Equal(val.Bytes().AsRaw(), exp.BytesValue)
		}
		return false
	default:
		return false
	}
}

func matchRegex(val pcommon.Value, re *regexp.Regexp) bool {
	switch val.Type() {
	case pcommon.ValueTypeStr:
		return re.MatchString(val.Str())
	case pcommon.ValueTypeInt:
		return re.MatchString(strconv.FormatInt(val.Int(), 10))
	default:
		return false
	}
}

func compareNumeric(val pcommon.Value, expected *policyv1alpha1.NumericValue) int {
	var targetIsInt bool
	var targetInt int64
	var targetDouble float64

	switch val.Type() {
	case pcommon.ValueTypeInt:
		targetIsInt = true
		targetInt = val.Int()
		targetDouble = float64(targetInt)
	case pcommon.ValueTypeDouble:
		targetDouble = val.Double()
	default:
		return -2
	}

	switch exp := expected.GetValue().(type) {
	case *policyv1alpha1.NumericValue_IntValue:
		if targetIsInt {
			if targetInt < exp.IntValue {
				return -1
			}
			if targetInt > exp.IntValue {
				return 1
			}
			return 0
		}
		expDouble := float64(exp.IntValue)
		if targetDouble < expDouble {
			return -1
		}
		if targetDouble > expDouble {
			return 1
		}
		return 0
	case *policyv1alpha1.NumericValue_DoubleValue:
		if targetDouble < exp.DoubleValue {
			return -1
		}
		if targetDouble > exp.DoubleValue {
			return 1
		}
		return 0
	default:
		return -2
	}
}

func matchValueContains(val pcommon.Value, needle *policyv1alpha1.Value) bool {
	switch val.Type() {
	case pcommon.ValueTypeSlice:
		slice := val.Slice()
		for i := 0; i < slice.Len(); i++ {
			elem := slice.At(i)
			if matchValueEquals(elem, needle) {
				return true
			}
		}
		return false
	case pcommon.ValueTypeStr:
		if s, ok := needle.GetValue().(*policyv1alpha1.Value_StringValue); ok {
			return strings.Contains(val.Str(), s.StringValue)
		}
		return false
	case pcommon.ValueTypeBytes:
		if b, ok := needle.GetValue().(*policyv1alpha1.Value_BytesValue); ok {
			return bytes.Contains(val.Bytes().AsRaw(), b.BytesValue)
		}
		return false
	default:
		return false
	}
}
