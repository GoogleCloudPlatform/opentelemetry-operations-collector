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
	"errors"
	"fmt"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/matcher"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for metric filter policies.
const PolicyType = "metric_filter"

// driver loads a MetricFilterPolicy from either shape it can arrive in: the
// proto a control plane sends, or the map an authored config decodes to.
//
// Exposed to the package (rather than constructed inline in init) so tests
// exercise the very value that is registered.
var driver = googlepolicy.ProtoDriver[*policyv1alpha1.MetricFilterPolicy]{
	New: func(pb *policyv1alpha1.MetricFilterPolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(err)
	}
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
	ErrMissingTarget = matcher.ErrMissingMetricTarget
	// ErrMissingPredicate is returned when a matcher does not specify a predicate.
	ErrMissingPredicate = matcher.ErrMissingPredicate
)

// Policy represents a compiled, validated MetricFilterPolicy ready for hot-loop evaluation.
type Policy struct {
	proto            *policyv1alpha1.MetricFilterPolicy
	matchers         []matcher.CompiledMetricMatcher
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
		matchers: make([]matcher.CompiledMetricMatcher, 0, len(pb.GetMatches())),
	}
	for i, m := range pb.GetMatches() {
		cm, err := matcher.CompileMetricMatcher(m)
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
		if !p.matchers[i].Eval(ctx) {
			return false
		}
	}
	return true
}

// MetricTypeString renders an instrument type using the spelling that policies
// match against.
func MetricTypeString(t pmetric.MetricType) string {
	return matcher.MetricTypeString(t)
}

// TemporalityString renders an aggregation temporality using the spelling that
// policies match against.
func TemporalityString(t pmetric.AggregationTemporality) string {
	return matcher.TemporalityString(t)
}
