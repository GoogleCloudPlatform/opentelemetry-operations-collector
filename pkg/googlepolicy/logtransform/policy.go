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
	// ErrMissingSource is returned when a rename action does not specify a source field selector.
	ErrMissingSource = errors.New("log transform rename action must specify a source field selector")
	// ErrMissingValue is returned when an action does not specify a valid value.
	ErrMissingValue = matcher.ErrMissingValue
	// ErrImmutableField is returned when an action targets a read-only field (such as trace_id or span_id).
	ErrImmutableField = errors.New("cannot mutate read-only field")
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
		if a.Rename == nil {
			return nil, ErrMissingAction
		}
		stage = googlepolicy.TransformStageRename
		action, err = compileRename(a.Rename)
		if err != nil {
			return nil, fmt.Errorf("rename: %w", err)
		}
	case *policyv1alpha1.LogTransformPolicy_Redact:
		if a.Redact == nil {
			return nil, ErrMissingAction
		}
		stage = googlepolicy.TransformStageRedact
		action, err = compileRedact(a.Redact)
		if err != nil {
			return nil, fmt.Errorf("redact: %w", err)
		}
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
	case *policyv1alpha1.LogTargetSelector_LogAttribute:
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

	case *policyv1alpha1.LogTargetSelector_BodyField:
		setBodyField, err := matcher.CompilePathSetter(t.BodyField, "body", a.GetValue(), a.GetUpsert())
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			body := ctx.Record.Body()
			if !bodyExists(body) {
				body.SetEmptyMap()
			} else if body.Type() != pcommon.ValueTypeMap {
				return false
			}
			return setBodyField(body.Map())
		}, nil

	case *policyv1alpha1.LogTargetSelector_RecordField:
		return compileRecordFieldAdd(t.RecordField, a.GetValue(), a.GetUpsert())

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

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
		policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
		return nil, fmt.Errorf("%w: %s", ErrImmutableField, field)

	default:
		return nil, errors.New("log record field cannot be unspecified")
	}
}

func compileRemove(a *policyv1alpha1.LogRemoveAction) (compiledAction, error) {
	if a == nil || a.Target == nil || a.Target.Target == nil {
		return nil, ErrMissingTarget
	}

	switch t := a.Target.Target.(type) {
	case *policyv1alpha1.LogAttributeSelector_LogAttribute:
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

	case *policyv1alpha1.LogAttributeSelector_BodyField:
		rmBody, err := matcher.CompilePathRemover(t.BodyField, "body")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			body := ctx.Record.Body()
			if body.Type() != pcommon.ValueTypeMap {
				return false
			}
			return rmBody(body.Map())
		}, nil

	default:
		return nil, ErrMissingTarget
	}
}

func compileRename(a *policyv1alpha1.LogRenameAction) (compiledAction, error) {
	if a == nil {
		return nil, ErrMissingAction
	}
	if a.Source == nil || a.Source.Target == nil {
		return nil, ErrMissingSource
	}
	if a.Target == nil || a.Target.Target == nil {
		return nil, ErrMissingTarget
	}

	if proto.Equal(a.Source, a.Target) {
		return func(_ googlepolicy.LogContext) bool {
			return false
		}, nil
	}

	var (
		srcAttrPath *policyv1alpha1.AttributePath
		srcKind     string
		srcInBody   bool
	)
	switch s := a.Source.Target.(type) {
	case *policyv1alpha1.LogAttributeSelector_LogAttribute:
		srcAttrPath = s.LogAttribute
		srcKind = "log"
	case *policyv1alpha1.LogAttributeSelector_BodyField:
		srcAttrPath = s.BodyField
		srcKind = "body"
		srcInBody = true
	default:
		return nil, ErrMissingSource
	}

	var (
		tgtAttrPath *policyv1alpha1.AttributePath
		tgtKind     string
		tgtInBody   bool
	)
	switch t := a.Target.Target.(type) {
	case *policyv1alpha1.LogAttributeSelector_LogAttribute:
		tgtAttrPath = t.LogAttribute
		tgtKind = "log"
	case *policyv1alpha1.LogAttributeSelector_BodyField:
		tgtAttrPath = t.BodyField
		tgtKind = "body"
		tgtInBody = true
	default:
		return nil, ErrMissingTarget
	}

	srcSteps, err := matcher.CompilePath(srcAttrPath, srcKind)
	if err != nil {
		return nil, err
	}
	tgtSteps, err := matcher.CompilePath(tgtAttrPath, tgtKind)
	if err != nil {
		return nil, err
	}

	if srcInBody == tgtInBody && isPathPrefix(srcAttrPath.GetPath(), tgtAttrPath.GetPath()) {
		return nil, errors.New("rename source cannot be an ancestor of target")
	}

	removeSource, err := matcher.CompilePathRemover(srcAttrPath, srcKind)
	if err != nil {
		return nil, err
	}

	writeTarget, err := matcher.CompilePathValueSetter(tgtAttrPath, tgtKind, a.GetUpsert())
	if err != nil {
		return nil, err
	}

	upsert := a.GetUpsert()

	return func(ctx googlepolicy.LogContext) bool {
		if ctx.Record == (plog.LogRecord{}) {
			return false
		}

		var srcMap pcommon.Map
		if srcInBody {
			body := ctx.Record.Body()
			if body.Type() != pcommon.ValueTypeMap {
				return false
			}
			srcMap = body.Map()
		} else {
			srcMap = ctx.Record.Attributes()
		}

		srcVal, ok := matcher.EvalPathValue(srcMap, srcSteps)
		if !ok {
			return false
		}

		var tgtMap pcommon.Map
		if tgtInBody {
			body := ctx.Record.Body()
			if body.Type() == pcommon.ValueTypeEmpty {
				body.SetEmptyMap()
			} else if body.Type() != pcommon.ValueTypeMap {
				return false
			}
			tgtMap = body.Map()
		} else {
			tgtMap = ctx.Record.Attributes()
		}

		if !upsert && matcher.PathExists(tgtMap, tgtSteps) {
			return false
		}

		cloned := pcommon.NewValueEmpty()
		srcVal.CopyTo(cloned)
		removeSource(srcMap)
		return writeTarget(tgtMap, cloned)
	}, nil
}

func compileRedact(a *policyv1alpha1.LogRedactAction) (compiledAction, error) {
	if a == nil {
		return nil, ErrMissingAction
	}
	target := a.GetTarget()
	if target == nil || target.Target == nil {
		return nil, ErrMissingTarget
	}
	redactStr, err := matcher.CompileStringRedactor(a.GetRegex(), a.GetReplacement())
	if err != nil {
		return nil, err
	}

	switch t := target.Target.(type) {
	case *policyv1alpha1.LogTargetSelector_LogAttribute:
		steps, err := matcher.CompilePath(t.LogAttribute, "log")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			val, ok := matcher.EvalPathValue(ctx.Record.Attributes(), steps)
			if !ok || val.Type() != pcommon.ValueTypeStr {
				return false
			}
			redacted, modified := redactStr(val.Str())
			if !modified {
				return false
			}
			val.SetStr(redacted)
			return true
		}, nil

	case *policyv1alpha1.LogTargetSelector_BodyField:
		steps, err := matcher.CompilePath(t.BodyField, "body")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			body := ctx.Record.Body()
			if body.Type() != pcommon.ValueTypeMap {
				return false
			}
			val, ok := matcher.EvalPathValue(body.Map(), steps)
			if !ok || val.Type() != pcommon.ValueTypeStr {
				return false
			}
			redacted, modified := redactStr(val.Str())
			if !modified {
				return false
			}
			val.SetStr(redacted)
			return true
		}, nil

	case *policyv1alpha1.LogTargetSelector_RecordField:
		return compileRecordFieldRedact(t.RecordField, redactStr)

	default:
		return nil, ErrMissingTarget
	}
}

func compileRecordFieldRedact(field policyv1alpha1.LogRecordField, redactStr func(string) (string, bool)) (compiledAction, error) {
	switch field {
	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			body := ctx.Record.Body()
			if body.Type() != pcommon.ValueTypeStr {
				return false
			}
			redacted, modified := redactStr(body.Str())
			if !modified {
				return false
			}
			body.SetStr(redacted)
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			sev := ctx.Record.SeverityText()
			redacted, modified := redactStr(sev)
			if !modified {
				return false
			}
			ctx.Record.SetSeverityText(redacted)
			return true
		}, nil

	case policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
		policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
		policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
		return nil, fmt.Errorf("log record field %s is not supported as a redact target", field)

	default:
		return nil, errors.New("log record field cannot be unspecified")
	}
}

func isPathPrefix(prefix, full []string) bool {
	if len(prefix) >= len(full) {
		return false
	}
	for i, seg := range prefix {
		if seg != full[i] {
			return false
		}
	}
	return true
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
