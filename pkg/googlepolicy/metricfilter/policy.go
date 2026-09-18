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

// Package metricfilter compiles and evaluates MetricFilterPolicy
// configurations. It is the single authoritative implementation for the metric
// signal: the collector processor walks the pdata tree and delegates every
// matching decision here.
package metricfilter

import (
	"encoding/json"
	"errors"
	"fmt"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/matcher"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for metric filter policies.
const PolicyType = "metric_filter"

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, &Driver{}); err != nil {
		panic(err)
	}
}

// Driver implements googlepolicy.PolicyDriver for loading MetricFilterPolicy configurations.
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
	return &policyv1alpha1.MetricFilterPolicy{}
}

// LoadPolicy unmarshals a raw policy configuration map into a compiled *Policy.
func (d *Driver) LoadPolicy(raw map[string]any) (googlepolicy.Policy, error) {
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal raw policy map to json: %w", err)
	}

	pb := &policyv1alpha1.MetricFilterPolicy{}
	unmarshaler := protojson.UnmarshalOptions{
		DiscardUnknown: true,
	}
	if err := unmarshaler.Unmarshal(jsonBytes, pb); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into MetricFilterPolicy proto: %w", err)
	}

	return NewPolicyFromProto(pb)
}

var (
	// ErrNilProto is returned when a nil MetricFilterPolicy protobuf is provided.
	ErrNilProto = errors.New("metric filter policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("metric filter policy id cannot be empty")
	// ErrMissingAction is returned when the policy action is unspecified or invalid.
	ErrMissingAction = errors.New("metric filter policy must specify a valid action (ACTION_KEEP or ACTION_DROP)")
	// ErrMissingMatchers is returned when the policy contains no matchers.
	ErrMissingMatchers = errors.New("metric filter policy must contain at least one matcher")
	// ErrMissingTarget is returned when a matcher does not specify a target field selector.
	ErrMissingTarget = errors.New("metric matcher must specify a target field selector")
	// ErrMissingPredicate is returned when a matcher does not specify a predicate.
	ErrMissingPredicate = matcher.ErrMissingPredicate
)

// Policy represents a compiled, validated MetricFilterPolicy ready for hot-loop evaluation.
type Policy struct {
	proto            *policyv1alpha1.MetricFilterPolicy
	matchers         []compiledMatcher
	isDatapointLevel bool
}

var _ googlepolicy.TransformationPolicy = (*Policy)(nil)
var _ googlepolicy.MetricPolicyEvaluator = (*Policy)(nil)

// NewPolicyFromProto validates and compiles a MetricFilterPolicy protobuf into a Policy.
func NewPolicyFromProto(pb *policyv1alpha1.MetricFilterPolicy) (*Policy, error) {
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

	p := &Policy{
		proto:    pb,
		matchers: make([]compiledMatcher, 0, len(pb.GetMatches())),
	}
	for i, m := range pb.GetMatches() {
		cm, err := compileMatcher(m)
		if err != nil {
			return nil, fmt.Errorf("matcher[%d]: %w", i, err)
		}
		if _, ok := m.GetTarget().GetTarget().(*policyv1alpha1.MetricFieldSelector_DatapointAttribute); ok {
			p.isDatapointLevel = true
		}
		p.matchers = append(p.matchers, cm)
	}

	return p, nil
}

// PolicyName returns the unique identifier of this policy instance.
func (p *Policy) PolicyName() string {
	return p.proto.GetId()
}

// PolicyType returns the type identifier of this policy ("metric_filter").
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns PolicyClassTransformation for metric filter policies.
func (p *Policy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}

// TargetSignals returns the telemetry signals targeted by this policy (SignalMetrics).
func (p *Policy) TargetSignals() []googlepolicy.Signal {
	return []googlepolicy.Signal{googlepolicy.SignalMetrics}
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

// IsDatapointLevel reports whether any matcher targets datapoint attributes.
// Instrument-level-only policies can be evaluated once per metric rather than
// once per datapoint.
func (p *Policy) IsDatapointLevel() bool {
	return p.isDatapointLevel
}

// EvaluateMetric evaluates the policy against the given MetricContext and
// returns EvalKeep, EvalDrop, or EvalNoMatch.
func (p *Policy) EvaluateMetric(ctx googlepolicy.MetricContext) googlepolicy.EvalResult {
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
// MetricContext. Matchers are AND-ed.
func (p *Policy) matchesContext(ctx googlepolicy.MetricContext) bool {
	for i := range p.matchers {
		if !p.matchers[i].eval(ctx) {
			return false
		}
	}
	return true
}

// targetExtractor pulls the value a matcher targets out of a MetricContext.
type targetExtractor func(ctx googlepolicy.MetricContext) (val any, exists bool)

type compiledMatcher struct {
	extract   targetExtractor
	predicate matcher.Predicate
}

func (cm *compiledMatcher) eval(ctx googlepolicy.MetricContext) bool {
	return cm.predicate(cm.extract(ctx))
}

func compileMatcher(m *policyv1alpha1.MetricMatcher) (compiledMatcher, error) {
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

func compileExtractor(target *policyv1alpha1.MetricFieldSelector) (targetExtractor, error) {
	if target == nil || target.Target == nil {
		return nil, ErrMissingTarget
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.MetricFieldSelector_DescriptorField:
		return compileDescriptorFieldExtractor(t.DescriptorField)

	case *policyv1alpha1.MetricFieldSelector_DatapointAttribute:
		steps, err := matcher.CompilePath(t.DatapointAttribute, "datapoint")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			return matcher.EvalPath(ctx.DatapointAttributes, steps)
		}, nil

	case *policyv1alpha1.MetricFieldSelector_ResourceAttribute:
		steps, err := matcher.CompilePath(t.ResourceAttribute, "resource")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Resource == (pcommon.Resource{}) {
				return nil, false
			}
			return matcher.EvalPath(ctx.Resource.Attributes(), steps)
		}, nil

	case *policyv1alpha1.MetricFieldSelector_ScopeAttribute:
		steps, err := matcher.CompilePath(t.ScopeAttribute, "scope")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return nil, false
			}
			return matcher.EvalPath(ctx.Scope.Attributes(), steps)
		}, nil

	case *policyv1alpha1.MetricFieldSelector_ScopeField:
		return compileScopeFieldExtractor(t.ScopeField)

	default:
		return nil, ErrMissingTarget
	}
}

// compileDescriptorFieldExtractor builds an extractor for a first-class metric
// descriptor field.
//
// Per metric_filter_policy.proto: "When evaluated with `exists`, first-class
// fields evaluate to true when set to a non-default (non-empty / non-zero)
// value." String fields (NAME, DESCRIPTION, UNIT) return (nil, false) when
// empty; enum fields (TYPE) return their rendered string ("UNSPECIFIED") with
// exists=false so string predicates can still match the default (see matcher.present).
func compileDescriptorFieldExtractor(field policyv1alpha1.MetricDescriptorField) (targetExtractor, error) {
	switch field {
	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) {
				return nil, false
			}
			s := ctx.Metric.Name()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) {
				return nil, false
			}
			s := ctx.Metric.Description()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) {
				return nil, false
			}
			s := ctx.Metric.Unit()
			if s == "" {
				return nil, false
			}
			return s, true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE:
		// Every instrument has a type, so an untyped one is present-but-default
		// rather than absent: `exists` reports false, but string predicates can
		// still select it by its "UNSPECIFIED" spelling.
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) {
				return nil, false
			}
			t := ctx.Metric.Type()
			return MetricTypeString(t), t != pmetric.MetricTypeEmpty
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY:
		// Unlike TYPE, temporality is only defined for sums and histograms, and
		// the proto enumerates "DELTA" and "CUMULATIVE" as the only matchable
		// spellings. An instrument without a temporality is therefore reported
		// as truly absent, not as "UNSPECIFIED".
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.AggregationTemporality == pmetric.AggregationTemporalityUnspecified {
				return nil, false
			}
			return TemporalityString(ctx.AggregationTemporality), true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC:
		// Monotonicity is only defined for sums; every other instrument type
		// reports the field as absent rather than as false.
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.Metric == (pmetric.Metric{}) || ctx.Metric.Type() != pmetric.MetricTypeSum {
				return nil, false
			}
			return ctx.Metric.Sum().IsMonotonic(), true
		}, nil

	default:
		return nil, errors.New("metric descriptor field cannot be unspecified")
	}
}

func compileScopeFieldExtractor(field policyv1alpha1.ScopeField) (targetExtractor, error) {
	switch field {
	case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
		return func(ctx googlepolicy.MetricContext) (any, bool) {
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
		return func(ctx googlepolicy.MetricContext) (any, bool) {
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
		return func(ctx googlepolicy.MetricContext) (any, bool) {
			if ctx.ScopeSchemaURL == "" {
				return nil, false
			}
			return ctx.ScopeSchemaURL, true
		}, nil

	default:
		return nil, errors.New("scope field cannot be unspecified")
	}
}

// MetricTypeString renders an instrument type using the spelling that policies
// match against.
func MetricTypeString(t pmetric.MetricType) string {
	switch t {
	case pmetric.MetricTypeGauge:
		return "GAUGE"
	case pmetric.MetricTypeSum:
		return "SUM"
	case pmetric.MetricTypeHistogram:
		return "HISTOGRAM"
	case pmetric.MetricTypeExponentialHistogram:
		return "EXPONENTIAL_HISTOGRAM"
	case pmetric.MetricTypeSummary:
		return "SUMMARY"
	default:
		return "UNSPECIFIED"
	}
}

// TemporalityString renders an aggregation temporality using the spelling that
// policies match against.
func TemporalityString(t pmetric.AggregationTemporality) string {
	switch t {
	case pmetric.AggregationTemporalityDelta:
		return "DELTA"
	case pmetric.AggregationTemporalityCumulative:
		return "CUMULATIVE"
	default:
		return "UNSPECIFIED"
	}
}
