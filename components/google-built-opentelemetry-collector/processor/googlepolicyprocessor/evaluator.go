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

package googlepolicyprocessor

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
)

// Evaluator holds the compiled transformation policies for each signal. All
// matching logic lives in the pkg/googlepolicy filter packages; the Evaluator
// only walks the pdata trees and applies the KEEP/DROP outcomes they return.
type Evaluator struct {
	logFilterPolicies                []googlepolicy.LogFilterPolicyEvaluator
	logTransformPolicies             []googlepolicy.LogTransformPolicyEvaluator
	metricFilterPolicies             []googlepolicy.MetricFilterPolicyEvaluator
	instrumentMetricFilterPolicies   []googlepolicy.MetricFilterPolicyEvaluator
	datapointMetricFilterPolicies    []googlepolicy.MetricFilterPolicyEvaluator
	hasDatapointMetricFilterPolicies bool
	traceFilterPolicies              []googlepolicy.TraceFilterPolicyEvaluator
}

// NewEvaluator compiles a slice of TransformationPolicy objects into an Evaluator.
//
// Policies loaded through the googlepolicy registry already arrive compiled by
// their owning filter/transform package and implement the per-signal evaluator
// interfaces directly. A transformation policy that implements none of them
// cannot be evaluated, so it is rejected here rather than silently ignored.
func NewEvaluator(policies []googlepolicy.TransformationPolicy) (*Evaluator, error) {
	ev := &Evaluator{}
	for _, tp := range policies {
		if tp == nil {
			continue
		}

		switch p := tp.(type) {
		case googlepolicy.LogFilterPolicyEvaluator:
			ev.addLogFilterPolicy(p)
		case googlepolicy.LogTransformPolicyEvaluator:
			ev.addLogTransformPolicy(p)
		case googlepolicy.MetricFilterPolicyEvaluator:
			ev.addMetricFilterPolicy(p)
		case googlepolicy.TraceFilterPolicyEvaluator:
			ev.addTraceFilterPolicy(p)
		default:
			return nil, fmt.Errorf(
				"transformation policy %q (%T) implements none of googlepolicy.LogFilterPolicyEvaluator, "+
					"googlepolicy.LogTransformPolicyEvaluator, googlepolicy.MetricFilterPolicyEvaluator "+
					"or googlepolicy.TraceFilterPolicyEvaluator: "+
					"transformation policies must implement one of them",
				tp.PolicyName(), tp)
		}
	}
	slices.SortFunc(ev.logTransformPolicies, func(a, b googlepolicy.LogTransformPolicyEvaluator) int {
		if c := cmp.Compare(a.TransformStage(), b.TransformStage()); c != 0 {
			return c
		}
		return cmp.Compare(a.PolicyName(), b.PolicyName())
	})
	return ev, nil
}

func (e *Evaluator) addLogFilterPolicy(p googlepolicy.LogFilterPolicyEvaluator) {
	e.logFilterPolicies = append(e.logFilterPolicies, p)
}

func (e *Evaluator) addLogTransformPolicy(p googlepolicy.LogTransformPolicyEvaluator) {
	e.logTransformPolicies = append(e.logTransformPolicies, p)
}

func (e *Evaluator) addMetricFilterPolicy(p googlepolicy.MetricFilterPolicyEvaluator) {
	e.metricFilterPolicies = append(e.metricFilterPolicies, p)
	if p.IsDatapointLevel() {
		e.hasDatapointMetricFilterPolicies = true
		e.datapointMetricFilterPolicies = append(e.datapointMetricFilterPolicies, p)
	} else {
		e.instrumentMetricFilterPolicies = append(e.instrumentMetricFilterPolicies, p)
	}
}

func (e *Evaluator) addTraceFilterPolicy(p googlepolicy.TraceFilterPolicyEvaluator) {
	e.traceFilterPolicies = append(e.traceFilterPolicies, p)
}
