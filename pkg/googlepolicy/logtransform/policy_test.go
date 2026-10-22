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
		Predicate: &policyv1alpha1.LogMatcher_Existence{},
	}
}

func alwaysTrueMatcher() *policyv1alpha1.LogMatcher {
	return &policyv1alpha1.LogMatcher{
		Target:    logAttrTarget("__never_present__"),
		Predicate: &policyv1alpha1.LogMatcher_Existence{},
		Negated:    true,
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

func addAction(target *policyv1alpha1.LogTargetSelector, val *policyv1alpha1.Value, upsert bool) *policyv1alpha1.LogTransformPolicy_Addition {
	return &policyv1alpha1.LogTransformPolicy_Addition{
		Addition: &policyv1alpha1.LogAddAction{
			Target:        target,
			Value:         val,
			UpsertEnabled: proto.Bool(upsert),
		},
	}
}

func TestPolicyValidationErrors(t *testing.T) {
	validMatcher := bodyExistsMatcher()
	validAdd := addAction(logTargetAttr("tier"), strVal("soc"), false)

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
				Id:      "",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  validAdd,
			},
			errExpect: ErrMissingID,
		},
		{
			name: "missing action (nil Action)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-no-action",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  nil,
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "missing action (nil Addition inside LogTransformPolicy_Addition)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-nil-add-msg",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  &policyv1alpha1.LogTransformPolicy_Addition{Addition: nil},
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "unsupported action removal",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-remove",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action: &policyv1alpha1.LogTransformPolicy_Removal{
					Removal: &policyv1alpha1.LogRemoveAction{
						Target: logAttrSelector("tier"),
					},
				},
			},
			errExpect: ErrUnsupportedAction,
		},
		{
			name: "unsupported action renaming",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-rename",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action: &policyv1alpha1.LogTransformPolicy_Renaming{
					Renaming: &policyv1alpha1.LogRenameAction{
						Source: logAttrSelector("tier"),
						Target: logAttrSelector("new_tier"),
					},
				},
			},
			errExpect: ErrUnsupportedAction,
		},
		{
			name: "unsupported action redaction",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-redact",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action: &policyv1alpha1.LogTransformPolicy_Redaction{
					Redaction: &policyv1alpha1.LogRedactAction{
						Target:      logTargetAttr("tier"),
						Replacement: proto.String("[REDACTED]"),
					},
				},
			},
			errExpect: ErrUnsupportedAction,
		},
		{
			name: "invalid matcher",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id: "p-bad-matcher",
				Matchers: []*policyv1alpha1.LogMatcher{
					{
						Target:    nil,
						Predicate: &policyv1alpha1.LogMatcher_Existence{},
					},
				},
				Action: validAdd,
			},
			errSubstr: "matcher[0]:",
		},
		{
			name: "add missing target (nil selector)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-nil-target",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(nil, strVal("val"), false),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "add missing target (empty oneof)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-empty-target-oneof",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(&policyv1alpha1.LogTargetSelector{}, strVal("val"), false),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "add empty log_attribute path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-empty-log-attr",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetAttr(), strVal("val"), false),
			},
			errSubstr: "log attribute path cannot be empty",
		},
		{
			name: "add empty body_field path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-empty-body-field",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetBodyField(), strVal("val"), false),
			},
			errSubstr: "body attribute path cannot be empty",
		},
		{
			name: "add log_attribute missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-log-attr-no-val",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetAttr("k"), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add unspecified record_field",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-unspec-record-field",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_UNSPECIFIED), strVal("val"), false),
			},
			errSubstr: "log record field cannot be unspecified",
		},
		{
			name: "add BODY missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-body-nil-val",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), &policyv1alpha1.Value{}, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SEVERITY_TEXT missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevtext-nil-val",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SEVERITY_TEXT non-string value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevtext-int",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), intVal(9), false),
			},
			errSubstr: "SEVERITY_TEXT requires a string value",
		},
		{
			name: "add SEVERITY_NUMBER missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevnum-nil-val",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SEVERITY_NUMBER non-int value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevnum-str",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), strVal("INFO"), false),
			},
			errSubstr: "SEVERITY_NUMBER requires an int value",
		},
		{
			name: "add SEVERITY_NUMBER out of range low (0)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevnum-zero",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(0), false),
			},
			errSubstr: "must be in range [1, 24], got 0",
		},
		{
			name: "add SEVERITY_NUMBER out of range high (25)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevnum-25",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(25), false),
			},
			errSubstr: "must be in range [1, 24], got 25",
		},
		{
			name: "add TRACE_ID immutable field",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-traceid-immutable",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), strVal("0102030405060708090a0b0c0d0e0f10"), false),
			},
			errExpect: ErrImmutableField,
		},
		{
			name: "add SPAN_ID immutable field",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-spanid-immutable",
				Matchers: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), strVal("0102030405060708"), false),
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
		Id:      "metadata-transform-policy",
		Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
		Action:  addAction(logTargetAttr("tier"), strVal("soc"), false),
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
}

func TestPolicy_EmptyMatchersAppliesUnconditionally(t *testing.T) {
	// A transform policy with empty/nil matchers applies to all records unconditionally.
	p, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
		Id: "unconditional-add",
		Action: &policyv1alpha1.LogTransformPolicy_Addition{
			Addition: &policyv1alpha1.LogAddAction{
				Target:        logTargetAttr("injected"),
				Value:         strVal("always"),
				UpsertEnabled: proto.Bool(true),
			},
		},
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
					"existence": map[string]any{},
				},
			},
			"addition": map[string]any{
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
			Id:      "from-proto",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetAttr("env"), strVal("prod"), false),
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
					"type":    "log_transform",
					"id":      "bad-json-type",
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
							"target": map[string]any{"record_field": "LOG_RECORD_FIELD_BODY"},
							"existence": map[string]any{},
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
			id     string
			action *policyv1alpha1.LogTransformPolicy_Addition
		}{
			{"add-str", addAction(logTargetAttr("str_key"), strVal("hello"), false)},
			{"add-int", addAction(logTargetAttr("int_key"), intVal(42), false)},
			{"add-double", addAction(logTargetAttr("double_key"), doubleVal(3.14), false)},
			{"add-bool", addAction(logTargetAttr("bool_key"), boolVal(true), false)},
			{"add-bytes", addAction(logTargetAttr("bytes_key"), bytesVal([]byte{0xde, 0xad, 0xbe, 0xef}), false)},
			{"add-nested", addAction(logTargetAttr("gcp", "routing", "tier"), strVal("soc"), false)},
		}

		for _, tc := range actions {
			pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
				Id:      tc.id,
				Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
				Action:  tc.action,
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
			Id:      "log-attr-no-upsert-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetAttr("http.method"), strVal("POST"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noUpsertFlat.TransformLog(ctx))

		noUpsertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "log-attr-no-upsert-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetAttr("metadata", "env"), strVal("staging"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noUpsertNested.TransformLog(ctx))

		v, _ := lr.Attributes().Get("http.method")
		assert.Equal(t, "GET", v.Str())
		metaVal, _ := lr.Attributes().Get("metadata")
		envVal, _ := metaVal.Map().Get("env")
		assert.Equal(t, "prod", envVal.Str())

		upsertFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "log-attr-upsert-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetAttr("http.method"), strVal("POST"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, upsertFlat.TransformLog(ctx))

		upsertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "log-attr-upsert-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetAttr("metadata", "env"), strVal("staging"), true),
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
			Id:      "body-field-insert-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetBodyField("status"), strVal("active"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, insertFlat.TransformLog(ctx))

		insertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "body-field-insert-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetBodyField("user", "tier"), strVal("gold"), false),
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
			Id:      "body-field-no-overwrite-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetBodyField("event_id"), strVal("evt-999"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noOverwriteFlat.TransformLog(ctx))

		noOverwriteNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "body-field-no-overwrite-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetBodyField("user", "tier"), strVal("silver"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noOverwriteNested.TransformLog(ctx))

		evtVal, _ := lr.Body().Map().Get("event_id")
		assert.Equal(t, "evt-123", evtVal.Str())

		// Upsert: true overwrites both.
		overwriteFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "body-field-overwrite-flat",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetBodyField("event_id"), strVal("evt-999"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, overwriteFlat.TransformLog(ctx))

		overwriteNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "body-field-overwrite-nested",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetBodyField("user", "tier"), strVal("silver"), true),
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
			Id:      "body-field-empty",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(logTargetBodyField("event_id"), strVal("evt-initial"), true),
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
			Id:      "body-field-empty-str",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(logTargetBodyField("event_id"), strVal("evt-from-empty-str"), true),
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
			Id:      "body-field-str-body",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(logTargetBodyField("event_id"), strVal("evt-123"), true),
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
			Id:      "body-no-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), strVal("default body"), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "body-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), strVal("overwritten body"), true),
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
			Id:      "sev-text-no-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), strVal("WARN"), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "sev-text-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), strVal("ERROR"), true),
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
			Id:      "sev-num-no-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(int64(plog.SeverityNumberWarn)), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "sev-num-upsert",
			Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(int64(plog.SeverityNumberError)), true),
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
					Predicate: &policyv1alpha1.LogMatcher_Equality{
						Equality: strVal("GET"),
					},
				},
				{
					Target: logAttrTarget("http.status_code"),
					Predicate: &policyv1alpha1.LogMatcher_Equality{
						Equality: intVal(500), // bundle has 200
					},
				},
			},
			Action: addAction(logTargetAttr("alert"), boolVal(true), true),
		})
		require.NoError(t, err)

		lr, sl, rl := newTestLogBundle()
		assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(logContext(lr, sl, rl)))
		_, exists := lr.Attributes().Get("alert")
		assert.False(t, exists)
	})

	t.Run("multiple atomic policies executed in order and return value semantics", func(t *testing.T) {
		p1, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "01-skip-existing",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetAttr("http.method"), strVal("POST"), false),
		})
		require.NoError(t, err)

		p2, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "02-insert-stage-first",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetAttr("stage"), strVal("first"), false),
		})
		require.NoError(t, err)

		p3, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "03-overwrite-stage-second",
			Matchers: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logTargetAttr("stage"), strVal("second"), true),
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
		name   string
		action *policyv1alpha1.LogTransformPolicy_Addition
	}{
		{
			name:   "log_attribute",
			action: addAction(logTargetAttr("k"), strVal("v"), true),
		},
		{
			name:   "body_field",
			action: addAction(logTargetBodyField("event_data", "userId"), strVal("v"), true),
		},
		{
			name:   "record_field BODY",
			action: addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), strVal("v"), true),
		},
		{
			name:   "record_field SEVERITY_TEXT",
			action: addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), strVal("INFO"), true),
		},
		{
			name:   "record_field SEVERITY_NUMBER",
			action: addAction(logTargetRecordField(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(9), true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
				Id:      "zero-ctx-" + tt.name,
				Matchers: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
				Action:  tt.action,
			})
			require.NoError(t, err)
			assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(googlepolicy.LogContext{}))
		})
	}
}
