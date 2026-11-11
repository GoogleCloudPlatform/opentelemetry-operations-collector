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

	policyv1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1"
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
var driver = googlepolicy.ProtoDriver[*policyv1.LogTransformPolicy]{
	New: func(pb *policyv1.LogTransformPolicy) (googlepolicy.Policy, error) {
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
	// ErrMissingAction is returned when the policy does not specify at least one transform operation.
	ErrMissingAction = errors.New("log transform policy must specify at least one operation")
	// ErrUnsupportedAction is returned when the policy specifies an operation variant not yet supported.
	ErrUnsupportedAction = errors.New("log transform policy operation is not supported in this release")
	// ErrMissingTarget is returned when an operation does not specify a target field selector.
	ErrMissingTarget = errors.New("log transform operation must specify a target field selector")
	// ErrMissingSource is returned when a rename operation does not specify a source field selector.
	ErrMissingSource = errors.New("log transform rename operation must specify a source field selector")
	// ErrMissingValue is returned when an addition operation does not specify a valid value.
	ErrMissingValue = matcher.ErrMissingValue
	// ErrImmutableField is returned when an operation targets a read-only field (such as trace_id or span_id).
	ErrImmutableField = errors.New("cannot mutate read-only field")
)

type compiledAction func(ctx googlepolicy.LogContext) bool

// Policy represents a compiled, validated LogTransformPolicy ready for hot-loop evaluation.
type Policy struct {
	proto      *policyv1.LogTransformPolicy
	matchers   []matcher.CompiledLogMatcher
	renamings  []compiledAction
	additions  []compiledAction
	removals   []compiledAction
	redactions []compiledAction
}

var _ googlepolicy.TransformationPolicy = (*Policy)(nil)
var _ googlepolicy.LogTransformPolicyEvaluator = (*Policy)(nil)

// NewPolicyFromProto validates and compiles a LogTransformPolicy protobuf into a Policy.
func NewPolicyFromProto(pb *policyv1.LogTransformPolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}
	if pb.GetId() == "" {
		return nil, ErrMissingID
	}
	if len(pb.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	if len(pb.GetRenamings()) == 0 &&
		len(pb.GetAdditions()) == 0 &&
		len(pb.GetRemovals()) == 0 &&
		len(pb.GetRedactions()) == 0 {
		return nil, ErrMissingAction
	}

	var renamings []compiledAction
	if len(pb.GetRenamings()) > 0 {
		renamings = make([]compiledAction, 0, len(pb.GetRenamings()))
		for i, op := range pb.GetRenamings() {
			cr, err := compileRename(op)
			if err != nil {
				return nil, fmt.Errorf("renamings[%d]: %w", i, err)
			}
			renamings = append(renamings, cr)
		}
	}

	var additions []compiledAction
	if len(pb.GetAdditions()) > 0 {
		additions = make([]compiledAction, 0, len(pb.GetAdditions()))
		for i, op := range pb.GetAdditions() {
			ca, err := compileAdd(op)
			if err != nil {
				return nil, fmt.Errorf("additions[%d]: %w", i, err)
			}
			additions = append(additions, ca)
		}
	}

	var removals []compiledAction
	if len(pb.GetRemovals()) > 0 {
		removals = make([]compiledAction, 0, len(pb.GetRemovals()))
		for i, op := range pb.GetRemovals() {
			cr, err := compileRemove(op)
			if err != nil {
				return nil, fmt.Errorf("removals[%d]: %w", i, err)
			}
			removals = append(removals, cr)
		}
	}

	var redactions []compiledAction
	if len(pb.GetRedactions()) > 0 {
		redactions = make([]compiledAction, 0, len(pb.GetRedactions()))
		for i, op := range pb.GetRedactions() {
			crd, err := compileRedact(op)
			if err != nil {
				return nil, fmt.Errorf("redactions[%d]: %w", i, err)
			}
			redactions = append(redactions, crd)
		}
	}

	matchers := make([]matcher.CompiledLogMatcher, 0, len(pb.GetMatchers()))
	for i, m := range pb.GetMatchers() {
		cm, err := matcher.CompileLogMatcher(m)
		if err != nil {
			return nil, fmt.Errorf("matcher[%d]: %w", i, err)
		}
		matchers = append(matchers, cm)
	}

	return &Policy{
		proto:      pb,
		matchers:   matchers,
		renamings:  renamings,
		additions:  additions,
		removals:   removals,
		redactions: redactions,
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
	if len(p.renamings) == 0 && len(p.additions) == 0 && len(p.removals) == 0 && len(p.redactions) == 0 {
		return ErrMissingAction
	}
	return nil
}

// Proto returns the underlying protobuf message for this policy.
func (p *Policy) Proto() proto.Message {
	return p.proto
}

// MatchesLog reports whether every matcher in the policy matches the given
// LogContext (or true when matchers is empty).
func (p *Policy) MatchesLog(ctx googlepolicy.LogContext) bool {
	for i := range p.matchers {
		if !p.matchers[i].Eval(ctx) {
			return false
		}
	}
	return true
}

// TransformLogStage executes this policy's compiled operations for the given
// stage in slice order without re-evaluating matchers. It returns
// TransformModified if at least one operation in the stage modified ctx, or
// TransformNoMatch otherwise.
func (p *Policy) TransformLogStage(stage googlepolicy.TransformStage, ctx googlepolicy.LogContext) googlepolicy.TransformResult {
	var ops []compiledAction
	switch stage {
	case googlepolicy.TransformStageRename:
		ops = p.renamings
	case googlepolicy.TransformStageAdd:
		ops = p.additions
	case googlepolicy.TransformStageRemove:
		ops = p.removals
	case googlepolicy.TransformStageRedact:
		ops = p.redactions
	default:
		return googlepolicy.TransformNoMatch
	}
	var modified bool
	for _, op := range ops {
		if op(ctx) {
			modified = true
		}
	}
	if !modified {
		return googlepolicy.TransformNoMatch
	}
	return googlepolicy.TransformModified
}

// TransformLog evaluates MatchesLog once against the pre-transform LogContext
// and, if all matchers evaluate to true, executes this policy's configured
// operations in global stage order (Rename -> Add -> Remove -> Redact).
// It returns TransformModified if the policy matched and at least one operation
// modified the record, or TransformNoMatch otherwise.
func (p *Policy) TransformLog(ctx googlepolicy.LogContext) googlepolicy.TransformResult {
	if !p.MatchesLog(ctx) {
		return googlepolicy.TransformNoMatch
	}
	var modified bool
	for _, stage := range [...]googlepolicy.TransformStage{
		googlepolicy.TransformStageRename,
		googlepolicy.TransformStageAdd,
		googlepolicy.TransformStageRemove,
		googlepolicy.TransformStageRedact,
	} {
		if p.TransformLogStage(stage, ctx) == googlepolicy.TransformModified {
			modified = true
		}
	}
	if !modified {
		return googlepolicy.TransformNoMatch
	}
	return googlepolicy.TransformModified
}

func compileAdd(a *policyv1.LogAddition) (compiledAction, error) {
	if a == nil {
		return nil, ErrMissingAction
	}
	if len(a.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	target := a.GetTarget()
	if target == nil {
		return nil, ErrMissingTarget
	}
	if len(target.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	if target.Target == nil {
		return nil, ErrMissingTarget
	}

	var val *policyv1.Value
	switch s := a.GetSource().(type) {
	case *policyv1.LogAddition_Value:
		if s == nil || s.Value == nil {
			return nil, ErrMissingValue
		}
		val = s.Value
	case nil:
		return nil, ErrMissingValue
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedAction, s)
	}

	switch t := target.Target.(type) {
	case *policyv1.LogTargetSelector_LogAttributePath:
		if t == nil {
			return nil, ErrMissingTarget
		}
		setAttr, err := matcher.CompilePathSetter(t.LogAttributePath, "log", val, a.GetUpsertEnabled())
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			return setAttr(ctx.Record.Attributes())
		}, nil

	case *policyv1.LogTargetSelector_BodyFieldPath:
		if t == nil {
			return nil, ErrMissingTarget
		}
		setBodyField, err := matcher.CompilePathSetter(t.BodyFieldPath, "body", val, a.GetUpsertEnabled())
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

	case *policyv1.LogTargetSelector_RecordField:
		if t == nil {
			return nil, ErrMissingTarget
		}
		return compileRecordFieldAdd(t.RecordField, val, a.GetUpsertEnabled())

	default:
		return nil, ErrMissingTarget
	}
}

func compileRecordFieldAdd(field policyv1.LogRecordField, val *policyv1.Value, upsert bool) (compiledAction, error) {
	switch field {
	case policyv1.LogRecordField_LOG_RECORD_FIELD_BODY:
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

	case policyv1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT:
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

	case policyv1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER:
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

	case policyv1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
		policyv1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
		return nil, fmt.Errorf("%w: %s", ErrImmutableField, field)

	default:
		return nil, errors.New("log record field cannot be unspecified")
	}
}

func compileRemove(a *policyv1.LogRemoval) (compiledAction, error) {
	if a == nil {
		return nil, ErrMissingTarget
	}
	if len(a.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	if a.Target == nil {
		return nil, ErrMissingTarget
	}
	if len(a.Target.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	if a.Target.Target == nil {
		return nil, ErrMissingTarget
	}

	switch t := a.Target.Target.(type) {
	case *policyv1.LogAttributeSelector_LogAttributePath:
		if t == nil {
			return nil, ErrMissingTarget
		}
		rmAttr, err := matcher.CompilePathRemover(t.LogAttributePath, "log")
		if err != nil {
			return nil, err
		}
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			return rmAttr(ctx.Record.Attributes())
		}, nil

	case *policyv1.LogAttributeSelector_BodyFieldPath:
		if t == nil {
			return nil, ErrMissingTarget
		}
		rmBody, err := matcher.CompilePathRemover(t.BodyFieldPath, "body")
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

func compileRename(a *policyv1.LogRenaming) (compiledAction, error) {
	if a == nil {
		return nil, ErrMissingAction
	}
	if len(a.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	if a.Source == nil {
		return nil, ErrMissingSource
	}
	if len(a.Source.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	if a.Source.Target == nil {
		return nil, ErrMissingSource
	}
	if a.Target == nil {
		return nil, ErrMissingTarget
	}
	if len(a.Target.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	if a.Target.Target == nil {
		return nil, ErrMissingTarget
	}

	var (
		srcAttrPath *policyv1.AttributePath
		srcKind     string
		srcInBody   bool
	)
	switch s := a.Source.Target.(type) {
	case *policyv1.LogAttributeSelector_LogAttributePath:
		if s == nil {
			return nil, ErrMissingSource
		}
		srcAttrPath = s.LogAttributePath
		srcKind = "log"
	case *policyv1.LogAttributeSelector_BodyFieldPath:
		if s == nil {
			return nil, ErrMissingSource
		}
		srcAttrPath = s.BodyFieldPath
		srcKind = "body"
		srcInBody = true
	default:
		return nil, ErrMissingSource
	}

	var (
		tgtAttrPath *policyv1.AttributePath
		tgtKind     string
		tgtInBody   bool
	)
	switch t := a.Target.Target.(type) {
	case *policyv1.LogAttributeSelector_LogAttributePath:
		if t == nil {
			return nil, ErrMissingTarget
		}
		tgtAttrPath = t.LogAttributePath
		tgtKind = "log"
	case *policyv1.LogAttributeSelector_BodyFieldPath:
		if t == nil {
			return nil, ErrMissingTarget
		}
		tgtAttrPath = t.BodyFieldPath
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

	if proto.Equal(a.Source, a.Target) {
		return func(_ googlepolicy.LogContext) bool {
			return false
		}, nil
	}

	if srcInBody == tgtInBody {
		if isPathPrefix(srcAttrPath.GetSegments(), tgtAttrPath.GetSegments()) {
			return nil, errors.New("rename source cannot be an ancestor of target")
		}
		if isPathPrefix(tgtAttrPath.GetSegments(), srcAttrPath.GetSegments()) {
			return nil, errors.New("rename target cannot be an ancestor of source")
		}
	}

	removeSource, err := matcher.CompilePathRemover(srcAttrPath, srcKind)
	if err != nil {
		return nil, err
	}

	writeTarget, err := matcher.CompilePathValueSetter(tgtAttrPath, tgtKind, a.GetUpsertEnabled())
	if err != nil {
		return nil, err
	}

	upsert := a.GetUpsertEnabled()

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
			if !bodyExists(body) {
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

		if !writeTarget(tgtMap, srcVal) {
			return false
		}
		removeSource(srcMap)
		return true
	}, nil
}

func compileRedact(a *policyv1.LogRedaction) (compiledAction, error) {
	if a == nil {
		return nil, ErrMissingAction
	}
	if len(a.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	target := a.GetTarget()
	if target == nil {
		return nil, ErrMissingTarget
	}
	if len(target.ProtoReflect().GetUnknown()) > 0 {
		return nil, ErrUnsupportedAction
	}
	if target.Target == nil {
		return nil, ErrMissingTarget
	}
	var regex string
	switch p := a.GetPattern().(type) {
	case nil:
	case *policyv1.LogRedaction_Regex:
		if p != nil {
			regex = p.Regex
		}
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedAction, p)
	}
	var replacement string
	switch m := a.GetMask().(type) {
	case nil:
	case *policyv1.LogRedaction_Replacement:
		if m != nil {
			replacement = m.Replacement
		}
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedAction, m)
	}
	redactStr, err := matcher.CompileStringRedactor(regex, replacement)
	if err != nil {
		return nil, err
	}

	switch t := target.Target.(type) {
	case *policyv1.LogTargetSelector_LogAttributePath:
		if t == nil {
			return nil, ErrMissingTarget
		}
		steps, err := matcher.CompilePath(t.LogAttributePath, "log")
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

	case *policyv1.LogTargetSelector_BodyFieldPath:
		if t == nil {
			return nil, ErrMissingTarget
		}
		steps, err := matcher.CompilePath(t.BodyFieldPath, "body")
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

	case *policyv1.LogTargetSelector_RecordField:
		if t == nil {
			return nil, ErrMissingTarget
		}
		return compileRecordFieldRedact(t.RecordField, redactStr)

	default:
		return nil, ErrMissingTarget
	}
}

func compileRecordFieldRedact(field policyv1.LogRecordField, redactStr func(string) (string, bool)) (compiledAction, error) {
	switch field {
	case policyv1.LogRecordField_LOG_RECORD_FIELD_BODY:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			body := ctx.Record.Body()
			if body.Type() != pcommon.ValueTypeStr || body.Str() == "" {
				return false
			}
			redacted, modified := redactStr(body.Str())
			if !modified {
				return false
			}
			body.SetStr(redacted)
			return true
		}, nil

	case policyv1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT:
		return func(ctx googlepolicy.LogContext) bool {
			if ctx.Record == (plog.LogRecord{}) {
				return false
			}
			sev := ctx.Record.SeverityText()
			if sev == "" {
				return false
			}
			redacted, modified := redactStr(sev)
			if !modified {
				return false
			}
			ctx.Record.SetSeverityText(redacted)
			return true
		}, nil

	case policyv1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
		policyv1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
		policyv1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID:
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

func requireStringValue(val *policyv1.Value, targetName string) (string, error) {
	if val == nil || val.GetKind() == nil {
		return "", ErrMissingValue
	}
	sv, ok := val.GetKind().(*policyv1.Value_StringValue)
	if !ok {
		return "", fmt.Errorf("%s requires a string value", targetName)
	}
	if sv == nil {
		return "", ErrMissingValue
	}
	return sv.StringValue, nil
}

func requireSeverityNumber(val *policyv1.Value) (plog.SeverityNumber, error) {
	if val == nil || val.GetKind() == nil {
		return plog.SeverityNumberUnspecified, ErrMissingValue
	}
	iv, ok := val.GetKind().(*policyv1.Value_IntValue)
	if !ok {
		return plog.SeverityNumberUnspecified, errors.New("log record field SEVERITY_NUMBER requires an int value")
	}
	if iv == nil {
		return plog.SeverityNumberUnspecified, ErrMissingValue
	}
	if iv.IntValue < int64(plog.SeverityNumberTrace) || iv.IntValue > int64(plog.SeverityNumberFatal4) {
		return plog.SeverityNumberUnspecified, fmt.Errorf("log record field SEVERITY_NUMBER must be in range [1, 24], got %d", iv.IntValue)
	}
	return plog.SeverityNumber(iv.IntValue), nil
}
