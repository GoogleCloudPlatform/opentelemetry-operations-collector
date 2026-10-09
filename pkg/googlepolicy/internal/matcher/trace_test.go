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
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestCompileTraceMatcher_AndExtractor(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.SetSchemaUrl("https://opentelemetry.io/schemas/1.24.0")
	rs.Resource().Attributes().PutStr("service.name", "frontend")

	ss := rs.ScopeSpans().AppendEmpty()
	ss.SetSchemaUrl("https://opentelemetry.io/schemas/1.24.0/scope")
	ss.Scope().SetName("tracer.lib")
	ss.Scope().SetVersion("2.0.0")
	ss.Scope().Attributes().PutStr("scope.env", "prod")

	span := ss.Spans().AppendEmpty()
	span.SetName("GET /checkout")
	span.SetKind(ptrace.SpanKindServer)
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetParentSpanID(pcommon.SpanID([8]byte{9, 10, 11, 12, 13, 14, 15, 16}))
	span.Status().SetCode(ptrace.StatusCodeError)
	span.Status().SetMessage("deadline exceeded")
	span.Attributes().PutInt("http.status_code", 504)

	ctx := googlepolicy.TraceContext{
		Span:              span,
		Resource:          rs.Resource(),
		Scope:             ss.Scope(),
		ResourceSchemaURL: rs.SchemaUrl(),
		ScopeSchemaURL:    ss.SchemaUrl(),
	}

	tests := []struct {
		name    string
		matcher *policyv1alpha1.TraceMatcher
		want    bool
	}{
		{
			name: "span name equals",
			matcher: &policyv1alpha1.TraceMatcher{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equals{
					Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "GET /checkout"}},
				},
			},
			want: true,
		},
		{
			name: "span kind SERVER",
			matcher: &policyv1alpha1.TraceMatcher{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_KIND,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equals{
					Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "SERVER"}},
				},
			},
			want: true,
		},
		{
			name: "span status code ERROR",
			matcher: &policyv1alpha1.TraceMatcher{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_CODE,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equals{
					Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "ERROR"}},
				},
			},
			want: true,
		},
		{
			name: "span status message contains",
			matcher: &policyv1alpha1.TraceMatcher{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_STATUS_MESSAGE,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Contains{
					Contains: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "deadline"}},
				},
			},
			want: true,
		},
		{
			name: "trace_id, span_id, parent_span_id exist",
			matcher: &policyv1alpha1.TraceMatcher{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_RecordField{
						RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_TRACE_ID,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Exists{Exists: &emptypb.Empty{}},
			},
			want: true,
		},
		{
			name: "span attribute gte",
			matcher: &policyv1alpha1.TraceMatcher{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_SpanAttribute{
						SpanAttribute: &policyv1alpha1.AttributePath{Path: []string{"http.status_code"}},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Gte{
					Gte: &policyv1alpha1.NumericValue{Value: &policyv1alpha1.NumericValue_IntValue{IntValue: 500}},
				},
			},
			want: true,
		},
		{
			name: "resource attribute equals",
			matcher: &policyv1alpha1.TraceMatcher{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ResourceAttribute{
						ResourceAttribute: &policyv1alpha1.AttributePath{Path: []string{"service.name"}},
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equals{
					Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "frontend"}},
				},
			},
			want: true,
		},
		{
			name: "scope attribute and scope fields",
			matcher: &policyv1alpha1.TraceMatcher{
				Target: &policyv1alpha1.TraceFieldSelector{
					Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
					},
				},
				Predicate: &policyv1alpha1.TraceMatcher_Equals{
					Equals: &policyv1alpha1.Value{Value: &policyv1alpha1.Value_StringValue{StringValue: "tracer.lib"}},
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm, err := CompileTraceMatcher(tc.matcher)
			require.NoError(t, err)
			assert.Equal(t, tc.want, cm.Eval(ctx))
			assert.Equal(t, tc.matcher.GetNegate(), cm.Eval(googlepolicy.TraceContext{}))
		})
	}
}

func TestCompileTraceExtractor_Errors(t *testing.T) {
	_, err := CompileTraceExtractor(nil)
	require.ErrorIs(t, err, ErrMissingTraceTarget)

	_, err = CompileTraceExtractor(&policyv1alpha1.TraceFieldSelector{})
	require.ErrorIs(t, err, ErrMissingTraceTarget)

	_, err = CompileTraceExtractor(&policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_RecordField{
			RecordField: policyv1alpha1.SpanRecordField_SPAN_RECORD_FIELD_UNSPECIFIED,
		},
	})
	require.Error(t, err)

	_, err = CompileTraceExtractor(&policyv1alpha1.TraceFieldSelector{
		Target: &policyv1alpha1.TraceFieldSelector_ScopeField{
			ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED,
		},
	})
	require.Error(t, err)
}
