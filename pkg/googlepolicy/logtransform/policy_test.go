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

package logtransform

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func newTestLogBundle() (plog.LogRecord, plog.ScopeLogs, plog.ResourceLogs) {
	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("cloud.zone", "us-central1-a")

	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName("my.library")
	sl.Scope().SetVersion("v1.2.3")
	sl.SetSchemaUrl("https://opentelemetry.io/schemas/1.24.0")
	sl.Scope().Attributes().PutStr("scope.tag", "backend")

	lr := sl.LogRecords().AppendEmpty()
	lr.Body().SetStr("hello world from service")
	lr.SetSeverityText("INFO")
	lr.SetSeverityNumber(plog.SeverityNumberInfo)
	lr.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	lr.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	lr.Attributes().PutStr("http.method", "GET")
	lr.Attributes().PutInt("http.status_code", 200)
	lr.Attributes().PutDouble("latency_seconds", 1.25)
	lr.Attributes().PutBool("feature.enabled", true)
	lr.Attributes().PutEmptyBytes("binary.id").FromRaw([]byte{0x01, 0x02, 0x03})

	meta := lr.Attributes().PutEmptyMap("metadata")
	meta.PutStr("env", "prod")

	return lr, sl, rl
}

func logContext(log plog.LogRecord, scope plog.ScopeLogs, res plog.ResourceLogs) googlepolicy.LogContext {
	return googlepolicy.LogContext{
		Record:            log,
		Resource:          res.Resource(),
		Scope:             scope.Scope(),
		ResourceSchemaURL: res.SchemaUrl(),
		ScopeSchemaURL:    scope.SchemaUrl(),
	}
}

func recordFieldTarget(f policyv1alpha1.LogRecordField) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_RecordField{RecordField: f},
	}
}

func scopeFieldTarget(f policyv1alpha1.ScopeField) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeField{ScopeField: f},
	}
}

func logAttrTarget(path ...string) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
			LogAttributePath: &policyv1alpha1.AttributePath{Segments: path},
		},
	}
}

func resourceAttrTarget(path ...string) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ResourceAttributePath{
			ResourceAttributePath: &policyv1alpha1.AttributePath{Segments: path},
		},
	}
}

func scopeAttrTarget(path ...string) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeAttributePath{
			ScopeAttributePath: &policyv1alpha1.AttributePath{Segments: path},
		},
	}
}

func strVal(s string) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: s}}
}

func intVal(n int64) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_IntValue{IntValue: n}}
}

func doubleVal(d float64) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_DoubleValue{DoubleValue: d}}
}

func boolVal(b bool) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_BoolValue{BoolValue: b}}
}

func bytesVal(b []byte) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_BytesValue{BytesValue: b}}
}

func bodyExistsMatcher() *policyv1alpha1.LogMatcher {
	return &policyv1alpha1.LogMatcher{
		Target:    recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY),
		Predicate: &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Existence{}},
	}
}

func alwaysTrueMatcher() *policyv1alpha1.LogMatcher {
	return &policyv1alpha1.LogMatcher{
		Target:    logAttrTarget("__never_present__"),
		Predicate: &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Existence{}},
		Negated:   true,
	}
}

func logTargetAttr(path ...string) *policyv1alpha1.LogTargetSelector {
	return &policyv1alpha1.LogTargetSelector{
		Target: &policyv1alpha1.LogTargetSelector_LogAttributePath{
			LogAttributePath: &policyv1alpha1.AttributePath{Segments: path},
		},
	}
}

func logTargetBodyField(path ...string) *policyv1alpha1.LogTargetSelector {
	return &policyv1alpha1.LogTargetSelector{
		Target: &policyv1alpha1.LogTargetSelector_BodyFieldPath{
			BodyFieldPath: &policyv1alpha1.AttributePath{Segments: path},
		},
	}
}

func logTargetRecordField(field policyv1alpha1.LogRecordField) *policyv1alpha1.LogTargetSelector {
	return &policyv1alpha1.LogTargetSelector{
		Target: &policyv1alpha1.LogTargetSelector_RecordField{
			RecordField: field,
		},
	}
}

func logAttrSelector(path ...string) *policyv1alpha1.LogAttributeSelector {
	return &policyv1alpha1.LogAttributeSelector{
		Target: &policyv1alpha1.LogAttributeSelector_LogAttributePath{
			LogAttributePath: &policyv1alpha1.AttributePath{Segments: path},
		},
	}
}

func addOps(target *policyv1alpha1.LogTargetSelector, val *policyv1alpha1.Value, upsert bool) []*policyv1alpha1.LogAddition {
	op := &policyv1alpha1.LogAddition{
		Target:        target,
		UpsertEnabled: proto.Bool(upsert),
	}
	if val != nil {
		op.Source = &policyv1alpha1.LogAddition_Value{Value: val}
	}
	return []*policyv1alpha1.LogAddition{op}
}

func logBodyFieldSelector(path ...string) *policyv1alpha1.LogAttributeSelector {
	return &policyv1alpha1.LogAttributeSelector{
		Target: &policyv1alpha1.LogAttributeSelector_BodyFieldPath{
			BodyFieldPath: &policyv1alpha1.AttributePath{Segments: path},
		},
	}
}

func removeOps(target *policyv1alpha1.LogAttributeSelector) []*policyv1alpha1.LogRemoval {
	return []*policyv1alpha1.LogRemoval{
		{
			Target: target,
		},
	}
}

func renameOps(source, target *policyv1alpha1.LogAttributeSelector, upsert bool) []*policyv1alpha1.LogRenaming {
	return []*policyv1alpha1.LogRenaming{
		{
			Source:        source,
			Target:        target,
			UpsertEnabled: proto.Bool(upsert),
		},
	}
}

func redactOps(target *policyv1alpha1.LogTargetSelector, regex, replacement string) []*policyv1alpha1.LogRedaction {
	op := &policyv1alpha1.LogRedaction{
		Target: target,
	}
	if regex != "" {
		op.Pattern = &policyv1alpha1.LogRedaction_Regex{Regex: regex}
	}
	if replacement != "" {
		op.Mask = &policyv1alpha1.LogRedaction_Replacement{Replacement: replacement}
	}
	return []*policyv1alpha1.LogRedaction{op}
}

func TestPolicyValidationErrors(t *testing.T) {
	validMatcher := bodyExistsMatcher()
	validAdd := addOps(logTargetAttr("tier"), strVal("soc"), false)

	tests := []struct {
		name      string
		proto     *policyv1alpha1.LogTransformPolicy
		errExpect error
		errSubstr string
	}{
		{
			name:      "nil proto",
			proto:     nil,
			errExpect: ErrNilProto,
		},
		{
			name: "missing id",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:        "",
				Matchers:  []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: validAdd,
			},
			errExpect: ErrMissingID,
		},
		{
			name: "missing operations (all four stage lists empty)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-no-ops",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "nil element in additions",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:        "p-nil-add-elem",
				Matchers:  []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: []*policyv1alpha1.LogAddition{nil},
			},
			errExpect: ErrMissingAction,
			errSubstr: "additions[0]:",
		},
		{
			name: "nil element in removals",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-nil-remove-elem",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Removals: []*policyv1alpha1.LogRemoval{nil},
			},
			errExpect: ErrMissingTarget,
			errSubstr: "removals[0]:",
		},
		{
			name: "nil element in renamings",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:        "p-nil-rename-elem",
				Matchers:  []*policyv1alpha1.LogMatcher{validMatcher},
				Renamings: []*policyv1alpha1.LogRenaming{nil},
			},
			errExpect: ErrMissingAction,
			errSubstr: "renamings[0]:",
		},
		{
			name: "nil element in redactions",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:         "p-nil-redact-elem",
				Matchers:   []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: []*policyv1alpha1.LogRedaction{nil},
			},
			errExpect: ErrMissingAction,
			errSubstr: "redactions[0]:",
		},
		{
			name: "remove missing target",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:     "p-remove-no-target",
				Removals: removeOps(nil),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "remove empty log_attribute path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:     "p-remove-empty-attr",
				Removals: removeOps(logAttrSelector()),
			},
			errSubstr: "log attribute path cannot be empty",
		},
		{
			name: "remove empty body_field path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:     "p-remove-empty-body",
				Removals: removeOps(logBodyFieldSelector()),
			},
			errSubstr: "body attribute path cannot be empty",
		},
		{
			name: "rename missing source",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:     "p-rename-no-src",
				Renamings: renameOps(nil, logAttrSelector("dst"), false),
			},
			errExpect: ErrMissingSource,
		},
		{
			name: "rename missing target",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:     "p-rename-no-tgt",
				Renamings: renameOps(logAttrSelector("src"), nil, false),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "rename empty source path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:     "p-rename-empty-src",
				Renamings: renameOps(logAttrSelector(), logAttrSelector("dst"), false),
			},
			errSubstr: "attribute path cannot be empty",
		},
		{
			name: "rename empty target path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:     "p-rename-empty-tgt",
				Renamings: renameOps(logAttrSelector("src"), logAttrSelector(), false),
			},
			errSubstr: "attribute path cannot be empty",
		},
		{
			name: "rename source is ancestor of target",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:     "p-rename-ancestor",
				Renamings: renameOps(logAttrSelector("user"), logAttrSelector("user", "name"), false),
			},
			errSubstr: "rename source cannot be an ancestor of target",
		},
		{
			name: "rename target is ancestor of source",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:     "p-rename-tgt-ancestor",
				Renamings: renameOps(logAttrSelector("user", "name"), logAttrSelector("user"), false),
			},
			errSubstr: "rename target cannot be an ancestor of source",
		},
		{
			name: "redact missing target (nil selector)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-nil-target",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: redactOps(nil, `\d+`, "[REDACTED]"),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "redact missing target (empty oneof)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-empty-target",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: redactOps(&policyv1alpha1.LogTargetSelector{}, `\d+`, "[REDACTED]"),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "redact empty log_attribute path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-empty-attr",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: redactOps(logTargetAttr(), `\d+`, "[REDACTED]"),
			},
			errSubstr: "log attribute path cannot be empty",
		},
		{
			name: "redact empty body_field path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-empty-body",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: redactOps(logTargetBodyField(), `\d+`, "[REDACTED]"),
			},
			errSubstr: "body attribute path cannot be empty",
		},
		{
			name: "redact unspecified record_field",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-unspec-field",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: redactOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_UNSPECIFIED), `\d+`, "[REDACTED]"),
			},
			errSubstr: "log record field cannot be unspecified",
		},
		{
			name: "redact unsupported record field (SEVERITY_NUMBER)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-sev-num",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: redactOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), `\d+`, "[REDACTED]"),
			},
			errSubstr: "is not supported as a redact target",
		},
		{
			name: "redact unsupported record field (TRACE_ID)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-trace-id",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: redactOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), `[0-9a-f]+`, "[REDACTED]"),
			},
			errSubstr: "is not supported as a redact target",
		},
		{
			name: "redact unsupported record field (SPAN_ID)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-span-id",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: redactOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), `[0-9a-f]+`, "[REDACTED]"),
			},
			errSubstr: "is not supported as a redact target",
		},
		{
			name: "redact invalid regex",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-bad-re",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: redactOps(logTargetAttr("user"), "[a-z", "[REDACTED]"),
			},
			errSubstr: "invalid redact regex",
		},
		{
			name: "invalid matcher",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id: "p-bad-matcher",
				Matchers: []*policyv1alpha1.LogMatcher{
					{
						Target:    nil,
						Predicate: &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Existence{}},
					},
				},
				Additions: validAdd,
			},
			errSubstr: "matcher[0]:",
		},
		{
			name: "add missing target (nil selector)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-nil-target",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(nil, strVal("val"), false),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "add missing target (empty oneof)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-empty-target-oneof",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(&policyv1alpha1.LogTargetSelector{}, strVal("val"), false),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "remove missing target (nil selector)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-rem-nil-target",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Removals: removeOps(nil),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "remove missing target (empty oneof)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-rem-empty-target-oneof",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Removals: removeOps(&policyv1alpha1.LogAttributeSelector{}),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "add empty log_attribute path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-empty-log-attr",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetAttr(), strVal("val"), false),
			},
			errSubstr: "log attribute path cannot be empty",
		},
		{
			name: "add empty body_field path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-empty-body-field",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetBodyField(), strVal("val"), false),
			},
			errSubstr: "body attribute path cannot be empty",
		},
		{
			name: "add log_attribute missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:        "p-log-attr-no-val",
				Matchers:  []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetAttr("k"), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add log_attribute typed-nil source oneof",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-log-attr-typed-nil-src",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: []*policyv1alpha1.LogAddition{
					{
						Target: logTargetAttr("k"),
						Source: (*policyv1alpha1.LogAddition_Value)(nil),
					},
				},
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "redact typed-nil pattern and mask oneofs",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-redact-typed-nil-oneof",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Redactions: []*policyv1alpha1.LogRedaction{
					{
						Target:  &policyv1alpha1.LogTargetSelector{Target: (*policyv1alpha1.LogTargetSelector_LogAttributePath)(nil)},
						Pattern: (*policyv1alpha1.LogRedaction_Regex)(nil),
						Mask:    (*policyv1alpha1.LogRedaction_Replacement)(nil),
					},
				},
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "add unspecified record_field",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-unspec-record-field",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_UNSPECIFIED), strVal("val"), false),
			},
			errSubstr: "log record field cannot be unspecified",
		},
		{
			name: "add BODY missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-body-nil-val",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), &policyv1alpha1.Value{}, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SEVERITY_TEXT missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-sevtext-nil-val",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SEVERITY_TEXT non-string value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-sevtext-int",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), intVal(9), false),
			},
			errSubstr: "SEVERITY_TEXT requires a string value",
		},
		{
			name: "add SEVERITY_NUMBER missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-sevnum-nil-val",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SEVERITY_NUMBER non-int value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-sevnum-str",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), strVal("INFO"), false),
			},
			errSubstr: "SEVERITY_NUMBER requires an int value",
		},
		{
			name: "add SEVERITY_NUMBER out of range low (0)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-sevnum-zero",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(0), false),
			},
			errSubstr: "must be in range [1, 24], got 0",
		},
		{
			name: "add SEVERITY_NUMBER out of range high (25)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-sevnum-25",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(25), false),
			},
			errSubstr: "must be in range [1, 24], got 25",
		},
		{
			name: "add TRACE_ID immutable field",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-traceid-immutable",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), strVal("0102030405060708090a0b0c0d0e0f10"), false),
			},
			errExpect: ErrImmutableField,
		},
		{
			name: "add SPAN_ID immutable field",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:       "p-spanid-immutable",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), strVal("0102030405060708"), false),
			},
			errExpect: ErrImmutableField,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPolicyFromProto(tt.proto)
			require.Error(t, err)
			if tt.errExpect != nil {
				assert.ErrorIs(t, err, tt.errExpect)
			}
			if tt.errSubstr != "" {
				assert.ErrorContains(t, err, tt.errSubstr)
			}
		})
	}
}

func TestPolicyMetadataAndValidate(t *testing.T) {
	pb := &policyv1alpha1.LogTransformPolicy{
		Id:        "metadata-transform-policy",
		Matchers:  []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
		Additions: addOps(logTargetAttr("tier"), strVal("soc"), false),
	}

	pol, err := NewPolicyFromProto(pb)
	require.NoError(t, err)

	assert.Equal(t, "metadata-transform-policy", pol.PolicyName())
	assert.Equal(t, PolicyType, pol.PolicyType())
	assert.Equal(t, googlepolicy.PolicyClassTransformation, pol.PolicyClass())
	assert.Equal(t, []googlepolicy.Signal{googlepolicy.SignalLogs}, pol.TargetSignals())
	assert.NoError(t, pol.Validate())

	got, ok := pol.Proto().(*policyv1alpha1.LogTransformPolicy)
	require.True(t, ok)
	assert.Same(t, pb, got)

	assert.ErrorIs(t, (&Policy{}).Validate(), ErrNilProto)
	assert.ErrorIs(t, (&Policy{proto: &policyv1alpha1.LogTransformPolicy{Id: "empty"}}).Validate(), ErrMissingAction)
}

func TestPolicy_EmptyMatchersAppliesUnconditionally(t *testing.T) {
	// A transform policy with empty/nil matchers applies to all records unconditionally.
	p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
		Id:        "unconditional-add",
		Additions: addOps(logTargetAttr("injected"), strVal("always"), true),
	})
	require.NoError(t, err)

	lr, sl, rl := newTestLogBundle()
	res := p.TransformLog(logContext(lr, sl, rl))
	assert.Equal(t, googlepolicy.TransformModified, res)
	val, ok := lr.Attributes().Get("injected")
	require.True(t, ok)
	assert.Equal(t, "always", val.Str())
}

func TestDriverLoadPolicyAndProto(t *testing.T) {
	t.Run("LoadPolicy from raw map", func(t *testing.T) {
		raw := map[string]any{
			"type": "log_transform",
			"id":   "loaded-via-driver",
			"matchers": []any{
				map[string]any{
					"target": map[string]any{
						"record_field": "LOG_RECORD_FIELD_BODY",
					},
					"predicate": map[string]any{
						"existence": map[string]any{},
					},
				},
			},
			"additions": []any{
				map[string]any{
					"target": map[string]any{
						"log_attribute_path": map[string]any{
							"segments": []any{"gcp", "routing", "tier"},
						},
					},
					"value": map[string]any{
						"string_value": "soc",
					},
					"upsert_enabled": true,
				},
			},
		}

		p, err := googlepolicy.LoadPolicy(PolicyType, raw)
		require.NoError(t, err)
		assert.Equal(t, "loaded-via-driver", p.PolicyName())
		assert.Equal(t, googlepolicy.PolicyClassTransformation, p.PolicyClass())

		eval, ok := p.(googlepolicy.LogTransformPolicyEvaluator)
		require.True(t, ok)

		lr, sl, rl := newTestLogBundle()
		assert.Equal(t, googlepolicy.TransformModified, eval.TransformLog(logContext(lr, sl, rl)))

		gcpMap, ok := lr.Attributes().Get("gcp")
		require.True(t, ok)
		routingMap, ok := gcpMap.Map().Get("routing")
		require.True(t, ok)
		tierVal, ok := routingMap.Map().Get("tier")
		require.True(t, ok)
		assert.Equal(t, "soc", tierVal.Str())
	})

	t.Run("LoadPolicyFromProto happy path", func(t *testing.T) {
		pb := &policyv1alpha1.LogTransformPolicy{
			Id:       "from-proto",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetAttr("env"), strVal("prod"), false),
		}

		p, err := googlepolicy.LoadPolicyFromProto(pb)
		require.NoError(t, err)
		assert.Equal(t, "from-proto", p.PolicyName())

		pol, ok := p.(*Policy)
		require.True(t, ok)
		assert.Same(t, pb, pol.Proto())
	})

	t.Run("LoadPolicyProto rejects foreign proto", func(t *testing.T) {
		_, err := driver.LoadPolicyProto(&policyv1alpha1.LogFilterPolicy{Id: "wrong-type"})
		require.ErrorIs(t, err, googlepolicy.ErrPolicyProtoMismatch)
	})

	t.Run("LoadPolicy errors", func(t *testing.T) {
		tests := []struct {
			name string
			raw  map[string]any
		}{
			{
				name: "unmarshalable config value",
				raw: map[string]any{
					"type": "log_transform",
					"id":   make(chan int),
				},
			},
			{
				name: "invalid protojson field type",
				raw: map[string]any{
					"type":     "log_transform",
					"id":       "bad-json-type",
					"matchers": "not-an-array",
				},
			},
			{
				name: "policy validation error (missing action)",
				raw: map[string]any{
					"type": "log_transform",
					"id":   "missing-action",
					"matchers": []any{
						map[string]any{
							"target":    map[string]any{"record_field": "LOG_RECORD_FIELD_BODY"},
							"predicate": map[string]any{"existence": map[string]any{}},
						},
					},
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := googlepolicy.LoadPolicy(PolicyType, tt.raw)
				require.Error(t, err)
			})
		}
	})
}

func TestTransformLogAttributes(t *testing.T) {
	t.Run("log_attribute all scalar value types and nested map creation", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		ctx := logContext(lr, sl, rl)

		actions := []struct {
			id  string
			ops []*policyv1alpha1.LogAddition
		}{
			{"add-str", addOps(logTargetAttr("str_key"), strVal("hello"), false)},
			{"add-int", addOps(logTargetAttr("int_key"), intVal(42), false)},
			{"add-double", addOps(logTargetAttr("double_key"), doubleVal(3.14), false)},
			{"add-bool", addOps(logTargetAttr("bool_key"), boolVal(true), false)},
			{"add-bytes", addOps(logTargetAttr("bytes_key"), bytesVal([]byte{0xde, 0xad, 0xbe, 0xef}), false)},
			{"add-nested", addOps(logTargetAttr("gcp", "routing", "tier"), strVal("soc"), false)},
		}

		for _, tc := range actions {
			pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
				Id:        tc.id,
				Matchers:  []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
				Additions: tc.ops,
			})
			require.NoError(t, err)
			assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))
		}

		attrs := lr.Attributes()
		v, ok := attrs.Get("str_key")
		require.True(t, ok)
		assert.Equal(t, "hello", v.Str())

		v, ok = attrs.Get("int_key")
		require.True(t, ok)
		assert.Equal(t, int64(42), v.Int())

		v, ok = attrs.Get("double_key")
		require.True(t, ok)
		assert.Equal(t, 3.14, v.Double())

		v, ok = attrs.Get("bool_key")
		require.True(t, ok)
		assert.True(t, v.Bool())

		v, ok = attrs.Get("bytes_key")
		require.True(t, ok)
		assert.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, v.Bytes().AsRaw())

		gcpVal, ok := attrs.Get("gcp")
		require.True(t, ok)
		routingVal, ok := gcpVal.Map().Get("routing")
		require.True(t, ok)
		tierVal, ok := routingVal.Map().Get("tier")
		require.True(t, ok)
		assert.Equal(t, "soc", tierVal.Str())
	})

	t.Run("log_attribute upsert false vs true", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		ctx := logContext(lr, sl, rl)

		// http.method already exists ("GET") and metadata.env already exists ("prod").
		noUpsertFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "log-attr-no-upsert-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetAttr("http.method"), strVal("POST"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noUpsertFlat.TransformLog(ctx))

		noUpsertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "log-attr-no-upsert-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetAttr("metadata", "env"), strVal("staging"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noUpsertNested.TransformLog(ctx))

		v, _ := lr.Attributes().Get("http.method")
		assert.Equal(t, "GET", v.Str())
		metaVal, _ := lr.Attributes().Get("metadata")
		envVal, _ := metaVal.Map().Get("env")
		assert.Equal(t, "prod", envVal.Str())

		upsertFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "log-attr-upsert-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetAttr("http.method"), strVal("POST"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, upsertFlat.TransformLog(ctx))

		upsertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "log-attr-upsert-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetAttr("metadata", "env"), strVal("staging"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, upsertNested.TransformLog(ctx))

		v, _ = lr.Attributes().Get("http.method")
		assert.Equal(t, "POST", v.Str())
		metaVal, _ = lr.Attributes().Get("metadata")
		envVal, _ = metaVal.Map().Get("env")
		assert.Equal(t, "staging", envVal.Str())
	})

	t.Run("body_field flat and nested with upsert false vs true", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		lr.Body().SetEmptyMap()
		lr.Body().Map().PutStr("event_id", "evt-123")
		ctx := logContext(lr, sl, rl)

		insertFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-field-insert-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetBodyField("status"), strVal("active"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, insertFlat.TransformLog(ctx))

		insertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-field-insert-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetBodyField("user", "tier"), strVal("gold"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, insertNested.TransformLog(ctx))

		statusVal, ok := lr.Body().Map().Get("status")
		require.True(t, ok)
		assert.Equal(t, "active", statusVal.Str())

		userVal, ok := lr.Body().Map().Get("user")
		require.True(t, ok)
		tierVal, ok := userVal.Map().Get("tier")
		require.True(t, ok)
		assert.Equal(t, "gold", tierVal.Str())

		// Upsert: false does not overwrite existing event_id ("evt-123") or user.tier ("gold").
		noOverwriteFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-field-no-overwrite-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetBodyField("event_id"), strVal("evt-999"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noOverwriteFlat.TransformLog(ctx))

		noOverwriteNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-field-no-overwrite-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetBodyField("user", "tier"), strVal("silver"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noOverwriteNested.TransformLog(ctx))

		evtVal, _ := lr.Body().Map().Get("event_id")
		assert.Equal(t, "evt-123", evtVal.Str())

		// Upsert: true overwrites both.
		overwriteFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-field-overwrite-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetBodyField("event_id"), strVal("evt-999"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, overwriteFlat.TransformLog(ctx))

		overwriteNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-field-overwrite-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetBodyField("user", "tier"), strVal("silver"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, overwriteNested.TransformLog(ctx))

		evtVal, _ = lr.Body().Map().Get("event_id")
		assert.Equal(t, "evt-999", evtVal.Str())
		userVal, _ = lr.Body().Map().Get("user")
		tierVal, _ = userVal.Map().Get("tier")
		assert.Equal(t, "silver", tierVal.Str())
	})

	t.Run("body_field on empty body initializes map", func(t *testing.T) {
		lr := plog.NewLogRecord()
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-field-empty",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Additions: addOps(logTargetBodyField("event_id"), strVal("evt-initial"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p.TransformLog(ctx))
		assert.Equal(t, pcommon.ValueTypeMap, lr.Body().Type())
		val, ok := lr.Body().Map().Get("event_id")
		require.True(t, ok)
		assert.Equal(t, "evt-initial", val.Str())
	})

	t.Run("body_field on empty string body initializes map", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetStr("")
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-field-empty-str",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Additions: addOps(logTargetBodyField("event_id"), strVal("evt-from-empty-str"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p.TransformLog(ctx))
		assert.Equal(t, pcommon.ValueTypeMap, lr.Body().Type())
		val, ok := lr.Body().Map().Get("event_id")
		require.True(t, ok)
		assert.Equal(t, "evt-from-empty-str", val.Str())
	})

	t.Run("body_field on non-map body fails open", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetStr("plain text log")
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-field-str-body",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Additions: addOps(logTargetBodyField("event_id"), strVal("evt-123"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, p.TransformLog(ctx))
		assert.Equal(t, "plain text log", lr.Body().Str())
	})
}

func TestTransformLogRecordFields(t *testing.T) {
	t.Run("LOG_RECORD_FIELD_BODY empty, empty-string, non-empty string, and non-string bodies", func(t *testing.T) {
		_, sl, rl := newTestLogBundle()

		polNoUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-no-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), strVal("default body"), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "body-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), strVal("overwritten body"), true),
		})
		require.NoError(t, err)

		// 1. Empty body (ValueTypeEmpty) -> treated as absent, upsert: false sets it.
		lrEmpty := plog.NewLogRecord()
		assert.Equal(t, googlepolicy.TransformModified, polNoUpsert.TransformLog(logContext(lrEmpty, sl, rl)))
		assert.Equal(t, "default body", lrEmpty.Body().Str())

		// 2. Empty-string body ("") -> treated as absent, so upsert: false overwrites it!
		lrEmptyStr := plog.NewLogRecord()
		lrEmptyStr.Body().SetStr("")
		assert.Equal(t, googlepolicy.TransformModified, polNoUpsert.TransformLog(logContext(lrEmptyStr, sl, rl)))
		assert.Equal(t, "default body", lrEmptyStr.Body().Str())

		// 3. Non-empty string body -> upsert: false does NOT overwrite; upsert: true overwrites.
		lrStr := plog.NewLogRecord()
		lrStr.Body().SetStr("existing body")
		assert.Equal(t, googlepolicy.TransformNoMatch, polNoUpsert.TransformLog(logContext(lrStr, sl, rl)))
		assert.Equal(t, "existing body", lrStr.Body().Str())
		assert.Equal(t, googlepolicy.TransformModified, polUpsert.TransformLog(logContext(lrStr, sl, rl)))
		assert.Equal(t, "overwritten body", lrStr.Body().Str())

		// 4. Non-string body (int / bool) -> bodyExists returns true, so upsert: false does NOT overwrite; upsert: true overwrites.
		lrInt := plog.NewLogRecord()
		lrInt.Body().SetInt(999)
		assert.Equal(t, googlepolicy.TransformNoMatch, polNoUpsert.TransformLog(logContext(lrInt, sl, rl)))
		assert.Equal(t, int64(999), lrInt.Body().Int())
		assert.Equal(t, googlepolicy.TransformModified, polUpsert.TransformLog(logContext(lrInt, sl, rl)))
		assert.Equal(t, "overwritten body", lrInt.Body().Str())
	})

	t.Run("LOG_RECORD_FIELD_SEVERITY_TEXT unset vs set with upsert false vs true", func(t *testing.T) {
		_, sl, rl := newTestLogBundle()

		polNoUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "sev-text-no-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), strVal("WARN"), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "sev-text-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), strVal("ERROR"), true),
		})
		require.NoError(t, err)

		lr := plog.NewLogRecord()
		ctx := logContext(lr, sl, rl)

		// Unset ("") -> upsert: false populates it.
		assert.Equal(t, googlepolicy.TransformModified, polNoUpsert.TransformLog(ctx))
		assert.Equal(t, "WARN", lr.SeverityText())

		// Now set ("WARN") -> upsert: false leaves it alone.
		assert.Equal(t, googlepolicy.TransformNoMatch, polNoUpsert.TransformLog(ctx))
		assert.Equal(t, "WARN", lr.SeverityText())

		// Upsert: true overwrites it.
		assert.Equal(t, googlepolicy.TransformModified, polUpsert.TransformLog(ctx))
		assert.Equal(t, "ERROR", lr.SeverityText())
	})

	t.Run("LOG_RECORD_FIELD_SEVERITY_NUMBER unset vs set with upsert false vs true", func(t *testing.T) {
		_, sl, rl := newTestLogBundle()

		polNoUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "sev-num-no-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(int64(plog.SeverityNumberWarn)), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "sev-num-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Additions: addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(int64(plog.SeverityNumberError)), true),
		})
		require.NoError(t, err)

		lr := plog.NewLogRecord()
		ctx := logContext(lr, sl, rl)

		// Unset (SeverityNumberUnspecified) -> upsert: false sets it.
		assert.Equal(t, googlepolicy.TransformModified, polNoUpsert.TransformLog(ctx))
		assert.Equal(t, plog.SeverityNumberWarn, lr.SeverityNumber())

		// Already set -> upsert: false returns TransformNoMatch.
		assert.Equal(t, googlepolicy.TransformNoMatch, polNoUpsert.TransformLog(ctx))
		assert.Equal(t, plog.SeverityNumberWarn, lr.SeverityNumber())

		// Upsert: true overwrites it.
		assert.Equal(t, googlepolicy.TransformModified, polUpsert.TransformLog(ctx))
		assert.Equal(t, plog.SeverityNumberError, lr.SeverityNumber())
	})
}

func TestTransformLogMultipleMatchersAndSequentialPolicies(t *testing.T) {
	t.Run("multiple matchers AND semantics - non-matching leaves record untouched", func(t *testing.T) {
		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id: "multi-matcher-policy",
			Matchers: []*policyv1alpha1.LogMatcher{
				{
					Target: logAttrTarget("http.method"),
					Predicate: &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Equality{
						Equality: strVal("GET"),
					}},
				},
				{
					Target: logAttrTarget("http.status_code"),
					Predicate: &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Equality{
						Equality: intVal(500), // bundle has 200
					}},
				},
			},
			Additions: addOps(logTargetAttr("alert"), boolVal(true), true),
		})
		require.NoError(t, err)

		lr, sl, rl := newTestLogBundle()
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(logContext(lr, sl, rl)))
		_, exists := lr.Attributes().Get("alert")
		assert.False(t, exists)
	})

	t.Run("multiple atomic policies executed in order and return value semantics", func(t *testing.T) {
		p1, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "01-skip-existing",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetAttr("http.method"), strVal("POST"), false),
		})
		require.NoError(t, err)

		p2, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "02-insert-stage-first",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetAttr("stage"), strVal("first"), false),
		})
		require.NoError(t, err)

		p3, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "03-overwrite-stage-second",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Additions: addOps(logTargetAttr("stage"), strVal("second"), true),
		})
		require.NoError(t, err)

		lr, sl, rl := newTestLogBundle()
		ctx := logContext(lr, sl, rl)

		assert.Equal(t, googlepolicy.TransformNoMatch, p1.TransformLog(ctx))
		assert.Equal(t, googlepolicy.TransformModified, p2.TransformLog(ctx))
		assert.Equal(t, googlepolicy.TransformModified, p3.TransformLog(ctx))

		methodVal, _ := lr.Attributes().Get("http.method")
		assert.Equal(t, "GET", methodVal.Str())

		stageVal, ok := lr.Attributes().Get("stage")
		require.True(t, ok)
		assert.Equal(t, "second", stageVal.Str())
	})
}

func TestTransformLogZeroContextGuards(t *testing.T) {
	tests := []struct {
		name string
		ops  []*policyv1alpha1.LogAddition
	}{
		{
			name: "log_attribute",
			ops:  addOps(logTargetAttr("k"), strVal("v"), true),
		},
		{
			name: "body_field",
			ops:  addOps(logTargetBodyField("event_data", "userId"), strVal("v"), true),
		},
		{
			name: "record_field BODY",
			ops:  addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), strVal("v"), true),
		},
		{
			name: "record_field SEVERITY_TEXT",
			ops:  addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), strVal("INFO"), true),
		},
		{
			name: "record_field SEVERITY_NUMBER",
			ops:  addOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(9), true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
				Id:        "zero-ctx-" + tt.name,
				Matchers:  []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
				Additions: tt.ops,
			})
			require.NoError(t, err)
			assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(googlepolicy.LogContext{}))
		})
	}

	// Zero-context guards for remove across supported target types.
	remTargets := []struct {
		name string
		sel  *policyv1alpha1.LogAttributeSelector
	}{
		{name: "flat_attr", sel: logAttrSelector("k")},
		{name: "nested_attr", sel: logAttrSelector("nested", "k")},
		{name: "flat_body", sel: logBodyFieldSelector("event_data")},
		{name: "nested_body", sel: logBodyFieldSelector("event_data", "userId")},
	}
	for _, tc := range remTargets {
		t.Run("zero_ctx_"+tc.name, func(t *testing.T) {
			polRem, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
				Id:     "zero-ctx-rem-" + tc.name,
				Removals: removeOps(tc.sel),
			})
			require.NoError(t, err)
			assert.Equal(t, googlepolicy.TransformNoMatch, polRem.TransformLog(googlepolicy.LogContext{}))
		})
	}
}

func TestTransformLogRemoveAction(t *testing.T) {
	t.Run("remove log_attribute (flat and nested)", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		lr.Attributes().PutEmptyMap("nested").PutStr("key", "val")
		ctx := logContext(lr, sl, rl)

		cases := []struct {
			name string
			sel  *policyv1alpha1.LogAttributeSelector
		}{
			{name: "flat_method", sel: logAttrSelector("http.method")},
			{name: "nested_key", sel: logAttrSelector("nested", "key")},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
					Id:     "rem-" + tc.name,
					Removals: removeOps(tc.sel),
				})
				require.NoError(t, err)
				assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))
				// Removing again returns TransformNoMatch.
				assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctx))
			})
		}

		_, ok := lr.Attributes().Get("http.method")
		assert.False(t, ok)
		nestedMap, ok := lr.Attributes().Get("nested")
		require.True(t, ok)
		_, ok = nestedMap.Map().Get("key")
		assert.False(t, ok)
	})

	t.Run("remove duplicate keys pruned by RemoveIf", func(t *testing.T) {
		rawJSON := []byte(`{
			"resourceLogs": [{
				"scopeLogs": [{
					"logRecords": [{
						"attributes": [
							{"key": "dup_key", "value": {"stringValue": "val1"}},
							{"key": "dup_key", "value": {"stringValue": "val2"}},
							{"key": "keep_key", "value": {"stringValue": "val3"}}
						]
					}]
				}]
			}]
		}`)
		var unmarshaler plog.JSONUnmarshaler
		logs, err := unmarshaler.UnmarshalLogs(rawJSON)
		require.NoError(t, err)
		lr := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0)
		require.Equal(t, 3, lr.Attributes().Len())

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "rem-dup",
			Removals: removeOps(logAttrSelector("dup_key")),
		})
		require.NoError(t, err)

		ctx := googlepolicy.LogContext{Record: lr}
		assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))
		assert.Equal(t, 1, lr.Attributes().Len())
		_, ok := lr.Attributes().Get("dup_key")
		assert.False(t, ok)
		keepVal, ok := lr.Attributes().Get("keep_key")
		require.True(t, ok)
		assert.Equal(t, "val3", keepVal.Str())
	})

	t.Run("non-matching matcher leaves target intact", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		lr.Attributes().PutStr("target_key", "target_val")
		ctx := logContext(lr, sl, rl)

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id: "rem-non-matching",
			Matchers: []*policyv1alpha1.LogMatcher{
				{
					Target: &policyv1alpha1.LogFieldSelector{
						Target: &policyv1alpha1.LogFieldSelector_LogAttributePath{
							LogAttributePath: &policyv1alpha1.AttributePath{Segments: []string{"http.method"}},
						},
					},
					Predicate: &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Equality{
						Equality: &policyv1alpha1.Value{Kind: &policyv1alpha1.Value_StringValue{StringValue: "POST"}},
					}},
				},
			},
			Removals: removeOps(logAttrSelector("target_key")),
		})
		require.NoError(t, err)

		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctx))
		val, ok := lr.Attributes().Get("target_key")
		require.True(t, ok)
		assert.Equal(t, "target_val", val.Str())
	})

	t.Run("remove body_field (flat and nested)", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetEmptyMap().PutStr("event_id", "evt-123")
		lr.Body().Map().PutEmptyMap("event_data").PutStr("user", "alice")
		ctx := googlepolicy.LogContext{Record: lr}

		p1, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "rem-flat-body",
			Removals: removeOps(logBodyFieldSelector("event_id")),
		})
		require.NoError(t, err)

		p2, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "rem-nested-body",
			Removals: removeOps(logBodyFieldSelector("event_data", "user")),
		})
		require.NoError(t, err)

		assert.Equal(t, googlepolicy.TransformModified, p1.TransformLog(ctx))
		assert.Equal(t, googlepolicy.TransformNoMatch, p1.TransformLog(ctx))
		_, ok := lr.Body().Map().Get("event_id")
		assert.False(t, ok)

		assert.Equal(t, googlepolicy.TransformModified, p2.TransformLog(ctx))
		assert.Equal(t, googlepolicy.TransformNoMatch, p2.TransformLog(ctx))
		subMap, ok := lr.Body().Map().Get("event_data")
		require.True(t, ok)
		_, ok = subMap.Map().Get("user")
		assert.False(t, ok)
	})

	t.Run("remove body_field on non-map body fails open", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetStr("plain string body")
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "rem-str-body",
			Removals: removeOps(logBodyFieldSelector("event_id")),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, p.TransformLog(ctx))
		assert.Equal(t, "plain string body", lr.Body().Str())
	})
}

func TestTransformLogRenameAction(t *testing.T) {
	t.Run("stage execution dispatch", func(t *testing.T) {
		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:        "stage-rename",
			Renamings: renameOps(logAttrSelector("old"), logAttrSelector("new"), false),
		})
		require.NoError(t, err)
		lr := plog.NewLogRecord()
		lr.Attributes().PutStr("old", "val")
		ctx := googlepolicy.LogContext{Record: lr}
		assert.Equal(t, googlepolicy.TransformNoMatch, p.TransformLogStage(googlepolicy.TransformStageUnspecified, ctx))
		assert.Equal(t, googlepolicy.TransformNoMatch, p.TransformLogStage(googlepolicy.TransformStageAdd, ctx))
		assert.Equal(t, googlepolicy.TransformModified, p.TransformLogStage(googlepolicy.TransformStageRename, ctx))
	})

	t.Run("rename log_attribute to log_attribute (flat and nested)", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		lr.Attributes().PutStr("old_key", "old_val")
		lr.Attributes().PutEmptyMap("nested").PutInt("count", 10)
		ctx := logContext(lr, sl, rl)

		// 1. Rename flat to flat with default upsert: false.
		p1, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "rename-flat",
			Renamings: renameOps(logAttrSelector("old_key"), logAttrSelector("renamed_key"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p1.TransformLog(ctx))
		assert.Equal(t, googlepolicy.TransformNoMatch, p1.TransformLog(ctx)) // already renamed

		_, ok := lr.Attributes().Get("old_key")
		assert.False(t, ok)
		v, ok := lr.Attributes().Get("renamed_key")
		require.True(t, ok)
		assert.Equal(t, "old_val", v.Str())

		// 2. Rename nested to nested with upsert: false.
		p2, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "rename-nested",
			Renamings: renameOps(logAttrSelector("nested", "count"), logAttrSelector("metrics", "total"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p2.TransformLog(ctx))

		nestedMap, ok := lr.Attributes().Get("nested")
		require.True(t, ok)
		_, ok = nestedMap.Map().Get("count")
		assert.False(t, ok)

		metricsMap, ok := lr.Attributes().Get("metrics")
		require.True(t, ok)
		totalVal, ok := metricsMap.Map().Get("total")
		require.True(t, ok)
		assert.Equal(t, int64(10), totalVal.Int())
	})

	t.Run("upsert semantics", func(t *testing.T) {
		// upsert: false when target exists -> skips and preserves both
		lr := plog.NewLogRecord()
		lr.Attributes().PutStr("src", "from_source")
		lr.Attributes().PutStr("dst", "already_there")
		ctx := googlepolicy.LogContext{Record: lr}

		pNoUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "rename-no-upsert",
			Renamings: renameOps(logAttrSelector("src"), logAttrSelector("dst"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, pNoUpsert.TransformLog(ctx))
		vSrc, _ := lr.Attributes().Get("src")
		assert.Equal(t, "from_source", vSrc.Str())
		vDst, _ := lr.Attributes().Get("dst")
		assert.Equal(t, "already_there", vDst.Str())

		// upsert: true when target exists -> overwrites target and removes source
		pUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "rename-upsert",
			Renamings: renameOps(logAttrSelector("src"), logAttrSelector("dst"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, pUpsert.TransformLog(ctx))
		_, ok := lr.Attributes().Get("src")
		assert.False(t, ok)
		vDst, ok = lr.Attributes().Get("dst")
		require.True(t, ok)
		assert.Equal(t, "from_source", vDst.Str())
	})

	t.Run("cross-boundary log_attribute to body_field", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Attributes().PutStr("user_id", "usr-456")
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "attr-to-body",
			Renamings: renameOps(logAttrSelector("user_id"), logBodyFieldSelector("auth", "uid"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p.TransformLog(ctx))

		_, ok := lr.Attributes().Get("user_id")
		assert.False(t, ok)
		require.Equal(t, pcommon.ValueTypeMap, lr.Body().Type())
		authMap, ok := lr.Body().Map().Get("auth")
		require.True(t, ok)
		uidVal, ok := authMap.Map().Get("uid")
		require.True(t, ok)
		assert.Equal(t, "usr-456", uidVal.Str())
	})

	t.Run("cross-boundary body_field to log_attribute", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetEmptyMap().PutStr("trace_tag", "span-99")
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "body-to-attr",
			Renamings: renameOps(logBodyFieldSelector("trace_tag"), logAttrSelector("app.trace_tag"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p.TransformLog(ctx))

		_, ok := lr.Body().Map().Get("trace_tag")
		assert.False(t, ok)
		appMap, ok := lr.Attributes().Get("app.trace_tag")
		require.True(t, ok)
		assert.Equal(t, "span-99", appMap.Str())
	})

	t.Run("body_field to body_field", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetEmptyMap().PutStr("old_field", "data")
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "body-to-body",
			Renamings: renameOps(logBodyFieldSelector("old_field"), logBodyFieldSelector("new_field"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p.TransformLog(ctx))

		_, ok := lr.Body().Map().Get("old_field")
		assert.False(t, ok)
		newVal, ok := lr.Body().Map().Get("new_field")
		require.True(t, ok)
		assert.Equal(t, "data", newVal.Str())
	})

	t.Run("source identical to target is no-op", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Attributes().PutStr("key", "val")
		lr.Body().SetEmptyMap().PutStr("field", "val2")
		ctx := googlepolicy.LogContext{Record: lr}

		p1, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "same-attr-key",
			Renamings: renameOps(logAttrSelector("key"), logAttrSelector("key"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, p1.TransformLog(ctx))
		v, ok := lr.Attributes().Get("key")
		require.True(t, ok)
		assert.Equal(t, "val", v.Str())

		p2, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:        "same-body-field",
			Renamings: renameOps(logBodyFieldSelector("field"), logBodyFieldSelector("field"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, p2.TransformLog(ctx))
		v2, ok := lr.Body().Map().Get("field")
		require.True(t, ok)
		assert.Equal(t, "val2", v2.Str())

		// Identical segments with differing unknown fields on nested AttributePath
		// must still be treated as a no-op rather than deleting the source.
		srcWithUnknown := logAttrSelector("key")
		srcWithUnknown.GetLogAttributePath().ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
		p3, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:        "same-attr-key-unknown-fields",
			Renamings: renameOps(srcWithUnknown, logAttrSelector("key"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, p3.TransformLog(ctx))
		v3, ok := lr.Attributes().Get("key")
		require.True(t, ok)
		assert.Equal(t, "val", v3.Str())
	})

	t.Run("cross-boundary body_field to nested log_attribute", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetEmptyMap().PutStr("trace_tag", "span-99")
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "body-to-nested-attr",
			Renamings: renameOps(logBodyFieldSelector("trace_tag"), logAttrSelector("meta", "nested", "tag"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p.TransformLog(ctx))

		_, ok := lr.Body().Map().Get("trace_tag")
		assert.False(t, ok)
		metaMap, ok := lr.Attributes().Get("meta")
		require.True(t, ok)
		nestedMap, ok := metaMap.Map().Get("nested")
		require.True(t, ok)
		tagVal, ok := nestedMap.Map().Get("tag")
		require.True(t, ok)
		assert.Equal(t, "span-99", tagVal.Str())
	})

	t.Run("upsert overwrites intermediate non-map scalar", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Attributes().PutStr("old", "val")
		lr.Attributes().PutStr("a", "scalar_blocking")
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "upsert-overwrite-scalar",
			Renamings: renameOps(logAttrSelector("old"), logAttrSelector("a", "b"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p.TransformLog(ctx))

		_, ok := lr.Attributes().Get("old")
		assert.False(t, ok)
		aMap, ok := lr.Attributes().Get("a")
		require.True(t, ok)
		require.Equal(t, pcommon.ValueTypeMap, aMap.Type())
		bVal, ok := aMap.Map().Get("b")
		require.True(t, ok)
		assert.Equal(t, "val", bVal.Str())
	})

	t.Run("non-map body fail-open", func(t *testing.T) {
		// String body when source is body_field
		lrStr := plog.NewLogRecord()
		lrStr.Body().SetStr("plain string")
		ctxStr := googlepolicy.LogContext{Record: lrStr}

		pSrcBody, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "src-body-str",
			Renamings: renameOps(logBodyFieldSelector("f"), logAttrSelector("f"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, pSrcBody.TransformLog(ctxStr))

		// String body when target is body_field
		lrStr2 := plog.NewLogRecord()
		lrStr2.Body().SetStr("plain string")
		lrStr2.Attributes().PutStr("f", "val")
		ctxStr2 := googlepolicy.LogContext{Record: lrStr2}

		pTgtBody, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "tgt-body-str",
			Renamings: renameOps(logAttrSelector("f"), logBodyFieldSelector("f"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, pTgtBody.TransformLog(ctxStr2))
		v, ok := lrStr2.Attributes().Get("f")
		require.True(t, ok)
		assert.Equal(t, "val", v.Str())
	})

	t.Run("rename into empty string body initializes body map", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetStr("")
		lr.Attributes().PutStr("user_id", "usr-123")
		ctx := googlepolicy.LogContext{Record: lr}

		p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "attr-to-empty-str-body",
			Renamings: renameOps(logAttrSelector("user_id"), logBodyFieldSelector("auth", "uid"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, p.TransformLog(ctx))

		_, ok := lr.Attributes().Get("user_id")
		assert.False(t, ok)
		require.Equal(t, pcommon.ValueTypeMap, lr.Body().Type())
		authMap, ok := lr.Body().Map().Get("auth")
		require.True(t, ok)
		uidVal, ok := authMap.Map().Get("uid")
		require.True(t, ok)
		assert.Equal(t, "usr-123", uidVal.Str())
	})

	t.Run("zero-context guards", func(t *testing.T) {
		renSelectors := [][2]*policyv1alpha1.LogAttributeSelector{
			{logAttrSelector("a"), logAttrSelector("b")},
			{logAttrSelector("a"), logBodyFieldSelector("b")},
			{logBodyFieldSelector("a"), logAttrSelector("b")},
			{logBodyFieldSelector("a"), logBodyFieldSelector("b")},
		}
		for _, pair := range renSelectors {
			p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
				Id:     "zero-ctx-ren",
				Renamings: renameOps(pair[0], pair[1], false),
			})
			require.NoError(t, err)
			assert.Equal(t, googlepolicy.TransformNoMatch, p.TransformLog(googlepolicy.LogContext{}))
		}
	})
}

func TestTransformLogRedactAction(t *testing.T) {
	t.Run("log_attribute full replacement when regex is empty", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		lr.Attributes().PutStr("api_token", "secret-12345")
		ctx := logContext(lr, sl, rl)

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-token-all",
			Redactions: redactOps(logTargetAttr("api_token"), "", "[REDACTED]"),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))
		v, ok := lr.Attributes().Get("api_token")
		require.True(t, ok)
		assert.Equal(t, "[REDACTED]", v.Str())

		// Second pass is a no-op because value is already [REDACTED]
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctx))
	})

	t.Run("log_attribute regex pattern with capture groups", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		lr.Attributes().PutStr("user.email", "alice.smith@example.com")
		ctx := logContext(lr, sl, rl)

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "mask-email",
			Redactions: redactOps(logTargetAttr("user.email"), `([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+\.[a-zA-Z]{2,})`, "$1@***"),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))
		v, ok := lr.Attributes().Get("user.email")
		require.True(t, ok)
		assert.Equal(t, "alice.smith@***", v.Str())

		// Second pass with non-matching pattern returns TransformNoMatch
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctx))
	})

	t.Run("nested log_attribute regex redaction", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		authMap := lr.Attributes().PutEmptyMap("auth")
		authMap.PutStr("bearer", "Bearer secret-xyz-token")
		ctx := logContext(lr, sl, rl)

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-nested-bearer",
			Redactions: redactOps(logTargetAttr("auth", "bearer"), `Bearer .*`, "Bearer [REDACTED]"),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))

		authVal, ok := lr.Attributes().Get("auth")
		require.True(t, ok)
		bearerVal, ok := authVal.Map().Get("bearer")
		require.True(t, ok)
		assert.Equal(t, "Bearer [REDACTED]", bearerVal.Str())
	})

	t.Run("body_field regex redaction in nested map body", func(t *testing.T) {
		lr := plog.NewLogRecord()
		bodyMap := lr.Body().SetEmptyMap()
		userMap := bodyMap.PutEmptyMap("user")
		userMap.PutStr("ssn", "123-45-6789")
		ctx := googlepolicy.LogContext{Record: lr}

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-ssn-body",
			Redactions: redactOps(logTargetBodyField("user", "ssn"), `\d{3}-\d{2}-\d{4}`, "XXX-XX-XXXX"),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))

		bMap := lr.Body().Map()
		uVal, ok := bMap.Get("user")
		require.True(t, ok)
		ssnVal, ok := uVal.Map().Get("ssn")
		require.True(t, ok)
		assert.Equal(t, "XXX-XX-XXXX", ssnVal.Str())
	})

	t.Run("body_field fail-open on non-map body or missing field", func(t *testing.T) {
		lrStr := plog.NewLogRecord()
		lrStr.Body().SetStr("plain string body")
		ctxStr := googlepolicy.LogContext{Record: lrStr}

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-body-field-failopen",
			Redactions: redactOps(logTargetBodyField("user", "ssn"), `.*`, "[REDACTED]"),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctxStr))
		assert.Equal(t, "plain string body", lrStr.Body().Str())

		// Missing body field in map body
		lrMap := plog.NewLogRecord()
		lrMap.Body().SetEmptyMap().PutStr("other", "val")
		ctxMap := googlepolicy.LogContext{Record: lrMap}
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctxMap))
	})

	t.Run("record_field string body redaction", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetStr("Access granted with password=supersecret for user alice")
		ctx := googlepolicy.LogContext{Record: lr}

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-record-body",
			Redactions: redactOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), `password=\S+`, "password=[REDACTED]"),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))
		assert.Equal(t, "Access granted with password=[REDACTED] for user alice", lr.Body().Str())

		// If body is not string (e.g. map body), record_field BODY redaction fails open
		lrMap := plog.NewLogRecord()
		lrMap.Body().SetEmptyMap().PutStr("key", "val")
		ctxMap := googlepolicy.LogContext{Record: lrMap}
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctxMap))

		// If body is empty string, record_field BODY redaction fails open
		lrEmpty := plog.NewLogRecord()
		lrEmpty.Body().SetStr("")
		ctxEmpty := googlepolicy.LogContext{Record: lrEmpty}
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctxEmpty))
	})

	t.Run("record_field severity_text redaction", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.SetSeverityText("DEBUG_VERBOSE")
		ctx := googlepolicy.LogContext{Record: lr}

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-sev-text",
			Redactions: redactOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), `_VERBOSE`, ""),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))
		assert.Equal(t, "DEBUG", lr.SeverityText())

		// Unset severity_text fails open / no-op
		lrUnset := plog.NewLogRecord()
		ctxUnset := googlepolicy.LogContext{Record: lrUnset}
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctxUnset))
	})

	t.Run("record_field full string replacement on unset or empty values fails open", func(t *testing.T) {
		// Policy with regex: "" (full string replacement) targeting SEVERITY_TEXT
		polSev, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-full-sev-text",
			Redactions: redactOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), "", "[REDACTED]"),
		})
		require.NoError(t, err)

		lrUnsetSev := plog.NewLogRecord()
		ctxUnsetSev := googlepolicy.LogContext{Record: lrUnsetSev}
		assert.Equal(t, googlepolicy.TransformNoMatch, polSev.TransformLog(ctxUnsetSev))
		assert.Equal(t, "", lrUnsetSev.SeverityText())

		// Policy with regex: "" (full string replacement) targeting BODY
		polBody, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-full-body",
			Redactions: redactOps(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), "", "[REDACTED]"),
		})
		require.NoError(t, err)

		lrEmptyBody := plog.NewLogRecord()
		lrEmptyBody.Body().SetStr("")
		ctxEmptyBody := googlepolicy.LogContext{Record: lrEmptyBody}
		assert.Equal(t, googlepolicy.TransformNoMatch, polBody.TransformLog(ctxEmptyBody))
		assert.Equal(t, "", lrEmptyBody.Body().Str())
	})

	t.Run("non-string target attributes fail-open", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		lr.Attributes().PutInt("status_code", 200)
		ctx := logContext(lr, sl, rl)

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-int-attr",
			Redactions: redactOps(logTargetAttr("status_code"), `\d+`, "[REDACTED]"),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctx))
		v, ok := lr.Attributes().Get("status_code")
		require.True(t, ok)
		assert.Equal(t, int64(200), v.Int())
	})

	t.Run("non-string target in body_field_path fails open", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetEmptyMap().PutInt("status_code", 404)
		ctx := googlepolicy.LogContext{Record: lr}

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-int-body-field",
			Redactions: redactOps(logTargetBodyField("status_code"), `\d+`, "[REDACTED]"),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctx))
		v, ok := lr.Body().Map().Get("status_code")
		require.True(t, ok)
		assert.Equal(t, int64(404), v.Int())
	})

	t.Run("full replacement on body_field_path", func(t *testing.T) {
		lr := plog.NewLogRecord()
		lr.Body().SetEmptyMap().PutStr("token", "secret123")
		ctx := googlepolicy.LogContext{Record: lr}

		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:     "redact-full-body-field",
			Redactions: redactOps(logTargetBodyField("token"), "", "[REDACTED]"),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(ctx))
		v, ok := lr.Body().Map().Get("token")
		require.True(t, ok)
		assert.Equal(t, "[REDACTED]", v.Str())

		// Second pass is a no-op
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(ctx))
	})

	t.Run("zero-context guards", func(t *testing.T) {
		targets := []*policyv1alpha1.LogTargetSelector{
			logTargetAttr("a"),
			logTargetBodyField("b"),
			logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY),
			logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT),
		}
		for _, tgt := range targets {
			p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
				Id:     "zero-ctx-redact",
				Redactions: redactOps(tgt, `\d+`, "[REDACTED]"),
			})
			require.NoError(t, err)
			assert.Equal(t, googlepolicy.TransformNoMatch, p.TransformLog(googlepolicy.LogContext{}))
		}
	})
}

func TestTransformLogPartialFailure(t *testing.T) {
	p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
		Id:       "partial-failure-add",
		Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
		Additions: []*policyv1alpha1.LogAddition{
			{
				Target:        logTargetAttr("existing_key"),
				Source:        &policyv1alpha1.LogAddition_Value{Value: strVal("new_value")},
				UpsertEnabled: proto.Bool(false), // This will fail because "existing_key" exists
			},
			{
				Target:        logTargetAttr("new_key"),
				Source:        &policyv1alpha1.LogAddition_Value{Value: strVal("success_value")},
				UpsertEnabled: proto.Bool(false), // This will succeed
			},
		},
	})
	require.NoError(t, err)

	lr, sl, rl := newTestLogBundle()
	lr.Attributes().PutStr("existing_key", "old_value")

	res := p.TransformLog(logContext(lr, sl, rl))
	assert.Equal(t, googlepolicy.TransformModified, res)

	val1, ok := lr.Attributes().Get("existing_key")
	require.True(t, ok)
	assert.Equal(t, "old_value", val1.Str()) // Unchanged

	val2, ok := lr.Attributes().Get("new_key")
	require.True(t, ok)
	assert.Equal(t, "success_value", val2.Str()) // Added
}


func TestTransformLogCombinedMultiStageSinglePolicy(t *testing.T) {
	// Single combined policy matching program == "haproxy", renaming program -> service.name
	// and legacy_user -> user.name in Stage 1, adding fallback user.name = "guest" (upsert: false)
	// and tier = "edge" in Stage 2, removing scratch_token in Stage 3, and redacting user.name in Stage 4.
	pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
		Id: "haproxy-normalize-and-scrub",
		Matchers: []*policyv1alpha1.LogMatcher{
			{
				Target: logAttrTarget("program"),
				Predicate: &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Equality{
					Equality: strVal("haproxy"),
				}},
			},
			{
				Target: scopeAttrTarget("scope.tag"),
				Predicate: &policyv1alpha1.Predicate{Kind: &policyv1alpha1.Predicate_Equality{
					Equality: strVal("edge-proxy"),
				}},
			},
		},
		Renamings: []*policyv1alpha1.LogRenaming{
			{
				Source:        logAttrSelector("program"),
				Target:        logAttrSelector("service.name"),
				UpsertEnabled: proto.Bool(true),
			},
			{
				Source:        logAttrSelector("legacy_user"),
				Target:        logAttrSelector("user.name"),
				UpsertEnabled: proto.Bool(false),
			},
		},
		Additions: []*policyv1alpha1.LogAddition{
			{
				Target:        logTargetAttr("user.name"),
				Source:        &policyv1alpha1.LogAddition_Value{Value: strVal("guest@example.com")},
				UpsertEnabled: proto.Bool(false),
			},
			{
				Target:        logTargetAttr("tier"),
				Source:        &policyv1alpha1.LogAddition_Value{Value: strVal("edge")},
				UpsertEnabled: proto.Bool(true),
			},
		},
		Removals: []*policyv1alpha1.LogRemoval{
			{Target: logAttrSelector("scratch_token")},
		},
		Redactions: []*policyv1alpha1.LogRedaction{
			{
				Target:  logTargetAttr("user.name"),
				Pattern: &policyv1alpha1.LogRedaction_Regex{Regex: `@.+`},
				Mask:    &policyv1alpha1.LogRedaction_Replacement{Replacement: "@[REDACTED]"},
			},
		},
	})
	require.NoError(t, err)

	scope := pcommon.NewInstrumentationScope()
	scope.Attributes().PutStr("scope.tag", "edge-proxy")

	// Record 1: Matches program == "haproxy" and scope.tag == "edge-proxy", with legacy_user = "alice@corp.example".
	// Even though Stage 1 renames "program" away, pre-transform MatchesLog ensures
	// Stages 2, 3, and 4 all execute; Stage 2 fallback on user.name sees Stage 1's rename
	// and does not overwrite it; Stage 4 redacts the renamed user.name.
	lr1 := plog.NewLogRecord()
	lr1.Attributes().PutStr("program", "haproxy")
	lr1.Attributes().PutStr("legacy_user", "alice@corp.example")
	lr1.Attributes().PutStr("scratch_token", "tmp-secret")
	assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(googlepolicy.LogContext{Record: lr1, Scope: scope}))

	_, hasProgram := lr1.Attributes().Get("program")
	assert.False(t, hasProgram)
	svc, ok := lr1.Attributes().Get("service.name")
	require.True(t, ok)
	assert.Equal(t, "haproxy", svc.Str())
	user, ok := lr1.Attributes().Get("user.name")
	require.True(t, ok)
	assert.Equal(t, "alice@[REDACTED]", user.Str())
	tier, ok := lr1.Attributes().Get("tier")
	require.True(t, ok)
	assert.Equal(t, "edge", tier.Str())
	_, hasScratch := lr1.Attributes().Get("scratch_token")
	assert.False(t, hasScratch)

	// Record 2: Matches program == "haproxy" without legacy_user -> Stage 1 rename on legacy_user
	// fails open, Stage 2 fallback adds user.name = "guest@example.com", Stage 4 redacts it to "guest@[REDACTED]".
	lr2 := plog.NewLogRecord()
	lr2.Attributes().PutStr("program", "haproxy")
	assert.Equal(t, googlepolicy.TransformModified, pol.TransformLog(googlepolicy.LogContext{Record: lr2, Scope: scope}))
	user2, ok := lr2.Attributes().Get("user.name")
	require.True(t, ok)
	assert.Equal(t, "guest@[REDACTED]", user2.Str())

	// Record 3: Non-matching program -> untouched.
	lr3 := plog.NewLogRecord()
	lr3.Attributes().PutStr("program", "nginx")
	lr3.Attributes().PutStr("scratch_token", "keep-me")
	assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(googlepolicy.LogContext{Record: lr3, Scope: scope}))
	_, ok = lr3.Attributes().Get("scratch_token")
	assert.True(t, ok)
}

func TestPolicyValidationAtomicMultiStageAndUnknownRedactionWireField(t *testing.T) {
	t.Run("atomic rejection when one redaction in combined policy has invalid regex", func(t *testing.T) {
		_, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:        "atomic-reject-combined",
			Renamings: renameOps(logAttrSelector("a"), logAttrSelector("b"), false),
			Additions: addOps(logTargetAttr("tier"), strVal("soc"), true),
			Removals:  removeOps(logAttrSelector("tmp")),
			Redactions: []*policyv1alpha1.LogRedaction{
				{
					Target:  logTargetAttr("b"),
					Pattern: &policyv1alpha1.LogRedaction_Regex{Regex: `\d+`},
					Mask:    &policyv1alpha1.LogRedaction_Replacement{Replacement: "X"},
				},
				{
					Target:  logTargetAttr("b"),
					Pattern: &policyv1alpha1.LogRedaction_Regex{Regex: "[unclosed"},
					Mask:    &policyv1alpha1.LogRedaction_Replacement{Replacement: "X"},
				},
			},
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "redactions[1]:")
		assert.ErrorContains(t, err, "invalid redact regex")
	})

	t.Run("rejects unknown wire field on LogRedaction (xDS version skew protection)", func(t *testing.T) {
		red := &policyv1alpha1.LogRedaction{
			Target: logTargetAttr("secret"),
			Mask:   &policyv1alpha1.LogRedaction_Replacement{Replacement: "[REDACTED]"},
		}
		raw, err := proto.Marshal(red)
		require.NoError(t, err)
		// Append an unknown wire tag 4 (e.g. a future pattern classifier variant).
		raw = protowire.AppendTag(raw, 4, protowire.BytesType)
		raw = protowire.AppendString(raw, "future-builtin-classifier")

		skewedRed := &policyv1alpha1.LogRedaction{}
		require.NoError(t, proto.Unmarshal(raw, skewedRed))
		require.Nil(t, skewedRed.GetPattern())

		_, err = NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:         "skewed-redaction-policy",
			Redactions: []*policyv1alpha1.LogRedaction{skewedRed},
		})
		require.ErrorIs(t, err, ErrUnsupportedAction)
		assert.ErrorContains(t, err, "redactions[0]:")
	})

	t.Run("rejects unknown stage wire field on LogTransformPolicy even when known stage is present", func(t *testing.T) {
		base := &policyv1alpha1.LogTransformPolicy{
			Id:       "skewed-combined-policy",
			Removals: removeOps(logAttrSelector("raw_payload")),
		}
		raw, err := proto.Marshal(base)
		require.NoError(t, err)
		// Append an unknown top-level stage tag 7 (e.g. a future parsing stage).
		raw = protowire.AppendTag(raw, 7, protowire.BytesType)
		raw = protowire.AppendString(raw, "future-stage-op")

		skewed := &policyv1alpha1.LogTransformPolicy{}
		require.NoError(t, proto.Unmarshal(raw, skewed))
		require.Len(t, skewed.GetRemovals(), 1)

		_, err = NewPolicyFromProto(skewed)
		require.ErrorIs(t, err, ErrUnsupportedAction)
	})

	t.Run("rejects unknown stage wire field on LogTransformPolicy when only future stage is set", func(t *testing.T) {
		base := &policyv1alpha1.LogTransformPolicy{Id: "skewed-only-future-stage"}
		raw, err := proto.Marshal(base)
		require.NoError(t, err)
		raw = protowire.AppendTag(raw, 7, protowire.BytesType)
		raw = protowire.AppendString(raw, "future-stage-op")

		skewed := &policyv1alpha1.LogTransformPolicy{}
		require.NoError(t, proto.Unmarshal(raw, skewed))

		_, err = NewPolicyFromProto(skewed)
		require.ErrorIs(t, err, ErrUnsupportedAction)
	})

	t.Run("rejects unknown source wire field on LogAddition and unknown target selector wire field", func(t *testing.T) {
		add := &policyv1alpha1.LogAddition{Target: logTargetAttr("tier")}
		rawAdd, err := proto.Marshal(add)
		require.NoError(t, err)
		// Append an unknown source oneof tag 4 (e.g. copy_from_resource).
		rawAdd = protowire.AppendTag(rawAdd, 4, protowire.BytesType)
		rawAdd = protowire.AppendString(rawAdd, "service.name")

		skewedAdd := &policyv1alpha1.LogAddition{}
		require.NoError(t, proto.Unmarshal(rawAdd, skewedAdd))
		require.Nil(t, skewedAdd.GetSource())

		_, err = NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:        "skewed-addition-source",
			Additions: []*policyv1alpha1.LogAddition{skewedAdd},
		})
		require.ErrorIs(t, err, ErrUnsupportedAction)
		assert.ErrorContains(t, err, "additions[0]:")

		// Unknown target selector oneof tag 4 on LogAttributeSelector.
		var rawSel []byte
		rawSel = protowire.AppendTag(rawSel, 4, protowire.BytesType)
		rawSel = protowire.AppendString(rawSel, "future-selector")
		skewedSel := &policyv1alpha1.LogAttributeSelector{}
		require.NoError(t, proto.Unmarshal(rawSel, skewedSel))

		_, err = NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:       "skewed-removal-selector",
			Removals: removeOps(skewedSel),
		})
		require.ErrorIs(t, err, ErrUnsupportedAction)
		assert.ErrorContains(t, err, "removals[0]:")
	})

	t.Run("driver sets PolicyClassTransformation on FailedPolicy", func(t *testing.T) {
		ps, err := googlepolicy.MakePolicySetFromProtos("test-ps", "rev1", []proto.Message{
			&policyv1alpha1.LogTransformPolicy{Id: "invalid-empty-transform"},
		})
		require.Error(t, err)
		require.NotNil(t, ps)
		require.Len(t, ps.FailedPolicies, 1)
		assert.Equal(t, "invalid-empty-transform", ps.FailedPolicies[0].ID)
		assert.Equal(t, googlepolicy.PolicyClassTransformation, ps.FailedPolicies[0].Class)
	})
}
