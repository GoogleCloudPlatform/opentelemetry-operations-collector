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
		Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
			LogAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func resourceAttrTarget(path ...string) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ResourceAttribute{
			ResourceAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func scopeAttrTarget(path ...string) *policyv1alpha1.LogFieldSelector {
	return &policyv1alpha1.LogFieldSelector{
		Target: &policyv1alpha1.LogFieldSelector_ScopeAttribute{
			ScopeAttribute: &policyv1alpha1.AttributePath{Path: path},
		},
	}
}

func strVal(s string) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: s}}
}

func intVal(n int64) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_IntValue{IntValue: n}}
}

func doubleVal(d float64) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_DoubleValue{DoubleValue: d}}
}

func boolVal(b bool) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BoolValue{BoolValue: b}}
}

func bytesVal(b []byte) *policyv1alpha1.Value {
	return &policyv1alpha1.Value{Value: &policyv1alpha1.Value_BytesValue{BytesValue: b}}
}

func bodyExistsMatcher() *policyv1alpha1.LogMatcher {
	return &policyv1alpha1.LogMatcher{
		Target:    recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY),
		Predicate: &policyv1alpha1.LogMatcher_Exists{},
	}
}

func alwaysTrueMatcher() *policyv1alpha1.LogMatcher {
	return &policyv1alpha1.LogMatcher{
		Target:    logAttrTarget("__never_present__"),
		Predicate: &policyv1alpha1.LogMatcher_Exists{},
		Negate:    true,
	}
}

func addAction(target *policyv1alpha1.LogFieldSelector, val *policyv1alpha1.Value, upsert bool) *policyv1alpha1.LogTransformPolicy_Add {
	return &policyv1alpha1.LogTransformPolicy_Add{
		Add: &policyv1alpha1.LogAddAction{
			Target: target,
			Value:  val,
			Upsert: upsert,
		},
	}
}

func TestPolicyValidationErrors(t *testing.T) {
	validMatcher := bodyExistsMatcher()
	validAdd := addAction(logAttrTarget("tier"), strVal("soc"), false)

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
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  validAdd,
			},
			errExpect: ErrMissingID,
		},
		{
			name: "missing matchers",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-no-matchers",
				Matches: nil,
				Action:  validAdd,
			},
			errExpect: ErrMissingMatchers,
		},
		{
			name: "missing action (nil Action)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-no-action",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  nil,
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "missing action (nil Add inside LogTransformPolicy_Add)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-nil-add-msg",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  &policyv1alpha1.LogTransformPolicy_Add{Add: nil},
			},
			errExpect: ErrMissingAction,
		},
		{
			name: "unsupported action remove",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-remove",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action: &policyv1alpha1.LogTransformPolicy_Remove{
					Remove: &policyv1alpha1.LogRemoveAction{
						Target: logAttrTarget("tier"),
					},
				},
			},
			errExpect: ErrUnsupportedAction,
		},
		{
			name: "unsupported action rename",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-rename",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action: &policyv1alpha1.LogTransformPolicy_Rename{
					Rename: &policyv1alpha1.LogRenameAction{
						From: logAttrTarget("tier"),
						To:   logAttrTarget("new_tier"),
					},
				},
			},
			errExpect: ErrUnsupportedAction,
		},
		{
			name: "unsupported action redact",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-redact",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action: &policyv1alpha1.LogTransformPolicy_Redact{
					Redact: &policyv1alpha1.LogRedactAction{
						Target:      logAttrTarget("tier"),
						Replacement: "[REDACTED]",
					},
				},
			},
			errExpect: ErrUnsupportedAction,
		},
		{
			name: "invalid matcher",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id: "p-bad-matcher",
				Matches: []*policyv1alpha1.LogMatcher{
					{
						Target: recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY),
						Predicate: &policyv1alpha1.LogMatcher_Regex{
							Regex: "[unclosed",
						},
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
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(nil, strVal("val"), false),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "add missing target (empty oneof)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-empty-target-oneof",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(&policyv1alpha1.LogFieldSelector{}, strVal("val"), false),
			},
			errExpect: ErrMissingTarget,
		},
		{
			name: "add empty log_attribute path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-empty-log-attr",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logAttrTarget(), strVal("val"), false),
			},
			errSubstr: "log attribute path cannot be empty",
		},
		{
			name: "add empty resource_attribute path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-empty-res-attr",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(resourceAttrTarget(), strVal("val"), false),
			},
			errSubstr: "resource attribute path cannot be empty",
		},
		{
			name: "add empty scope_attribute path",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-empty-scope-attr",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(scopeAttrTarget(), strVal("val"), false),
			},
			errSubstr: "scope attribute path cannot be empty",
		},
		{
			name: "add log_attribute missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-log-attr-no-val",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(logAttrTarget("k"), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add unspecified record_field",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-unspec-record-field",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_UNSPECIFIED), strVal("val"), false),
			},
			errSubstr: "log record field cannot be unspecified",
		},
		{
			name: "add unspecified scope_field",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-unspec-scope-field",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED), strVal("val"), false),
			},
			errSubstr: "scope field cannot be unspecified",
		},
		{
			name: "add unsupported SCOPE_FIELD_SCHEMA_URL",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-schema-url",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL), strVal("https://opentelemetry.io/schemas/1.24.0"), false),
			},
			errSubstr: "scope field SCHEMA_URL is not supported as a transform target",
		},
		{
			name: "add BODY missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-body-nil-val",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), &policyv1alpha1.Value{}, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SEVERITY_TEXT missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevtext-nil-val",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SEVERITY_TEXT non-string value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevtext-int",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), intVal(9), false),
			},
			errSubstr: "SEVERITY_TEXT requires a string value",
		},
		{
			name: "add SEVERITY_NUMBER missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevnum-nil-val",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SEVERITY_NUMBER non-int value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevnum-str",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), strVal("INFO"), false),
			},
			errSubstr: "SEVERITY_NUMBER requires an int value",
		},
		{
			name: "add SEVERITY_NUMBER out of range low (0)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevnum-zero",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(0), false),
			},
			errSubstr: "must be in range [1, 24], got 0",
		},
		{
			name: "add SEVERITY_NUMBER out of range high (25)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-sevnum-25",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(25), false),
			},
			errSubstr: "must be in range [1, 24], got 25",
		},
		{
			name: "add TRACE_ID missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-traceid-nil",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add TRACE_ID invalid hex string",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-traceid-bad-hex",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), strVal("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"), false),
			},
			errSubstr: "TRACE_ID requires a 32-character hex string or 16-byte bytes value",
		},
		{
			name: "add TRACE_ID wrong length hex string",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-traceid-short-hex",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), strVal("01020304"), false),
			},
			errSubstr: "TRACE_ID requires a 32-character hex string or 16-byte bytes value",
		},
		{
			name: "add TRACE_ID wrong length bytes",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-traceid-short-bytes",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), bytesVal([]byte{1, 2, 3}), false),
			},
			errSubstr: "TRACE_ID requires a 32-character hex string or 16-byte bytes value",
		},
		{
			name: "add TRACE_ID wrong value type (int)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-traceid-int",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), intVal(123), false),
			},
			errSubstr: "TRACE_ID requires a 32-character hex string or 16-byte bytes value",
		},
		{
			name: "add SPAN_ID missing value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-spanid-nil",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), nil, false),
			},
			errExpect: ErrMissingValue,
		},
		{
			name: "add SPAN_ID invalid hex string",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-spanid-bad-hex",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), strVal("zzzzzzzzzzzzzzzz"), false),
			},
			errSubstr: "SPAN_ID requires a 16-character hex string or 8-byte bytes value",
		},
		{
			name: "add SPAN_ID wrong length hex string",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-spanid-short-hex",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), strVal("0102"), false),
			},
			errSubstr: "SPAN_ID requires a 16-character hex string or 8-byte bytes value",
		},
		{
			name: "add SPAN_ID wrong length bytes",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-spanid-short-bytes",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), bytesVal([]byte{1, 2, 3}), false),
			},
			errSubstr: "SPAN_ID requires a 16-character hex string or 8-byte bytes value",
		},
		{
			name: "add SPAN_ID wrong value type (int)",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-spanid-int",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), intVal(123), false),
			},
			errSubstr: "SPAN_ID requires a 16-character hex string or 8-byte bytes value",
		},
		{
			name: "add SCOPE_FIELD_NAME non-string value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-scopename-int",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME), intVal(123), false),
			},
			errSubstr: "scope field NAME requires a string value",
		},
		{
			name: "add SCOPE_FIELD_VERSION non-string value",
			proto: &policyv1alpha1.LogTransformPolicy{
				Id:      "p-scopeversion-bool",
				Matches: []*policyv1alpha1.LogMatcher{validMatcher},
				Action:  addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION), boolVal(true), false),
			},
			errSubstr: "scope field VERSION requires a string value",
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
		Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
		Action:  addAction(logAttrTarget("tier"), strVal("soc"), false),
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

func TestDriverLoadPolicyAndProto(t *testing.T) {
	t.Run("LoadPolicy from raw map", func(t *testing.T) {
		raw := map[string]any{
			"type": "log_transform",
			"id":   "loaded-via-driver",
			"matches": []any{
				map[string]any{
					"target": map[string]any{
						"record_field": "LOG_RECORD_FIELD_BODY",
					},
					"exists": map[string]any{},
				},
			},
			"add": map[string]any{
				"target": map[string]any{
					"log_attribute": map[string]any{
						"path": []any{"gcp", "routing", "tier"},
					},
				},
				"value": map[string]any{
					"string_value": "soc",
				},
				"upsert": true,
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
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logAttrTarget("env"), strVal("prod"), false),
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
					"matches": "not-an-array",
				},
			},
			{
				name: "policy validation error (missing action)",
				raw: map[string]any{
					"type": "log_transform",
					"id":   "missing-action",
					"matches": []any{
						map[string]any{
							"target": map[string]any{"record_field": "LOG_RECORD_FIELD_BODY"},
							"exists": map[string]any{},
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
			action *policyv1alpha1.LogTransformPolicy_Add
		}{
			{"add-str", addAction(logAttrTarget("str_key"), strVal("hello"), false)},
			{"add-int", addAction(logAttrTarget("int_key"), intVal(42), false)},
			{"add-double", addAction(logAttrTarget("double_key"), doubleVal(3.14), false)},
			{"add-bool", addAction(logAttrTarget("bool_key"), boolVal(true), false)},
			{"add-bytes", addAction(logAttrTarget("bytes_key"), bytesVal([]byte{0xde, 0xad, 0xbe, 0xef}), false)},
			{"add-nested", addAction(logAttrTarget("gcp", "routing", "tier"), strVal("soc"), false)},
		}

		for _, tc := range actions {
			pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
				Id:      tc.id,
				Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
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
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logAttrTarget("http.method"), strVal("POST"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noUpsertFlat.TransformLog(ctx))

		noUpsertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "log-attr-no-upsert-nested",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logAttrTarget("metadata", "env"), strVal("staging"), false),
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
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logAttrTarget("http.method"), strVal("POST"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, upsertFlat.TransformLog(ctx))

		upsertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "log-attr-upsert-nested",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logAttrTarget("metadata", "env"), strVal("staging"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, upsertNested.TransformLog(ctx))

		v, _ = lr.Attributes().Get("http.method")
		assert.Equal(t, "POST", v.Str())
		metaVal, _ = lr.Attributes().Get("metadata")
		envVal, _ = metaVal.Map().Get("env")
		assert.Equal(t, "staging", envVal.Str())
	})

	t.Run("resource_attribute flat and nested with upsert false vs true", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		ctx := logContext(lr, sl, rl)

		// Insert new flat and nested resource attributes with upsert: false.
		insertFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "res-attr-insert-flat",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(resourceAttrTarget("cloud.region"), strVal("us-central1"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, insertFlat.TransformLog(ctx))

		insertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "res-attr-insert-nested",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(resourceAttrTarget("cloud", "account", "tier"), strVal("enterprise"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, insertNested.TransformLog(ctx))

		regVal, ok := rl.Resource().Attributes().Get("cloud.region")
		require.True(t, ok)
		assert.Equal(t, "us-central1", regVal.Str())

		cloudVal, ok := rl.Resource().Attributes().Get("cloud")
		require.True(t, ok)
		acctVal, ok := cloudVal.Map().Get("account")
		require.True(t, ok)
		tierVal, ok := acctVal.Map().Get("tier")
		require.True(t, ok)
		assert.Equal(t, "enterprise", tierVal.Str())

		// Re-running with upsert: false on already existing attributes yields TransformNoMatch and does not overwrite.
		noOverwriteFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "res-attr-no-overwrite-flat",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(resourceAttrTarget("cloud.zone"), strVal("europe-west1-b"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noOverwriteFlat.TransformLog(ctx))

		noOverwriteNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "res-attr-no-overwrite-nested",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(resourceAttrTarget("cloud", "account", "tier"), strVal("free"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noOverwriteNested.TransformLog(ctx))

		zoneVal, _ := rl.Resource().Attributes().Get("cloud.zone")
		assert.Equal(t, "us-central1-a", zoneVal.Str())

		// Running with upsert: true overwrites both.
		overwriteFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "res-attr-overwrite-flat",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(resourceAttrTarget("cloud.zone"), strVal("europe-west1-b"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, overwriteFlat.TransformLog(ctx))

		overwriteNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "res-attr-overwrite-nested",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(resourceAttrTarget("cloud", "account", "tier"), strVal("free"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, overwriteNested.TransformLog(ctx))

		zoneVal, _ = rl.Resource().Attributes().Get("cloud.zone")
		assert.Equal(t, "europe-west1-b", zoneVal.Str())
		cloudVal, _ = rl.Resource().Attributes().Get("cloud")
		acctVal, _ = cloudVal.Map().Get("account")
		tierVal, _ = acctVal.Map().Get("tier")
		assert.Equal(t, "free", tierVal.Str())
	})

	t.Run("scope_attribute flat and nested with upsert false vs true", func(t *testing.T) {
		lr, sl, rl := newTestLogBundle()
		ctx := logContext(lr, sl, rl)

		insertFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-attr-insert-flat",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(scopeAttrTarget("scope.owner"), strVal("observability"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, insertFlat.TransformLog(ctx))

		insertNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-attr-insert-nested",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(scopeAttrTarget("scope", "meta", "tier"), strVal("gold"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, insertNested.TransformLog(ctx))

		ownerVal, ok := sl.Scope().Attributes().Get("scope.owner")
		require.True(t, ok)
		assert.Equal(t, "observability", ownerVal.Str())

		// Upsert: false does not overwrite existing scope.tag ("backend") or scope.meta.tier ("gold").
		noOverwriteFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-attr-no-overwrite-flat",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(scopeAttrTarget("scope.tag"), strVal("frontend"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noOverwriteFlat.TransformLog(ctx))

		noOverwriteNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-attr-no-overwrite-nested",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(scopeAttrTarget("scope", "meta", "tier"), strVal("silver"), false),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformNoMatch, noOverwriteNested.TransformLog(ctx))

		tagVal, _ := sl.Scope().Attributes().Get("scope.tag")
		assert.Equal(t, "backend", tagVal.Str())

		// Upsert: true overwrites both.
		overwriteFlat, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-attr-overwrite-flat",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(scopeAttrTarget("scope.tag"), strVal("frontend"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, overwriteFlat.TransformLog(ctx))

		overwriteNested, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-attr-overwrite-nested",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(scopeAttrTarget("scope", "meta", "tier"), strVal("silver"), true),
		})
		require.NoError(t, err)
		assert.Equal(t, googlepolicy.TransformModified, overwriteNested.TransformLog(ctx))

		tagVal, _ = sl.Scope().Attributes().Get("scope.tag")
		assert.Equal(t, "frontend", tagVal.Str())
		scopeMap, _ := sl.Scope().Attributes().Get("scope")
		metaMap, _ := scopeMap.Map().Get("meta")
		tierVal, _ := metaMap.Map().Get("tier")
		assert.Equal(t, "silver", tierVal.Str())
	})
}

func TestTransformLogRecordFields(t *testing.T) {
	t.Run("LOG_RECORD_FIELD_BODY empty, empty-string, non-empty string, and non-string bodies", func(t *testing.T) {
		_, sl, rl := newTestLogBundle()

		polNoUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "body-no-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), strVal("default body"), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "body-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), strVal("overwritten body"), true),
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
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), strVal("WARN"), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "sev-text-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), strVal("ERROR"), true),
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
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(int64(plog.SeverityNumberWarn)), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "sev-num-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(int64(plog.SeverityNumberError)), true),
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

	t.Run("LOG_RECORD_FIELD_TRACE_ID hex string and bytes with upsert false vs true", func(t *testing.T) {
		_, sl, rl := newTestLogBundle()
		tid1Bytes := [16]byte{0xaa, 0xbb, 0xcc, 0xdd, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00, 0xab, 0xcd}
		tid2Bytes := [16]byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}

		polHexNoUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "trace-id-hex-no-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), strVal("aabbccdd11223344556677889900abcd"), false),
		})
		require.NoError(t, err)

		polBytesUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "trace-id-bytes-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), bytesVal(tid2Bytes[:]), true),
		})
		require.NoError(t, err)

		lr := plog.NewLogRecord()
		ctx := logContext(lr, sl, rl)

		// Empty TraceID -> upsert: false sets it from 32-char hex string.
		assert.Equal(t, googlepolicy.TransformModified, polHexNoUpsert.TransformLog(ctx))
		assert.Equal(t, pcommon.TraceID(tid1Bytes), lr.TraceID())

		// Non-empty TraceID -> upsert: false does not overwrite.
		assert.Equal(t, googlepolicy.TransformNoMatch, polHexNoUpsert.TransformLog(ctx))
		assert.Equal(t, pcommon.TraceID(tid1Bytes), lr.TraceID())

		// Upsert: true overwrites with 16-byte bytes_value.
		assert.Equal(t, googlepolicy.TransformModified, polBytesUpsert.TransformLog(ctx))
		assert.Equal(t, pcommon.TraceID(tid2Bytes), lr.TraceID())
	})

	t.Run("LOG_RECORD_FIELD_SPAN_ID hex string and bytes with upsert false vs true", func(t *testing.T) {
		_, sl, rl := newTestLogBundle()
		sid1Bytes := [8]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
		sid2Bytes := [8]byte{8, 7, 6, 5, 4, 3, 2, 1}

		polHexNoUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "span-id-hex-no-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), strVal("1122334455667788"), false),
		})
		require.NoError(t, err)

		polBytesUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "span-id-bytes-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), bytesVal(sid2Bytes[:]), true),
		})
		require.NoError(t, err)

		lr := plog.NewLogRecord()
		ctx := logContext(lr, sl, rl)

		// Empty SpanID -> upsert: false sets it from 16-char hex string.
		assert.Equal(t, googlepolicy.TransformModified, polHexNoUpsert.TransformLog(ctx))
		assert.Equal(t, pcommon.SpanID(sid1Bytes), lr.SpanID())

		// Non-empty SpanID -> upsert: false does not overwrite.
		assert.Equal(t, googlepolicy.TransformNoMatch, polHexNoUpsert.TransformLog(ctx))
		assert.Equal(t, pcommon.SpanID(sid1Bytes), lr.SpanID())

		// Upsert: true overwrites with 8-byte bytes_value.
		assert.Equal(t, googlepolicy.TransformModified, polBytesUpsert.TransformLog(ctx))
		assert.Equal(t, pcommon.SpanID(sid2Bytes), lr.SpanID())
	})
}

func TestTransformLogScopeFields(t *testing.T) {
	t.Run("SCOPE_FIELD_NAME unset vs set with upsert false vs true", func(t *testing.T) {
		lr := plog.NewLogRecord()
		sl := plog.NewScopeLogs()
		rl := plog.NewResourceLogs()
		ctx := logContext(lr, sl, rl)

		polNoUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-name-no-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME), strVal("default.scope"), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-name-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME), strVal("overwritten.scope"), true),
		})
		require.NoError(t, err)

		assert.Equal(t, googlepolicy.TransformModified, polNoUpsert.TransformLog(ctx))
		assert.Equal(t, "default.scope", sl.Scope().Name())

		assert.Equal(t, googlepolicy.TransformNoMatch, polNoUpsert.TransformLog(ctx))
		assert.Equal(t, "default.scope", sl.Scope().Name())

		assert.Equal(t, googlepolicy.TransformModified, polUpsert.TransformLog(ctx))
		assert.Equal(t, "overwritten.scope", sl.Scope().Name())
	})

	t.Run("SCOPE_FIELD_VERSION unset vs set with upsert false vs true", func(t *testing.T) {
		lr := plog.NewLogRecord()
		sl := plog.NewScopeLogs()
		rl := plog.NewResourceLogs()
		ctx := logContext(lr, sl, rl)

		polNoUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-ver-no-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION), strVal("v1.0.0"), false),
		})
		require.NoError(t, err)

		polUpsert, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "scope-ver-upsert",
			Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
			Action:  addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION), strVal("v2.0.0"), true),
		})
		require.NoError(t, err)

		assert.Equal(t, googlepolicy.TransformModified, polNoUpsert.TransformLog(ctx))
		assert.Equal(t, "v1.0.0", sl.Scope().Version())

		assert.Equal(t, googlepolicy.TransformNoMatch, polNoUpsert.TransformLog(ctx))
		assert.Equal(t, "v1.0.0", sl.Scope().Version())

		assert.Equal(t, googlepolicy.TransformModified, polUpsert.TransformLog(ctx))
		assert.Equal(t, "v2.0.0", sl.Scope().Version())
	})
}

func TestTransformLogMultipleMatchersAndSequentialPolicies(t *testing.T) {
	t.Run("multiple matchers AND semantics - non-matching leaves record untouched", func(t *testing.T) {
		pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id: "multi-matcher-policy",
			Matches: []*policyv1alpha1.LogMatcher{
				{
					Target: logAttrTarget("http.method"),
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: strVal("GET"),
					},
				},
				{
					Target: logAttrTarget("http.status_code"),
					Predicate: &policyv1alpha1.LogMatcher_Equals{
						Equals: intVal(500), // bundle has 200
					},
				},
			},
			Action: addAction(logAttrTarget("alert"), boolVal(true), true),
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
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logAttrTarget("http.method"), strVal("POST"), false),
		})
		require.NoError(t, err)

		p2, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "02-insert-stage-first",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logAttrTarget("stage"), strVal("first"), false),
		})
		require.NoError(t, err)

		p3, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
			Id:      "03-overwrite-stage-second",
			Matches: []*policyv1alpha1.LogMatcher{bodyExistsMatcher()},
			Action:  addAction(logAttrTarget("stage"), strVal("second"), true),
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
		action *policyv1alpha1.LogTransformPolicy_Add
	}{
		{
			name:   "log_attribute",
			action: addAction(logAttrTarget("k"), strVal("v"), true),
		},
		{
			name:   "resource_attribute",
			action: addAction(resourceAttrTarget("k"), strVal("v"), true),
		},
		{
			name:   "scope_attribute",
			action: addAction(scopeAttrTarget("k"), strVal("v"), true),
		},
		{
			name:   "record_field BODY",
			action: addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY), strVal("v"), true),
		},
		{
			name:   "record_field SEVERITY_TEXT",
			action: addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT), strVal("INFO"), true),
		},
		{
			name:   "record_field SEVERITY_NUMBER",
			action: addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER), intVal(9), true),
		},
		{
			name:   "record_field TRACE_ID",
			action: addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID), strVal("0102030405060708090a0b0c0d0e0f10"), true),
		},
		{
			name:   "record_field SPAN_ID",
			action: addAction(recordFieldTarget(policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID), strVal("0102030405060708"), true),
		},
		{
			name:   "scope_field NAME",
			action: addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_NAME), strVal("my.scope"), true),
		},
		{
			name:   "scope_field VERSION",
			action: addAction(scopeFieldTarget(policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION), strVal("v1.0.0"), true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pol, err := NewPolicyFromProto(&policyv1alpha1.LogTransformPolicy{
				Id:      "zero-ctx-" + tt.name,
				Matches: []*policyv1alpha1.LogMatcher{alwaysTrueMatcher()},
				Action:  tt.action,
			})
			require.NoError(t, err)
			assert.Equal(t, googlepolicy.TransformNoMatch, pol.TransformLog(googlepolicy.LogContext{}))
		})
	}
}
