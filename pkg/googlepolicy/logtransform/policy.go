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

// Package logtransform compiles and evaluates LogTransformPolicy configurations.
// It is the single authoritative implementation for log record mutations: the
// collector processor walks surviving log records and delegates every
// transformation here in deterministic stage order (rename -> add -> remove -> redact).
package logtransform

import (
	"encoding/hex"
	"errors"
	"fmt"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/internal/matcher"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for log transform policies.
const PolicyType = "log_transform"

// driver loads a LogTransformPolicy from either shape it can arrive in: the
// proto a control plane sends, or the map an authored config decodes to.
var driver = googlepolicy.ProtoDriver[*policyv1alpha1.LogTransformPolicy]{
	New: func(pb *policyv1alpha1.LogTransformPolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(err)
	}
}

var (
	// ErrNilProto is returned when a nil LogTransformPolicy protobuf is provided.
	ErrNilProto = errors.New("log transform policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("log transform policy id cannot be empty")
	// ErrMissingAction is returned when the policy does not specify a transform action.
	ErrMissingAction = errors.New("log transform policy must specify an action")
	// ErrUnsupportedAction is returned when the policy specifies an action not yet supported.
	ErrUnsupportedAction = errors.New("log transform policy action is not supported in this release")
	// ErrMissingTarget is returned when an action does not specify a target field selector.
	ErrMissingTarget = errors.New("log transform action must specify a target field selector")
	// ErrMissingValue is returned when an action does not specify a valid value.
	ErrMissingValue = matcher.ErrMissingValue
)

type compiledAction func(ctx googlepolicy.LogContext) bool

// Policy represents a compiled, validated LogTransformPolicy ready for hot-loop evaluation.
type Policy struct {
	proto    *policyv1alpha1.LogTransformPolicy
	stage    googlepolicy.TransformStage
	matchers []matcher.CompiledLogMatcher
	action   compiledAction
}

var _ googlepolicy.TransformationPolicy = (*Policy)(nil)
var _ googlepolicy.LogTransformPolicyEvaluator = (*Policy)(nil)

// NewPolicyFromProto validates and compiles a LogTransformPolicy protobuf into a Policy.
func NewPolicyFromProto(pb *policyv1alpha1.LogTransformPolicy) (*Policy, error) {
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
		stage  googlepolicy.TransformStage
		action compiledAction
		err    error
	)
	switch a := pb.Action.(type) {
	case *policyv1alpha1.LogTransformPolicy_Add:
		if a.Add == nil {
			return nil, ErrMissingAction
		}
		stage = googlepolicy.TransformStageAdd
		action, err = compileAdd(a.Add)
		if err != nil {
			return nil, fmt.Errorf("add: %w", err)
		}
	case *policyv1alpha1.LogTransformPolicy_Remove:
		if a.Remove == nil {
			return nil, ErrMissingAction
		}
		stage = googlepolicy.TransformStageRemove
		action, err = compileRemove(a.Remove)
		if err != nil {
			return nil, fmt.Errorf("remove: %w", err)
		}
	case *policyv1alpha1.LogTransformPolicy_Rename:
		return nil, fmt.Errorf("rename: %w", ErrUnsupportedAction)
	case *policyv1alpha1.LogTransformPolicy_Redact:
		return nil, fmt.Errorf("redact: %w", ErrUnsupportedAction)
	default:
		return nil, ErrMissingAction
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
		stage:    stage,
		matchers: matchers,
		action:   action,
	}, nil
}

// PolicyName returns the unique identifier of this policy instance.
func (p *Policy) PolicyName() string {
	return p.proto.GetId()
}

// PolicyType returns the type identifier of this policy ("log_transform").
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns PolicyClassTransformation for log transform policies.
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

// Proto returns the underlying protobuf message for this policy.
func (p *Policy) Proto() proto.Message {
	return p.proto
}

// TransformStage returns the execution stage of this transform policy
// (TransformStageRename -> TransformStageAdd -> TransformStageRemove -> TransformStageRedact).
func (p *Policy) TransformStage() googlepolicy.TransformStage {
	return p.stage
}

// TransformLog evaluates the policy against the given LogContext and, if all
// matchers evaluate to true, applies its configured action.
// It returns TransformModified if the policy matched and modified the record,
// or TransformNoMatch otherwise.
func (p *Policy) TransformLog(ctx googlepolicy.LogContext) googlepolicy.TransformResult {
	if !p.matchesContext(ctx) {
		return googlepolicy.TransformNoMatch
	}
	if !p.action(ctx) {
		return googlepolicy.TransformNoMatch
	}
	return googlepolicy.TransformModified
}

func (p *Policy) matchesContext(ctx googlepolicy.LogContext) bool {
	for i := range p.matchers {
		if !p.matchers[i].Eval(ctx) {
			return false
		}
	}
	return true
}

func compileAdd(a *policyv1alpha1.LogAddAction) (compiledAction, error) {
	target := a.GetTarget()
	if target == nil || target.Target == nil {
		return nil, ErrMissingTarget
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.LogFieldSelector_LogAttribute:
		setAttr, err := matcher.CompilePathSetter(t.LogAttribute, "log", a.GetValue(), a.GetUpsert())
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			return setAttr(ctx.Record.Attributes())
		}, nil

	case *policyv1alpha1.LogFieldSelector_ResourceAttribute:
		setAttr, err := matcher.CompilePathSetter(t.ResourceAttribute, "resource", a.GetValue(), a.GetUpsert())
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Resource == (pcommon.Resource{}) {
				return false
			}
			return setAttr(ctx.Resource.Attributes())
		}, nil

	case *policyv1alpha1.LogFieldSelector_ScopeAttribute:
		setAttr, err := matcher.CompilePathSetter(t.ScopeAttribute, "scope", a.GetValue(), a.GetUpsert())
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return false
			}
			return setAttr(ctx.Scope.Attributes())
		}, nil

	case *policyv1alpha1.LogFieldSelector_RecordField:
		return compileRecordFieldAdd(t.RecordField, a.GetValue(), a.GetUpsert())

	case *policyv1alpha1.LogFieldSelector_ScopeField:
		return compileScopeFieldAdd(t.ScopeField, a.GetValue(), a.GetUpsert())

	default:
		return nil, ErrMissingTarget
	}
}

func compileRecordFieldAdd(field policyv1alpha1.LogRecordField, val *policyv1alpha1.Value, upsert bool) (compiledAction, error) {
	switch field {
	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY:
		setBody, err := matcher.CompileValueSetter(val)
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			body := ctx.Record.Body()
			if !upsert && bodyExists(body) {
				return false
			}
			setBody(body)
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT:
		s, err := requireStringValue(val, "log record field SEVERITY_TEXT")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			if !upsert && ctx.Record.SeverityText() != "" {
				return false
			}
			ctx.Record.SetSeverityText(s)
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER:
		sev, err := requireSeverityNumber(val)
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			if !upsert && ctx.Record.SeverityNumber() != plog.SeverityNumberUnspecified {
				return false
			}
			ctx.Record.SetSeverityNumber(sev)
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID:
		tid, err := decodeTraceID(val)
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			if !upsert && !ctx.Record.TraceID().IsEmpty() {
				return false
			}
			ctx.Record.SetTraceID(tid)
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
		sid, err := decodeSpanID(val)
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			if !upsert && !ctx.Record.SpanID().IsEmpty() {
				return false
			}
			ctx.Record.SetSpanID(sid)
			return true
		}, nil

	default:
		return nil, errors.New("log record field cannot be unspecified")
	}
}

func compileScopeFieldAdd(field policyv1alpha1.ScopeField, val *policyv1alpha1.Value, upsert bool) (compiledAction, error) {
	switch field {
	case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
		s, err := requireStringValue(val, "scope field NAME")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return false
			}
			if !upsert && ctx.Scope.Name() != "" {
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
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return false
			}
			if !upsert && ctx.Scope.Version() != "" {
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

func compileRemove(a *policyv1alpha1.LogRemoveAction) (compiledAction, error) {
	return compileTargetRemover(a.GetTarget())
}

func compileTargetRemover(target *policyv1alpha1.LogFieldSelector) (compiledAction, error) {
	if target == nil || target.Target == nil {
		return nil, ErrMissingTarget
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.LogFieldSelector_LogAttribute:
		rmAttr, err := matcher.CompilePathRemover(t.LogAttribute, "log")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			return rmAttr(ctx.Record.Attributes())
		}, nil

	case *policyv1alpha1.LogFieldSelector_ResourceAttribute:
		rmAttr, err := matcher.CompilePathRemover(t.ResourceAttribute, "resource")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Resource == (pcommon.Resource{}) {
				return false
			}
			return rmAttr(ctx.Resource.Attributes())
		}, nil

	case *policyv1alpha1.LogFieldSelector_ScopeAttribute:
		rmAttr, err := matcher.CompilePathRemover(t.ScopeAttribute, "scope")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Scope == (pcommon.InstrumentationScope{}) {
				return false
			}
			return rmAttr(ctx.Scope.Attributes())
		}, nil

	case *policyv1alpha1.LogFieldSelector_RecordField:
		return compileRecordFieldRemove(t.RecordField)

	case *policyv1alpha1.LogFieldSelector_ScopeField:
		return compileScopeFieldRemove(t.ScopeField)

	default:
		return nil, ErrMissingTarget
	}
}

func compileRecordFieldRemove(field policyv1alpha1.LogRecordField) (compiledAction, error) {
	switch field {
	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY:
		emptyBody := pcommon.NewValueEmpty()
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			body := ctx.Record.Body()
			if !bodyExists(body) {
				return false
			}
			emptyBody.CopyTo(body)
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) || ctx.Record.SeverityText() == "" {
				return false
			}
			ctx.Record.SetSeverityText("")
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) || ctx.Record.SeverityNumber() == plog.SeverityNumberUnspecified {
				return false
			}
			ctx.Record.SetSeverityNumber(plog.SeverityNumberUnspecified)
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) || ctx.Record.TraceID().IsEmpty() {
				return false
			}
			ctx.Record.SetTraceID(pcommon.TraceID{})
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) || ctx.Record.SpanID().IsEmpty() {
				return false
			}
			ctx.Record.SetSpanID(pcommon.SpanID{})
			return true
		}, nil

	default:
		return nil, errors.New("log record field cannot be unspecified")
	}
}

func compileScopeFieldRemove(field policyv1alpha1.ScopeField) (compiledAction, error) {
	switch field {
	case policyv1alpha1.ScopeField_SCOPE_FIELD_NAME:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Scope == (pcommon.InstrumentationScope{}) || ctx.Scope.Name() == "" {
				return false
			}
			ctx.Scope.SetName("")
			return true
		}, nil

	case policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Scope == (pcommon.InstrumentationScope{}) || ctx.Scope.Version() == "" {
				return false
			}
			ctx.Scope.SetVersion("")
			return true
		}, nil

	case policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL:
		return nil, errors.New("scope field SCHEMA_URL is not supported as a transform target")

	default:
		return nil, errors.New("scope field cannot be unspecified")
	}
}

func bodyExists(v pcommon.Value) bool {
	switch v.Type() {
	case pcommon.ValueTypeEmpty:
		return false
	case pcommon.ValueTypeStr:
		return v.Str() != ""
	default:
		return true
	}
}

func requireStringValue(val *policyv1alpha1.Value, targetName string) (string, error) {
	if val == nil || val.GetValue() == nil {
		return "", ErrMissingValue
	}
	sv, ok := val.GetValue().(*policyv1alpha1.Value_StringValue)
	if !ok {
		return "", fmt.Errorf("%s requires a string value", targetName)
	}
	return sv.StringValue, nil
}

func requireSeverityNumber(val *policyv1alpha1.Value) (plog.SeverityNumber, error) {
	if val == nil || val.GetValue() == nil {
		return plog.SeverityNumberUnspecified, ErrMissingValue
	}
	iv, ok := val.GetValue().(*policyv1alpha1.Value_IntValue)
	if !ok {
		return plog.SeverityNumberUnspecified, errors.New("log record field SEVERITY_NUMBER requires an int value")
	}
	if iv.IntValue < int64(plog.SeverityNumberTrace) || iv.IntValue > int64(plog.SeverityNumberFatal4) {
		return plog.SeverityNumberUnspecified, fmt.Errorf("log record field SEVERITY_NUMBER must be in range [1, 24], got %d", iv.IntValue)
	}
	return plog.SeverityNumber(iv.IntValue), nil
}

func decodeTraceID(val *policyv1alpha1.Value) (pcommon.TraceID, error) {
	raw, err := decodeFixedID(val, 16, "log record field TRACE_ID requires a 32-character hex string or 16-byte bytes value")
	if err != nil {
		return pcommon.TraceID{}, err
	}
	var tid pcommon.TraceID
	copy(tid[:], raw)
	return tid, nil
}

func decodeSpanID(val *policyv1alpha1.Value) (pcommon.SpanID, error) {
	raw, err := decodeFixedID(val, 8, "log record field SPAN_ID requires a 16-character hex string or 8-byte bytes value")
	if err != nil {
		return pcommon.SpanID{}, err
	}
	var sid pcommon.SpanID
	copy(sid[:], raw)
	return sid, nil
}

func decodeFixedID(val *policyv1alpha1.Value, byteLen int, errMsg string) ([]byte, error) {
	if val == nil || val.GetValue() == nil {
		return nil, ErrMissingValue
	}
	switch v := val.GetValue().(type) {
	case *policyv1alpha1.Value_StringValue:
		b, err := hex.DecodeString(v.StringValue)
		if err != nil || len(b) != byteLen {
			return nil, errors.New(errMsg)
		}
		return b, nil
	case *policyv1alpha1.Value_BytesValue:
		if len(v.BytesValue) != byteLen {
			return nil, errors.New(errMsg)
		}
		return v.BytesValue, nil
	default:
		return nil, errors.New(errMsg)
	}
}
