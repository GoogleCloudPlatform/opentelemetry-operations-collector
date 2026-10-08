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

// Package metrictransform compiles and evaluates MetricTransformPolicy
// configurations. It is the single authoritative implementation for metric
// mutations: the collector processor walks surviving metrics and datapoints and
// delegates every transformation here in deterministic stage order
// (rename -> add).
package metrictransform

import (
	"errors"
	"fmt"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/matcher"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for metric transform policies.
const PolicyType = "metric_transform"

// driver loads a MetricTransformPolicy from either shape it can arrive in: the
// proto a control plane sends, or the map an authored config decodes to.
var driver = googlepolicy.ProtoDriver[*policyv1alpha1.MetricTransformPolicy]{
	New: func(pb *policyv1alpha1.MetricTransformPolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(err)
	}
}

var (
	// ErrNilProto is returned when a nil MetricTransformPolicy protobuf is provided.
	ErrNilProto = errors.New("metric transform policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("metric transform policy id cannot be empty")
	// ErrMissingAction is returned when the policy does not specify a transform action.
	ErrMissingAction = errors.New("metric transform policy must specify an action")
	// ErrUnsupportedAction is returned when the policy specifies a transform action that is not yet implemented.
	ErrUnsupportedAction = errors.New("metric transform policy action is not supported yet")
	// ErrMissingTarget is returned when an action does not specify a target field selector.
	ErrMissingTarget = errors.New("metric transform action must specify a target field selector")
	// ErrMissingValue is returned when an action does not specify a valid value.
	ErrMissingValue = matcher.ErrMissingValue
)

type compiledAction func(ctx googlepolicy.MetricContext) bool

// Policy represents a compiled, validated MetricTransformPolicy ready for hot-loop evaluation.
type Policy struct {
	proto            *policyv1alpha1.MetricTransformPolicy
	stage            googlepolicy.TransformStage
	matchers         []matcher.CompiledMetricMatcher
	action           compiledAction
	isDatapointLevel bool
}

var _ googlepolicy.TransformationPolicy = (*Policy)(nil)
var _ googlepolicy.MetricTransformPolicyEvaluator = (*Policy)(nil)

// NewPolicyFromProto validates and compiles a MetricTransformPolicy protobuf into a Policy.
func NewPolicyFromProto(pb *policyv1alpha1.MetricTransformPolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}
	if pb.GetId() == "" {
		return nil, ErrMissingID
	}
	if pb.Action == nil {
		return nil, ErrMissingAction
	}

	var (
		stage            googlepolicy.TransformStage
		action           compiledAction
		isDatapointLevel bool
		err              error
	)
	switch a := pb.Action.(type) {
	case *policyv1alpha1.MetricTransformPolicy_Add:
		if a.Add == nil {
			return nil, ErrMissingAction
		}
		stage = googlepolicy.TransformStageAdd
		action, isDatapointLevel, err = compileAdd(a.Add)
		if err != nil {
			return nil, fmt.Errorf("add: %w", err)
		}
	case *policyv1alpha1.MetricTransformPolicy_Rename:
		return nil, fmt.Errorf("rename: %w", ErrUnsupportedAction)
	default:
		return nil, ErrMissingAction
	}

	matchers := make([]matcher.CompiledMetricMatcher, 0, len(pb.GetMatches()))
	for i, m := range pb.GetMatches() {
		cm, err := matcher.CompileMetricMatcher(m)
		if err != nil {
			return nil, fmt.Errorf("matcher[%d]: %w", i, err)
		}
		if _, ok := m.GetTarget().GetTarget().(*policyv1alpha1.MetricFieldSelector_DatapointAttribute); ok {
			isDatapointLevel = true
		}
		matchers = append(matchers, cm)
	}

	return &Policy{
		proto:            pb,
		stage:            stage,
		matchers:         matchers,
		action:           action,
		isDatapointLevel: isDatapointLevel,
	}, nil
}

// PolicyName returns the unique identifier of this policy instance.
func (p *Policy) PolicyName() string {
	return p.proto.GetId()
}

// PolicyType returns the type identifier of this policy ("metric_transform").
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns PolicyClassTransformation for metric transform policies.
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

// Proto returns the underlying protobuf message for this policy.
func (p *Policy) Proto() proto.Message {
	return p.proto
}

// TransformStage returns the execution stage of this transform policy
// (TransformStageRename -> TransformStageAdd).
func (p *Policy) TransformStage() googlepolicy.TransformStage {
	return p.stage
}

// IsDatapointLevel reports whether any matcher or action targets datapoint attributes.
func (p *Policy) IsDatapointLevel() bool {
	return p.isDatapointLevel
}

// TransformMetric evaluates the policy against the given MetricContext and, if
// all matchers evaluate to true, applies its configured action.
// It returns TransformModified if the policy matched and modified the metric or
// datapoint, or TransformNoMatch otherwise.
func (p *Policy) TransformMetric(ctx googlepolicy.MetricContext) googlepolicy.TransformResult {
	if !p.matchesContext(ctx) {
		return googlepolicy.TransformNoMatch
	}
	if !p.action(ctx) {
		return googlepolicy.TransformNoMatch
	}
	return googlepolicy.TransformModified
}

func (p *Policy) matchesContext(ctx googlepolicy.MetricContext) bool {
	for i := range p.matchers {
		if !p.matchers[i].Eval(ctx) {
			return false
		}
	}
	return true
}

func compileAdd(a *policyv1alpha1.MetricAddAction) (compiledAction, bool, error) {
	target := a.GetTarget()
	if target == nil || target.Target == nil {
		return nil, false, ErrMissingTarget
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.MetricFieldSelector_DatapointAttribute:
		setAttr, err := matcher.CompilePathSetter(t.DatapointAttribute, "datapoint", a.GetValue(), false)
		if err != nil {
			return nil, false, err
		}
		return func(ctx googlepolicy.MetricContext) bool {
			return setAttr(ctx.DatapointAttributes)
		}, true, nil

	case *policyv1alpha1.MetricFieldSelector_ResourceAttribute:
		setAttr, err := matcher.CompilePathSetter(t.ResourceAttribute, "resource", a.GetValue(), false)
		if err != nil {
			return nil, false, err
		}
		return func(ctx googlepolicy.MetricContext) bool {
			if ctx.Resource == (pcommon.Resource{}) {
				return false
			}
			return setAttr(ctx.Resource.Attributes())
		}, false, nil

	case *policyv1alpha1.MetricFieldSelector_ScopeAttribute:
		setAttr, err := matcher.CompilePathSetter(t.ScopeAttribute, "scope", a.GetValue(), false)
		if err != nil {
			return nil, false, err
		}
		return func(ctx googlepolicy.MetricContext) bool {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return false
			}
			return setAttr(ctx.Scope.Attributes())
		}, false, nil

	case *policyv1alpha1.MetricFieldSelector_DescriptorField:
		fn, err := compileDescriptorFieldAdd(t.DescriptorField, a.GetValue())
		return fn, false, err

	case *policyv1alpha1.MetricFieldSelector_ScopeField:
		fn, err := compileScopeFieldAdd(t.ScopeField, a.GetValue())
		return fn, false, err

	default:
		return nil, false, ErrMissingTarget
	}
}

func compileDescriptorFieldAdd(field policyv1alpha1.MetricDescriptorField, val *policyv1alpha1.Value) (compiledAction, error) {
	switch field {
	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME:
		s, err := requireStringValue(val, "metric descriptor field NAME")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) bool {
			if ctx.Metric == (pmetric.Metric{}) || ctx.Metric.Name() != "" {
				return false
			}
			ctx.Metric.SetName(s)
			return true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION:
		s, err := requireStringValue(val, "metric descriptor field DESCRIPTION")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) bool {
			if ctx.Metric == (pmetric.Metric{}) || ctx.Metric.Description() != "" {
				return false
			}
			ctx.Metric.SetDescription(s)
			return true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT:
		s, err := requireStringValue(val, "metric descriptor field UNIT")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) bool {
			if ctx.Metric == (pmetric.Metric{}) || ctx.Metric.Unit() != "" {
				return false
			}
			ctx.Metric.SetUnit(s)
			return true
		}, nil

	case policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE,
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY,
		policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC:
		return nil, fmt.Errorf("metric descriptor field %s is not supported as a transform target", field)

	default:
		return nil, errors.New("metric descriptor field cannot be unspecified")
	}
}

func compileScopeFieldAdd(field policyv1alpha1.ScopeField, val *policyv1alpha1.Value) (compiledAction, error) {
	switch field {
	case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
		s, err := requireStringValue(val, "scope field NAME")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) bool {
			if ctx.Scope == (pcommon.InstrumentationScope{}) || ctx.Scope.Name() != "" {
				return false
			}
			ctx.Scope.SetName(s)
			return true
		}, nil

	case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
		s, err := requireStringValue(val, "scope field VERSION")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.MetricContext) bool {
			if ctx.Scope == (pcommon.InstrumentationScope{}) || ctx.Scope.Version() != "" {
				return false
			}
			ctx.Scope.SetVersion(s)
			return true
		}, nil

	case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
		return nil, errors.New("scope field SCHEMA_URL is not supported as a transform target")

	default:
		return nil, errors.New("scope field cannot be unspecified")
	}
}

func requireStringValue(val *policyv1alpha1.Value, targetName string) (string, error) {
	if val == nil || val.GetValue() == nil {
		return "", ErrMissingValue
	}
	sv, ok := val.GetValue().(*policyv1alpha1.Value_StringValue)
	if !ok {
		return "", fmt.Errorf("%s requires a string value, got %T", targetName, val.GetValue())
	}
	if sv.StringValue == "" {
		return "", fmt.Errorf("%s requires a non-empty string value", targetName)
	}
	return sv.StringValue, nil
}
