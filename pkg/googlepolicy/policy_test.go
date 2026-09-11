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

package googlepolicy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTransformationPolicy struct {
	name    string
	signals []Signal
}

func (m *mockTransformationPolicy) PolicyName() string {
	return m.name
}

func (m *mockTransformationPolicy) PolicyType() string {
	return "mock_transformation"
}

func (m *mockTransformationPolicy) PolicyClass() PolicyClass {
	return PolicyClassTransformation
}

func (m *mockTransformationPolicy) Validate() error {
	return nil
}

func (m *mockTransformationPolicy) TargetSignals() []Signal {
	return m.signals
}

var _ TransformationPolicy = (*mockTransformationPolicy)(nil)

type mockOtherPolicy struct {
	name  string
	class PolicyClass
}

func (m *mockOtherPolicy) PolicyName() string {
	return m.name
}

func (m *mockOtherPolicy) PolicyType() string {
	return "mock_other"
}

func (m *mockOtherPolicy) PolicyClass() PolicyClass {
	return m.class
}

func (m *mockOtherPolicy) Validate() error {
	return nil
}

var _ Policy = (*mockOtherPolicy)(nil)

func TestPolicySet_TransformationPolicies(t *testing.T) {
	logFilter := &mockTransformationPolicy{
		name:    "log-filter-1",
		signals: []Signal{SignalLogs},
	}
	metricFilter := &mockTransformationPolicy{
		name:    "metric-filter-1",
		signals: []Signal{SignalMetrics},
	}
	destPolicy := &mockOtherPolicy{
		name:  "dest-1",
		class: PolicyClassDestination,
	}
	sourcePolicy := &mockOtherPolicy{
		name:  "source-1",
		class: PolicyClassSource,
	}

	ps := &PolicySet{
		RevisionID: "rev-1",
		ReceivedAt: time.Now(),
		Policies: map[string]*PolicySetEntry{
			"log-filter-1":    {PolicyObj: logFilter},
			"metric-filter-1": {PolicyObj: metricFilter},
			"dest-1":          {PolicyObj: destPolicy},
			"source-1":        {PolicyObj: sourcePolicy},
		},
	}

	transPolicies := ps.TransformationPolicies()
	require.Len(t, transPolicies, 2)

	names := []string{transPolicies[0].PolicyName(), transPolicies[1].PolicyName()}
	assert.Contains(t, names, "log-filter-1")
	assert.Contains(t, names, "metric-filter-1")

	for _, tp := range transPolicies {
		assert.Equal(t, PolicyClassTransformation, tp.PolicyClass())
		assert.NotEmpty(t, tp.TargetSignals())
	}
}

func TestPolicySet_TransformationPolicies_Empty(t *testing.T) {
	destPolicy := &mockOtherPolicy{
		name:  "dest-1",
		class: PolicyClassDestination,
	}
	ps := &PolicySet{
		RevisionID: "rev-empty",
		ReceivedAt: time.Now(),
		Policies: map[string]*PolicySetEntry{
			"dest-1": {PolicyObj: destPolicy},
		},
	}

	transPolicies := ps.TransformationPolicies()
	assert.Empty(t, transPolicies)
}
