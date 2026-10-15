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

package matcher

import (
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestCompileLogMatcherAndExtractor(t *testing.T) {
	t.Run("error cases", func(t *testing.T) {
		_, err := CompileLogExtractor(nil)
		assert.ErrorIs(t, err, ErrMissingLogTarget)

		_, err = CompileLogExtractor(&policyv1alpha1.LogFieldSelector{})
		assert.ErrorIs(t, err, ErrMissingLogTarget)

		_, err = CompileLogExtractor(&policyv1alpha1.LogFieldSelector{
			Target: &policyv1alpha1.LogFieldSelector_RecordField{
				RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_UNSPECIFIED,
			},
		})
		assert.ErrorContains(t, err, "log record field cannot be unspecified")

		_, err = CompileLogExtractor(&policyv1alpha1.LogFieldSelector{
			Target: &policyv1alpha1.LogFieldSelector_ScopeField{
				ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED,
			},
		})
		assert.ErrorContains(t, err, "scope field cannot be unspecified")

		_, err = CompileLogExtractor(&policyv1alpha1.LogFieldSelector{
			Target: &policyv1alpha1.LogFieldSelector_LogAttribute{},
		})
		assert.ErrorContains(t, err, "log attribute path cannot be empty")

		_, err = CompileLogExtractor(&policyv1alpha1.LogFieldSelector{
			Target: &policyv1alpha1.LogFieldSelector_ResourceAttribute{},
		})
		assert.ErrorContains(t, err, "resource attribute path cannot be empty")

		_, err = CompileLogExtractor(&policyv1alpha1.LogFieldSelector{
			Target: &policyv1alpha1.LogFieldSelector_ScopeAttribute{},
		})
		assert.ErrorContains(t, err, "scope attribute path cannot be empty")

		_, err = CompileLogMatcher(&policyv1alpha1.LogMatcher{})
		assert.ErrorIs(t, err, ErrMissingLogTarget)

		_, err = CompileLogMatcher(&policyv1alpha1.LogMatcher{
			Target: &policyv1alpha1.LogFieldSelector{
				Target: &policyv1alpha1.LogFieldSelector_RecordField{
					RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
				},
			},
		})
		assert.ErrorIs(t, err, ErrMissingPredicate)
	})

	t.Run("all selectors populated vs empty/zero context", func(t *testing.T) {
		ld := plog.NewLogs()
		rl := ld.ResourceLogs().AppendEmpty()
		rl.Resource().Attributes().PutStr("res.key", "res-val")
		sl := rl.ScopeLogs().AppendEmpty()
		sl.Scope().SetName("my-scope")
		sl.Scope().SetVersion("v1")
		sl.SetSchemaUrl("https://schema.example/v1")
		sl.Scope().Attributes().PutStr("scope.key", "scope-val")

		lr := sl.LogRecords().AppendEmpty()
		lr.Body().SetStr("hello")
		lr.SetSeverityText("INFO")
		lr.SetSeverityNumber(plog.SeverityNumberInfo)
		tid := pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
		sid := pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
		lr.SetTraceID(tid)
		lr.SetSpanID(sid)
		lr.Attributes().PutStr("log.key", "log-val")

		fullCtx := googlepolicy.LogContext{
			Record:         lr,
			Resource:       rl.Resource(),
			Scope:          sl.Scope(),
			ScopeSchemaURL: sl.SchemaUrl(),
		}

		emptyRecord := plog.NewLogRecord()
		emptyScope := pcommon.NewInstrumentationScope()
		emptyRes := pcommon.NewResource()
		emptyLoadedCtx := googlepolicy.LogContext{
			Record:   emptyRecord,
			Resource: emptyRes,
			Scope:    emptyScope,
		}

		selectors := []struct {
			name    string
			target  *policyv1alpha1.LogFieldSelector
			wantVal any
		}{
			{
				name: "record_field BODY",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
					},
				},
				wantVal: "hello",
			},
			{
				name: "record_field SEVERITY_TEXT",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_TEXT,
					},
				},
				wantVal: "INFO",
			},
			{
				name: "record_field SEVERITY_NUMBER",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SEVERITY_NUMBER,
					},
				},
				wantVal: int64(plog.SeverityNumberInfo),
			},
			{
				name: "record_field TRACE_ID",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_TRACE_ID,
					},
				},
				wantVal: tid,
			},
			{
				name: "record_field SPAN_ID",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_RecordField{
						RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_SPAN_ID,
					},
				},
				wantVal: sid,
			},
			{
				name: "log_attribute",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_LogAttribute{
						LogAttribute: &policyv1alpha1.AttributePath{Path: []string{"log.key"}},
					},
				},
				wantVal: "log-val",
			},
			{
				name: "resource_attribute",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ResourceAttribute{
						ResourceAttribute: &policyv1alpha1.AttributePath{Path: []string{"res.key"}},
					},
				},
				wantVal: "res-val",
			},
			{
				name: "scope_attribute",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ScopeAttribute{
						ScopeAttribute: &policyv1alpha1.AttributePath{Path: []string{"scope.key"}},
					},
				},
				wantVal: "scope-val",
			},
			{
				name: "scope_field NAME",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
					},
				},
				wantVal: "my-scope",
			},
			{
				name: "scope_field VERSION",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
					},
				},
				wantVal: "v1",
			},
			{
				name: "scope_field SCHEMA_URL",
				target: &policyv1alpha1.LogFieldSelector{
					Target: &policyv1alpha1.LogFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
					},
				},
				wantVal: "https://schema.example/v1",
			},
		}

		for _, tc := range selectors {
			t.Run(tc.name, func(t *testing.T) {
				cm, err := CompileLogMatcher(&policyv1alpha1.LogMatcher{
					Target:    tc.target,
					Predicate: &policyv1alpha1.LogMatcher_Exists{},
				})
				require.NoError(t, err)

				assert.True(t, cm.Eval(fullCtx))
				assert.False(t, cm.Eval(googlepolicy.LogContext{}))
				assert.False(t, cm.Eval(emptyLoadedCtx))

				ext, err := CompileLogExtractor(tc.target)
				require.NoError(t, err)
				val, ok := ext(fullCtx)
				assert.True(t, ok)
				assert.Equal(t, tc.wantVal, val)
			})
		}

		// Also verify non-string body (e.g. int) and empty-string body on BODY extractor.
		bodyExt, err := CompileLogExtractor(&policyv1alpha1.LogFieldSelector{
			Target: &policyv1alpha1.LogFieldSelector_RecordField{
				RecordField: policyv1alpha1.LogRecordField_LOG_RECORD_FIELD_BODY,
			},
		})
		require.NoError(t, err)

		lrEmptyStr := plog.NewLogRecord()
		lrEmptyStr.Body().SetStr("")
		val, ok := bodyExt(googlepolicy.LogContext{Record: lrEmptyStr})
		assert.False(t, ok)
		assert.Nil(t, val)

		lrInt := plog.NewLogRecord()
		lrInt.Body().SetInt(123)
		val, ok = bodyExt(googlepolicy.LogContext{Record: lrInt})
		assert.True(t, ok)
		assert.Equal(t, int64(123), val)
	})
}
