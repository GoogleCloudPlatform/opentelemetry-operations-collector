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

// NewEvaluator compiles a slice of TransformationPolicy objects into an Evaluator.
func NewEvaluator(policies []googlepolicy.TransformationPolicy) (*Evaluator, error) {
	ev := &Evaluator{}
	for _, tp := range policies {
		if tp == nil {
			continue
		}
		raw := any(tp)
		if pp, ok := tp.(interface {
			Proto() *policyv1alpha1.LogFilterPolicy
		}); ok {
			raw = pp.Proto()
		} else if pp, ok := tp.(interface {
			Proto() *policyv1alpha1.MetricFilterPolicy
		}); ok {
			raw = pp.Proto()
		} else if pp, ok := tp.(interface {
			Proto() *policyv1alpha1.TraceFilterPolicy
		}); ok {
			raw = pp.Proto()
		} else if pp, ok := tp.(ProtoPolicy); ok {
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
