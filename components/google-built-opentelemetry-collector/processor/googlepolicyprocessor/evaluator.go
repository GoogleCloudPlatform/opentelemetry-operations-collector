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
	logPolicies                []googlepolicy.LogPolicyEvaluator
	logTransformPolicies       []googlepolicy.LogTransformPolicyEvaluator
	metricPolicies             []googlepolicy.MetricPolicyEvaluator
	instrumentMetricPolicies   []googlepolicy.MetricPolicyEvaluator
	datapointMetricPolicies    []googlepolicy.MetricPolicyEvaluator
	hasDatapointMetricPolicies bool
	metricTransformPolicies    []googlepolicy.MetricTransformPolicyEvaluator
	tracePolicies              []googlepolicy.TracePolicyEvaluator
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
		case googlepolicy.LogPolicyEvaluator:
			ev.addLogPolicy(p)
		case googlepolicy.LogTransformPolicyEvaluator:
			ev.addLogTransformPolicy(p)
		case googlepolicy.MetricPolicyEvaluator:
			ev.addMetricPolicy(p)
		case googlepolicy.MetricTransformPolicyEvaluator:
			ev.addMetricTransformPolicy(p)
		case googlepolicy.TracePolicyEvaluator:
			ev.addTracePolicy(p)
		default:
			return nil, fmt.Errorf(
				"transformation policy %q (%T) implements none of googlepolicy.LogPolicyEvaluator, "+
					"googlepolicy.LogTransformPolicyEvaluator, googlepolicy.MetricPolicyEvaluator, "+
					"googlepolicy.MetricTransformPolicyEvaluator or googlepolicy.TracePolicyEvaluator: "+
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
	slices.SortFunc(ev.metricTransformPolicies, func(a, b googlepolicy.MetricTransformPolicyEvaluator) int {
		if c := cmp.Compare(a.TransformStage(), b.TransformStage()); c != 0 {
			return c
		}
		return cmp.Compare(a.PolicyName(), b.PolicyName())
	})
	return ev, nil
}

func (e *Evaluator) addLogPolicy(p googlepolicy.LogPolicyEvaluator) {
	e.logPolicies = append(e.logPolicies, p)
}

func (e *Evaluator) addLogTransformPolicy(p googlepolicy.LogTransformPolicyEvaluator) {
	e.logTransformPolicies = append(e.logTransformPolicies, p)
}

func (e *Evaluator) addMetricPolicy(p googlepolicy.MetricPolicyEvaluator) {
	e.metricPolicies = append(e.metricPolicies, p)
	if p.IsDatapointLevel() {
		e.hasDatapointMetricPolicies = true
		e.datapointMetricPolicies = append(e.datapointMetricPolicies, p)
	} else {
		e.instrumentMetricPolicies = append(e.instrumentMetricPolicies, p)
	}
}

func (e *Evaluator) addMetricTransformPolicy(p googlepolicy.MetricTransformPolicyEvaluator) {
	e.metricTransformPolicies = append(e.metricTransformPolicies, p)
}

func (e *Evaluator) addTracePolicy(p googlepolicy.TracePolicyEvaluator) {
	e.tracePolicies = append(e.tracePolicies, p)
}
