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
	ErrMissingTarget = matcher.ErrMissingLogTarget
	// ErrMissingPredicate is returned when a matcher does not specify a predicate.
	ErrMissingPredicate = matcher.ErrMissingPredicate
)

// Policy represents a compiled, validated LogFilterPolicy ready for hot-loop evaluation.
type Policy struct {
	proto    *policyv1alpha1.LogFilterPolicy
	matchers []matcher.CompiledLogMatcher
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

	matchers := make([]matcher.CompiledLogMatcher, 0, len(pb.GetMatches()))
	for i, m := range pb.GetMatches() {
		cm, err := matcher.CompileLogMatcher(m)
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
		if !p.matchers[i].Eval(ctx) {
			return false
		}
	}
	return true
}
