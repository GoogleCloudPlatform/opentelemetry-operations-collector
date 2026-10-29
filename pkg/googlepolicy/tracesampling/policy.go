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

// Package tracesampling compiles and evaluates TraceSamplingPolicy configurations.
// It implements probabilistic trace sampling backed by the W3C TraceState Consistent
// Probability Sampling specification (OTEP 235) in equalizing mode.
package tracesampling

import (
	"errors"
	"fmt"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/matcher"
	"google.golang.org/protobuf/proto"
)

const (
	// PolicyType is the policy type identifier for TraceSamplingPolicy.
	PolicyType = "trace_sampling"
)

var (
	// ErrNilProto is returned when attempting to construct a policy from a nil protobuf message.
	ErrNilProto = errors.New("nil TraceSamplingPolicy proto")
	// ErrMissingID is returned when a policy proto does not specify an ID.
	ErrMissingID = errors.New("trace sampling policy must specify a non-empty id")
)

var driver = googlepolicy.ProtoDriver[*policyv1alpha1.TraceSamplingPolicy]{
	Class: googlepolicy.PolicyClassTransformation,
	New: func(pb *policyv1alpha1.TraceSamplingPolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(fmt.Sprintf("failed to register %s policy driver: %v", PolicyType, err))
	}
}

// Policy is the compiled in-memory representation of a TraceSamplingPolicy.
// It implements googlepolicy.TraceSamplingPolicyEvaluator.
type Policy struct {
	proto           *policyv1alpha1.TraceSamplingPolicy
	threshold       uint64
	samplingPercent float64
	matchers        []matcher.CompiledTraceMatcher
}

// NewPolicyFromProto compiles and validates a TraceSamplingPolicy from its protobuf representation.
func NewPolicyFromProto(pb *policyv1alpha1.TraceSamplingPolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}

	if pb.GetId() == "" {
		return nil, ErrMissingID
	}

	percent := pb.GetSamplingPercent()
	threshold, err := ProbabilityToThreshold(percent)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid sampling_percent %v", err, percent)
	}

	compiledMatchers := make([]matcher.CompiledTraceMatcher, 0, len(pb.GetMatchers()))
	for i, m := range pb.GetMatchers() {
		cm, err := matcher.CompileTraceMatcher(m)
		if err != nil {
			return nil, fmt.Errorf("failed to compile matcher at index %d: %w", i, err)
		}
		compiledMatchers = append(compiledMatchers, cm)
	}

	return &Policy{
		proto:           pb,
		threshold:       threshold,
		samplingPercent: percent,
		matchers:        compiledMatchers,
	}, nil
}

// PolicyName returns the policy's unique ID.
func (p *Policy) PolicyName() string {
	return p.proto.GetId()
}

// PolicyType returns "trace_sampling".
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns googlepolicy.PolicyClassTransformation.
func (p *Policy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassTransformation
}

// TargetSignals returns [googlepolicy.SignalTraces].
func (p *Policy) TargetSignals() []googlepolicy.Signal {
	return []googlepolicy.Signal{googlepolicy.SignalTraces}
}

// Proto returns the underlying protobuf message for this policy.
func (p *Policy) Proto() proto.Message {
	return p.proto
}

// Validate verifies the policy's configuration integrity.
func (p *Policy) Validate() error {
	if p.proto == nil {
		return ErrNilProto
	}
	if p.proto.GetId() == "" {
		return ErrMissingID
	}
	if _, err := ProbabilityToThreshold(p.samplingPercent); err != nil {
		return err
	}
	return nil
}

// Threshold returns the precomputed 56-bit W3C threshold T.
func (p *Policy) Threshold() uint64 {
	return p.threshold
}

// SamplingPercent returns the configured sampling percentage in (0.0, 100.0].
func (p *Policy) SamplingPercent() float64 {
	return p.samplingPercent
}

// MatchesTrace evaluates whether this policy applies to the given trace span context.
// An empty matchers list matches all spans unconditionally.
// A non-empty matchers list requires all matchers to evaluate to true (AND logic).
func (p *Policy) MatchesTrace(ctx googlepolicy.TraceContext) bool {
	for i := range p.matchers {
		if !p.matchers[i].Eval(ctx) {
			return false
		}
	}
	return true
}
