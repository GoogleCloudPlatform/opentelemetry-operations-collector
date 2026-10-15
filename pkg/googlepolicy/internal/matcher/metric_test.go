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
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func TestCompileMetricMatcherAndExtractor(t *testing.T) {
	t.Run("error cases", func(t *testing.T) {
		_, err := CompileMetricExtractor(nil)
		assert.ErrorIs(t, err, ErrMissingMetricTarget)

		_, err = CompileMetricExtractor(&policyv1alpha1.MetricFieldSelector{})
		assert.ErrorIs(t, err, ErrMissingMetricTarget)

		_, err = CompileMetricExtractor(&policyv1alpha1.MetricFieldSelector{
			Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
				DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNSPECIFIED,
			},
		})
		assert.ErrorContains(t, err, "metric descriptor field cannot be unspecified")

		_, err = CompileMetricExtractor(&policyv1alpha1.MetricFieldSelector{
			Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
				ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_UNSPECIFIED,
			},
		})
		assert.ErrorContains(t, err, "scope field cannot be unspecified")

		_, err = CompileMetricExtractor(&policyv1alpha1.MetricFieldSelector{
			Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{},
		})
		assert.ErrorContains(t, err, "datapoint attribute path cannot be empty")

		_, err = CompileMetricExtractor(&policyv1alpha1.MetricFieldSelector{
			Target: &policyv1alpha1.MetricFieldSelector_ResourceAttribute{},
		})
		assert.ErrorContains(t, err, "resource attribute path cannot be empty")

		_, err = CompileMetricExtractor(&policyv1alpha1.MetricFieldSelector{
			Target: &policyv1alpha1.MetricFieldSelector_ScopeAttribute{},
		})
		assert.ErrorContains(t, err, "scope attribute path cannot be empty")

		_, err = CompileMetricMatcher(&policyv1alpha1.MetricMatcher{})
		assert.ErrorIs(t, err, ErrMissingMetricTarget)

		_, err = CompileMetricMatcher(&policyv1alpha1.MetricMatcher{
			Target: &policyv1alpha1.MetricFieldSelector{
				Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
					DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
				},
			},
		})
		assert.ErrorIs(t, err, ErrMissingPredicate)
	})

	t.Run("all selectors populated vs empty/zero context", func(t *testing.T) {
		md := pmetric.NewMetrics()
		rm := md.ResourceMetrics().AppendEmpty()
		rm.Resource().Attributes().PutStr("res.key", "res-val")
		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName("my-scope")
		sm.Scope().SetVersion("v1")
		sm.SetSchemaUrl("https://schema.example/v1")
		sm.Scope().Attributes().PutStr("scope.key", "scope-val")

		m := sm.Metrics().AppendEmpty()
		m.SetName("system.cpu.time")
		m.SetDescription("CPU time")
		m.SetUnit("s")
		sum := m.SetEmptySum()
		sum.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
		sum.SetIsMonotonic(true)
		dp := sum.DataPoints().AppendEmpty()
		dp.Attributes().PutStr("dp.key", "dp-val")

		fullCtx := googlepolicy.MetricContext{
			Metric:                 m,
			DatapointAttributes:    dp.Attributes(),
			AggregationTemporality: sum.AggregationTemporality(),
			Resource:               rm.Resource(),
			Scope:                  sm.Scope(),
			ScopeSchemaURL:         sm.SchemaUrl(),
		}

		emptyLoadedCtx := googlepolicy.MetricContext{
			Metric:              pmetric.NewMetric(),
			DatapointAttributes: pcommon.NewMap(),
			Resource:            pcommon.NewResource(),
			Scope:               pcommon.NewInstrumentationScope(),
		}

		selectors := []struct {
			name    string
			target  *policyv1alpha1.MetricFieldSelector
			wantVal any
		}{
			{
				name: "descriptor_field NAME",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_NAME,
					},
				},
				wantVal: "system.cpu.time",
			},
			{
				name: "descriptor_field DESCRIPTION",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_DESCRIPTION,
					},
				},
				wantVal: "CPU time",
			},
			{
				name: "descriptor_field UNIT",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_UNIT,
					},
				},
				wantVal: "s",
			},
			{
				name: "descriptor_field TYPE",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_TYPE,
					},
				},
				wantVal: "SUM",
			},
			{
				name: "descriptor_field AGGREGATION_TEMPORALITY",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_AGGREGATION_TEMPORALITY,
					},
				},
				wantVal: "CUMULATIVE",
			},
			{
				name: "descriptor_field IS_MONOTONIC",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DescriptorField{
						DescriptorField: policyv1alpha1.MetricDescriptorField_METRIC_DESCRIPTOR_FIELD_IS_MONOTONIC,
					},
				},
				wantVal: true,
			},
			{
				name: "datapoint_attribute",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_DatapointAttribute{
						DatapointAttribute: &policyv1alpha1.AttributePath{Path: []string{"dp.key"}},
					},
				},
				wantVal: "dp-val",
			},
			{
				name: "resource_attribute",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ResourceAttribute{
						ResourceAttribute: &policyv1alpha1.AttributePath{Path: []string{"res.key"}},
					},
				},
				wantVal: "res-val",
			},
			{
				name: "scope_attribute",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ScopeAttribute{
						ScopeAttribute: &policyv1alpha1.AttributePath{Path: []string{"scope.key"}},
					},
				},
				wantVal: "scope-val",
			},
			{
				name: "scope_field NAME",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_NAME,
					},
				},
				wantVal: "my-scope",
			},
			{
				name: "scope_field VERSION",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_VERSION,
					},
				},
				wantVal: "v1",
			},
			{
				name: "scope_field SCHEMA_URL",
				target: &policyv1alpha1.MetricFieldSelector{
					Target: &policyv1alpha1.MetricFieldSelector_ScopeField{
						ScopeField: policyv1alpha1.ScopeField_SCOPE_FIELD_SCHEMA_URL,
					},
				},
				wantVal: "https://schema.example/v1",
			},
		}

		for _, tc := range selectors {
			t.Run(tc.name, func(t *testing.T) {
				cm, err := CompileMetricMatcher(&policyv1alpha1.MetricMatcher{
					Target:    tc.target,
					Predicate: &policyv1alpha1.MetricMatcher_Exists{},
				})
				require.NoError(t, err)

				assert.True(t, cm.Eval(fullCtx))
				assert.False(t, cm.Eval(googlepolicy.MetricContext{}))
				assert.False(t, cm.Eval(emptyLoadedCtx))

				ext, err := CompileMetricExtractor(tc.target)
				require.NoError(t, err)
				val, ok := ext(fullCtx)
				assert.True(t, ok)
				assert.Equal(t, tc.wantVal, val)
			})
		}
	})

	t.Run("MetricTypeString and TemporalityString", func(t *testing.T) {
		assert.Equal(t, "GAUGE", MetricTypeString(pmetric.MetricTypeGauge))
		assert.Equal(t, "SUM", MetricTypeString(pmetric.MetricTypeSum))
		assert.Equal(t, "HISTOGRAM", MetricTypeString(pmetric.MetricTypeHistogram))
		assert.Equal(t, "EXPONENTIAL_HISTOGRAM", MetricTypeString(pmetric.MetricTypeExponentialHistogram))
		assert.Equal(t, "SUMMARY", MetricTypeString(pmetric.MetricTypeSummary))
		assert.Equal(t, "UNSPECIFIED", MetricTypeString(pmetric.MetricTypeEmpty))

		assert.Equal(t, "DELTA", TemporalityString(pmetric.AggregationTemporalityDelta))
		assert.Equal(t, "CUMULATIVE", TemporalityString(pmetric.AggregationTemporalityCumulative))
		assert.Equal(t, "UNSPECIFIED", TemporalityString(pmetric.AggregationTemporalityUnspecified))
	})
}
