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
	"bytes"
	"fmt"
	"regexp"
	"strings"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/proto"
)

// ProtoPolicy is an optional interface that a TransformationPolicy can implement
// to provide its underlying protobuf message.
type ProtoPolicy interface {
	Proto() proto.Message
}

// Evaluator evaluates telemetry data against compiled Google transformation policies.
type Evaluator struct {
	logPolicies    []*compiledLogPolicy
	metricPolicies []*compiledMetricPolicy
	tracePolicies  []*compiledTracePolicy
}

type compiledLogPolicy struct {
	id       string
	action   policyv1alpha1.Action
	matchers []*compiledLogMatcher
}

type compiledMetricPolicy struct {
	id       string
	action   policyv1alpha1.Action
	matchers []*compiledMetricMatcher
}

type compiledTracePolicy struct {
	id       string
	action   policyv1alpha1.Action
	matchers []*compiledTraceMatcher
}

type predicateType int

const (
	predExists predicateType = iota
	predEquals
	predRegex
	predGt
	predGte
	predLt
	predLte
	predContains
)

type matcherPredicate struct {
	typ         predicateType
	equalsVal   *policyv1alpha1.Value
	regex       *regexp.Regexp
	gtVal       *policyv1alpha1.NumericValue
	gteVal      *policyv1alpha1.NumericValue
	ltVal       *policyv1alpha1.NumericValue
	lteVal      *policyv1alpha1.NumericValue
	containsVal *policyv1alpha1.Value
	negate      bool
}

type compiledLogMatcher struct {
	target *policyv1alpha1.LogFieldSelector
	pred   matcherPredicate
}

type compiledMetricMatcher struct {
	target *policyv1alpha1.MetricFieldSelector
	pred   matcherPredicate
}

type compiledTraceMatcher struct {
	target *policyv1alpha1.TraceFieldSelector
	pred   matcherPredicate
}

// NewEvaluator compiles a slice of TransformationPolicy objects into an Evaluator.
func NewEvaluator(policies []googlepolicy.TransformationPolicy) (*Evaluator, error) {
	ev := &Evaluator{}
	for _, tp := range policies {
		if tp == nil {
			continue
		}
		raw := any(tp)
		if pp, ok := tp.(ProtoPolicy); ok {
			raw = pp.Proto()
		}

		switch p := raw.(type) {
		case *policyv1alpha1.LogFilterPolicy:
			cp, err := compileLogPolicy(p)
			if err != nil {
				return nil, fmt.Errorf("failed to compile log policy %s: %w", p.GetId(), err)
			}
			ev.logPolicies = append(ev.logPolicies, cp)
		case *policyv1alpha1.MetricFilterPolicy:
			cp, err := compileMetricPolicy(p)
			if err != nil {
				return nil, fmt.Errorf("failed to compile metric policy %s: %w", p.GetId(), err)
			}
			ev.metricPolicies = append(ev.metricPolicies, cp)
		case *policyv1alpha1.TraceFilterPolicy:
			cp, err := compileTracePolicy(p)
			if err != nil {
				return nil, fmt.Errorf("failed to compile trace policy %s: %w", p.GetId(), err)
			}
			ev.tracePolicies = append(ev.tracePolicies, cp)
		}
	}
	return ev, nil
}

func compileLogPolicy(p *policyv1alpha1.LogFilterPolicy) (*compiledLogPolicy, error) {
	cp := &compiledLogPolicy{
		id:     p.GetId(),
		action: p.GetAction(),
	}
	for _, m := range p.GetMatches() {
		pred, err := compilePredicate(m.GetPredicate(), m.GetNegate())
		if err != nil {
			return nil, err
		}
		cp.matchers = append(cp.matchers, &compiledLogMatcher{
			target: m.GetTarget(),
			pred:   pred,
		})
	}
	return cp, nil
}

func compileMetricPolicy(p *policyv1alpha1.MetricFilterPolicy) (*compiledMetricPolicy, error) {
	cp := &compiledMetricPolicy{
		id:     p.GetId(),
		action: p.GetAction(),
	}
	for _, m := range p.GetMatches() {
		pred, err := compilePredicate(m.GetPredicate(), m.GetNegate())
		if err != nil {
			return nil, err
		}
		cp.matchers = append(cp.matchers, &compiledMetricMatcher{
			target: m.GetTarget(),
			pred:   pred,
		})
	}
	return cp, nil
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

func compilePredicate(predicate any, negate bool) (matcherPredicate, error) {
	pred := matcherPredicate{negate: negate}
	switch p := predicate.(type) {
	case *policyv1alpha1.LogMatcher_Exists, *policyv1alpha1.MetricMatcher_Exists, *policyv1alpha1.TraceMatcher_Exists:
		pred.typ = predExists
	case *policyv1alpha1.LogMatcher_Equals:
		pred.typ = predEquals
		pred.equalsVal = p.Equals
	case *policyv1alpha1.MetricMatcher_Equals:
		pred.typ = predEquals
		pred.equalsVal = p.Equals
	case *policyv1alpha1.TraceMatcher_Equals:
		pred.typ = predEquals
		pred.equalsVal = p.Equals
	case *policyv1alpha1.LogMatcher_Regex:
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return pred, fmt.Errorf("invalid regex '%s': %w", p.Regex, err)
		}
		pred.typ = predRegex
		pred.regex = re
	case *policyv1alpha1.MetricMatcher_Regex:
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return pred, fmt.Errorf("invalid regex '%s': %w", p.Regex, err)
		}
		pred.typ = predRegex
		pred.regex = re
	case *policyv1alpha1.TraceMatcher_Regex:
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return pred, fmt.Errorf("invalid regex '%s': %w", p.Regex, err)
		}
		pred.typ = predRegex
		pred.regex = re
	case *policyv1alpha1.LogMatcher_Gt:
		pred.typ = predGt
		pred.gtVal = p.Gt
	case *policyv1alpha1.MetricMatcher_Gt:
		pred.typ = predGt
		pred.gtVal = p.Gt
	case *policyv1alpha1.TraceMatcher_Gt:
		pred.typ = predGt
		pred.gtVal = p.Gt
	case *policyv1alpha1.LogMatcher_Gte:
		pred.typ = predGte
		pred.gteVal = p.Gte
	case *policyv1alpha1.MetricMatcher_Gte:
		pred.typ = predGte
		pred.gteVal = p.Gte
	case *policyv1alpha1.TraceMatcher_Gte:
		pred.typ = predGte
		pred.gteVal = p.Gte
	case *policyv1alpha1.LogMatcher_Lt:
		pred.typ = predLt
		pred.ltVal = p.Lt
	case *policyv1alpha1.MetricMatcher_Lt:
		pred.typ = predLt
		pred.ltVal = p.Lt
	case *policyv1alpha1.TraceMatcher_Lt:
		pred.typ = predLt
		pred.ltVal = p.Lt
	case *policyv1alpha1.LogMatcher_Lte:
		pred.typ = predLte
		pred.lteVal = p.Lte
	case *policyv1alpha1.MetricMatcher_Lte:
		pred.typ = predLte
		pred.lteVal = p.Lte
	case *policyv1alpha1.TraceMatcher_Lte:
		pred.typ = predLte
		pred.lteVal = p.Lte
	case *policyv1alpha1.LogMatcher_Contains:
		pred.typ = predContains
		pred.containsVal = p.Contains
	case *policyv1alpha1.MetricMatcher_Contains:
		pred.typ = predContains
		pred.containsVal = p.Contains
	case *policyv1alpha1.TraceMatcher_Contains:
		pred.typ = predContains
		pred.containsVal = p.Contains
	default:
		return pred, fmt.Errorf("unsupported or nil predicate: %T", predicate)
	}
	return pred, nil
}

func (p *matcherPredicate) evaluate(val any, exists bool) bool {
	matched := false
	switch p.typ {
	case predExists:
		matched = exists
	case predEquals:
		if exists && p.equalsVal != nil {
			matched = matchEquals(val, p.equalsVal)
		}
	case predRegex:
		if exists && p.regex != nil {
			if s, ok := val.(string); ok {
				matched = p.regex.MatchString(s)
			}
		}
	case predGt:
		if exists && p.gtVal != nil {
			if num, ok := toFloat64(val); ok {
				targetNum := numericToFloat64(p.gtVal)
				matched = num > targetNum
			}
		}
	case predGte:
		if exists && p.gteVal != nil {
			if num, ok := toFloat64(val); ok {
				targetNum := numericToFloat64(p.gteVal)
				matched = num >= targetNum
			}
		}
	case predLt:
		if exists && p.ltVal != nil {
			if num, ok := toFloat64(val); ok {
				targetNum := numericToFloat64(p.ltVal)
				matched = num < targetNum
			}
		}
	case predLte:
		if exists && p.lteVal != nil {
			if num, ok := toFloat64(val); ok {
				targetNum := numericToFloat64(p.lteVal)
				matched = num <= targetNum
			}
		}
	case predContains:
		if exists && p.containsVal != nil {
			matched = matchContains(val, p.containsVal)
		}
	}

	if p.negate {
		return !matched
	}
	return matched
}

func matchEquals(val any, expected *policyv1alpha1.Value) bool {
	switch exp := expected.Value.(type) {
	case *policyv1alpha1.Value_StringValue:
		s, ok := val.(string)
		return ok && s == exp.StringValue
	case *policyv1alpha1.Value_IntValue:
		if num, ok := toInt64(val); ok {
			return num == exp.IntValue
		}
	case *policyv1alpha1.Value_DoubleValue:
		if num, ok := toFloat64(val); ok {
			return num == exp.DoubleValue
		}
	case *policyv1alpha1.Value_BoolValue:
		b, ok := val.(bool)
		return ok && b == exp.BoolValue
	case *policyv1alpha1.Value_BytesValue:
		b, ok := val.([]byte)
		return ok && bytes.Equal(b, exp.BytesValue)
	}
	return false
}

func matchContains(val any, expected *policyv1alpha1.Value) bool {
	switch v := val.(type) {
	case string:
		if exp, ok := expected.Value.(*policyv1alpha1.Value_StringValue); ok {
			return strings.Contains(v, exp.StringValue)
		}
	case []byte:
		if exp, ok := expected.Value.(*policyv1alpha1.Value_BytesValue); ok {
			return bytes.Contains(v, exp.BytesValue)
		}
	case []any:
		for _, elem := range v {
			if matchEquals(elem, expected) {
				return true
			}
		}
	}
	return false
}

func toFloat64(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint32:
		return float64(v), true
	default:
		return 0, false
	}
}

func toInt64(val any) (int64, bool) {
	switch v := val.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case int32:
		return int64(v), true
	case uint64:
		return int64(v), true
	case uint32:
		return int64(v), true
	default:
		return 0, false
	}
}

func numericToFloat64(nv *policyv1alpha1.NumericValue) float64 {
	switch v := nv.Value.(type) {
	case *policyv1alpha1.NumericValue_DoubleValue:
		return v.DoubleValue
	case *policyv1alpha1.NumericValue_IntValue:
		return float64(v.IntValue)
	default:
		return 0
	}
}

// ----------------------------------------------------------------------------
// LOG EVALUATION
// ----------------------------------------------------------------------------

// EvalLog returns true if the log record should be DROPPED, false if it should be KEPT.
func (e *Evaluator) EvalLog(lr plog.LogRecord, res pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string) bool {
	if len(e.logPolicies) == 0 {
		return false
	}

	var hasKeep, hasDrop bool
	for _, p := range e.logPolicies {
		if p.matches(lr, res, scope, resSchema, scopeSchema) {
			if p.action == policyv1alpha1.Action_ACTION_KEEP {
				hasKeep = true
			} else if p.action == policyv1alpha1.Action_ACTION_DROP {
				hasDrop = true
			}
		}
	}

	if hasKeep {
		return false // KEEP always overrides DROP
	}
	if hasDrop {
		return true // Drop matching record
	}
	return false // Default allow
}

func (p *compiledLogPolicy) matches(lr plog.LogRecord, res pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string) bool {
	for _, m := range p.matchers {
		val, exists := extractLogTarget(lr, res, scope, resSchema, scopeSchema, m.target)
		if !m.pred.evaluate(val, exists) {
			return false
		}
	}
	return true
}

func extractLogTarget(lr plog.LogRecord, res pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string, target *policyv1alpha1.LogFieldSelector) (any, bool) {
	if target == nil {
		return nil, false
	}
	switch t := target.Target.(type) {
	case *policyv1alpha1.LogFieldSelector_RecordField:
		switch t.RecordField {
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY:
			s := lr.Body().AsString()
			return s, s != ""
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT:
			s := lr.SeverityText()
			return s, s != ""
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER:
			n := int64(lr.SeverityNumber())
			return n, n != 0
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID:
			tid := lr.TraceID()
			if tid.IsEmpty() {
				return nil, false
			}
			return tid.String(), true
		case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
			sid := lr.SpanID()
			if sid.IsEmpty() {
				return nil, false
			}
			return sid.String(), true
		default:
			return nil, false
		}
	case *policyv1alpha1.LogFieldSelector_LogAttribute:
		return lookupPath(lr.Attributes(), t.LogAttribute.GetPath())
	case *policyv1alpha1.LogFieldSelector_ResourceAttribute:
		return lookupPath(res.Attributes(), t.ResourceAttribute.GetPath())
	case *policyv1alpha1.LogFieldSelector_ScopeAttribute:
		return lookupPath(scope.Attributes(), t.ScopeAttribute.GetPath())
	case *policyv1alpha1.LogFieldSelector_ScopeField:
		switch t.ScopeField {
		case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
			s := scope.Name()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
			s := scope.Version()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
			return scopeSchema, scopeSchema != ""
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

// ----------------------------------------------------------------------------
// METRIC EVALUATION
// ----------------------------------------------------------------------------

// EvalMetricDatapoint returns true if the datapoint should be DROPPED, false if KEPT.
func (e *Evaluator) EvalMetricDatapoint(m pmetric.Metric, dpAttrs pcommon.Map, temporality pmetric.AggregationTemporality, res pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string) bool {
	if len(e.metricPolicies) == 0 {
		return false
	}

	var hasKeep, hasDrop bool
	for _, p := range e.metricPolicies {
		if p.matches(m, dpAttrs, temporality, res, scope, resSchema, scopeSchema) {
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

func (p *compiledMetricPolicy) matches(m pmetric.Metric, dpAttrs pcommon.Map, temporality pmetric.AggregationTemporality, res pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string) bool {
	for _, cm := range p.matchers {
		val, exists := extractMetricTarget(m, dpAttrs, temporality, res, scope, resSchema, scopeSchema, cm.target)
		if !cm.pred.evaluate(val, exists) {
			return false
		}
	}
	return true
}

func extractMetricTarget(m pmetric.Metric, dpAttrs pcommon.Map, temporality pmetric.AggregationTemporality, res pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string, target *policyv1alpha1.MetricFieldSelector) (any, bool) {
	if target == nil {
		return nil, false
	}
	switch t := target.Target.(type) {
	case *policyv1alpha1.MetricFieldSelector_DescriptorField:
		switch t.DescriptorField {
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME:
			s := m.Name()
			return s, s != ""
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION:
			s := m.Description()
			return s, s != ""
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT:
			s := m.Unit()
			return s, s != ""
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE:
			return metricTypeToString(m.Type()), true
		case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY:
			return temporalityToString(temporality), true
		default:
			return nil, false
		}
	case *policyv1alpha1.MetricFieldSelector_DatapointAttribute:
		return lookupPath(dpAttrs, t.DatapointAttribute.GetPath())
	case *policyv1alpha1.MetricFieldSelector_ResourceAttribute:
		return lookupPath(res.Attributes(), t.ResourceAttribute.GetPath())
	case *policyv1alpha1.MetricFieldSelector_ScopeAttribute:
		return lookupPath(scope.Attributes(), t.ScopeAttribute.GetPath())
	case *policyv1alpha1.MetricFieldSelector_ScopeField:
		switch t.ScopeField {
		case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
			s := scope.Name()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
			s := scope.Version()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
			return scopeSchema, scopeSchema != ""
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

func metricTypeToString(t pmetric.MetricType) string {
	switch t {
	case pmetric.MetricTypeGauge:
		return "gauge"
	case pmetric.MetricTypeSum:
		return "sum"
	case pmetric.MetricTypeHistogram:
		return "histogram"
	case pmetric.MetricTypeExponentialHistogram:
		return "exponential_histogram"
	case pmetric.MetricTypeSummary:
		return "summary"
	default:
		return "unspecified"
	}
}

func temporalityToString(t pmetric.AggregationTemporality) string {
	switch t {
	case pmetric.AggregationTemporalityDelta:
		return "delta"
	case pmetric.AggregationTemporalityCumulative:
		return "cumulative"
	default:
		return "unspecified"
	}
}

// ----------------------------------------------------------------------------
// TRACE EVALUATION
// ----------------------------------------------------------------------------

// EvalTraceSpan returns true if the span should be DROPPED, false if KEPT.
func (e *Evaluator) EvalTraceSpan(span ptrace.Span, res pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string) bool {
	if len(e.tracePolicies) == 0 {
		return false
	}

	var hasKeep, hasDrop bool
	for _, p := range e.tracePolicies {
		if p.matches(span, res, scope, resSchema, scopeSchema) {
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

func (p *compiledTracePolicy) matches(span ptrace.Span, res pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string) bool {
	for _, cm := range p.matchers {
		val, exists := extractTraceTarget(span, res, scope, resSchema, scopeSchema, cm.target)
		if !cm.pred.evaluate(val, exists) {
			return false
		}
	}
	return true
}

func extractTraceTarget(span ptrace.Span, res pcommon.Resource, scope pcommon.InstrumentationScope, resSchema, scopeSchema string, target *policyv1alpha1.TraceFieldSelector) (any, bool) {
	if target == nil {
		return nil, false
	}
	switch t := target.Target.(type) {
	case *policyv1alpha1.TraceFieldSelector_RecordField:
		switch t.RecordField {
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME:
			s := span.Name()
			return s, s != ""
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID:
			tid := span.TraceID()
			if tid.IsEmpty() {
				return nil, false
			}
			return tid.String(), true
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_SPAN_ID:
			sid := span.SpanID()
			if sid.IsEmpty() {
				return nil, false
			}
			return sid.String(), true
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_PARENT_SPAN_ID:
			psid := span.ParentSpanID()
			if psid.IsEmpty() {
				return nil, false
			}
			return psid.String(), true
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE:
			s := span.Status().Message()
			return s, s != ""
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND:
			return spanKindToString(span.Kind()), true
		case policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE:
			return spanStatusToString(span.Status().Code()), true
		default:
			return nil, false
		}
	case *policyv1alpha1.TraceFieldSelector_SpanAttribute:
		return lookupPath(span.Attributes(), t.SpanAttribute.GetPath())
	case *policyv1alpha1.TraceFieldSelector_ResourceAttribute:
		return lookupPath(res.Attributes(), t.ResourceAttribute.GetPath())
	case *policyv1alpha1.TraceFieldSelector_ScopeAttribute:
		return lookupPath(scope.Attributes(), t.ScopeAttribute.GetPath())
	case *policyv1alpha1.TraceFieldSelector_ScopeField:
		switch t.ScopeField {
		case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
			s := scope.Name()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
			s := scope.Version()
			return s, s != ""
		case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
			return scopeSchema, scopeSchema != ""
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

// ----------------------------------------------------------------------------
// PATH LOOKUP HELPER
// ----------------------------------------------------------------------------

func lookupPath(attrs pcommon.Map, path []string) (any, bool) {
	if len(path) == 0 {
		return nil, false
	}
	var current pcommon.Value
	for i, key := range path {
		if i == 0 {
			v, ok := attrs.Get(key)
			if !ok {
				return nil, false
			}
			current = v
		} else {
			if current.Type() != pcommon.ValueTypeMap {
				return nil, false
			}
			v, ok := current.Map().Get(key)
			if !ok {
				return nil, false
			}
			current = v
		}
	}
	return pcommonValueToAny(current), true
}

func pcommonValueToAny(v pcommon.Value) any {
	switch v.Type() {
	case pcommon.ValueTypeStr:
		return v.Str()
	case pcommon.ValueTypeInt:
		return v.Int()
	case pcommon.ValueTypeDouble:
		return v.Double()
	case pcommon.ValueTypeBool:
		return v.Bool()
	case pcommon.ValueTypeBytes:
		return v.Bytes().AsRaw()
	case pcommon.ValueTypeSlice:
		s := v.Slice()
		res := make([]any, s.Len())
		for i := 0; i < s.Len(); i++ {
			res[i] = pcommonValueToAny(s.At(i))
		}
		return res
	default:
		return v.AsString()
	}
}
