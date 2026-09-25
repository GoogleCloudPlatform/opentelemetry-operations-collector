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

package googlecontrolplaneextension

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
)

// stubPolicy is a minimal googlepolicy.Policy implementation for exercising
// registrySource, which is the only part of this package that touches the
// process wide policy registry.
type stubPolicy struct {
	name       string
	policyType string
	class      googlepolicy.PolicyClass
}

func (s stubPolicy) PolicyName() string                    { return s.name }
func (s stubPolicy) PolicyType() string                    { return s.policyType }
func (s stubPolicy) PolicyClass() googlepolicy.PolicyClass { return s.class }
func (s stubPolicy) Validate() error                       { return nil }

func setActivePolicySet(t *testing.T, ps *googlepolicy.PolicySet) {
	t.Helper()
	googlepolicy.SetActivePolicySet(ps)
	t.Cleanup(func() {
		for googlepolicy.ActivePolicySet() != nil {
			googlepolicy.RollbackActivePolicySet()
		}
	})
}

func TestRegistrySource_NoActivePolicySet(t *testing.T) {
	assert.Equal(t, PolicySetState{}, registrySource{}.ActivePolicySet())
}

// An activated policy set with no policies, as produced by an empty policy
// directory or an xDS revision that clears all policies, enforces nothing and
// must read as inactive even though it carries a revision.
func TestRegistrySource_EmptyPolicySetIsInactive(t *testing.T) {
	setActivePolicySet(t, &googlepolicy.PolicySet{
		RevisionID: "rev-empty",
		Policies:   map[string]*googlepolicy.PolicySetEntry{},
	})

	assert.Equal(t, PolicySetState{}, registrySource{}.ActivePolicySet())
}

func TestPolicySetNameOf(t *testing.T) {
	for _, tc := range []struct {
		name      string
		in        string
		wantSetID string
		wantOK    bool
	}{
		{
			name:      "control plane policy name",
			in:        "projects/my-project/locations/us-central1/policySets/demo-ps/policies/log-filter-0",
			wantSetID: "projects/my-project/locations/us-central1/policySets/demo-ps",
			wantOK:    true,
		},
		{
			// The policy id itself may contain slashes; only the last
			// "/policies/" separates the set name from the id.
			name:      "policy id containing a slash",
			in:        "projects/p/locations/l/policySets/ps/policies/a/b",
			wantSetID: "projects/p/locations/l/policySets/ps",
			wantOK:    true,
		},
		{
			name: "file sourced plain name",
			in:   "my_local_policy",
		},
		{
			name: "no policies segment",
			in:   "projects/p/locations/l/policySets/ps",
		},
		{
			name: "prefix is not a policy set resource name",
			in:   "projects/p/locations/l/policies/log-filter-0",
		},
		{
			name: "extra segments after the policy set",
			in:   "projects/p/locations/l/policySets/ps/extra/policies/x",
		},
		{
			name: "empty",
			in:   "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setID, ok := policySetNameOf(tc.in)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantSetID, setID)
		})
	}
}

func TestRegistrySource_DerivesPolicySetID(t *testing.T) {
	const psName = "projects/demo-project/locations/us-central1/policySets/demo-ps"

	first := stubPolicy{name: psName + "/policies/log-filter-0", policyType: "filter", class: googlepolicy.PolicyClassTransformation}
	second := stubPolicy{name: psName + "/policies/log-filter-1", policyType: "filter", class: googlepolicy.PolicyClassTransformation}

	setActivePolicySet(t, &googlepolicy.PolicySet{
		RevisionID: "rev-abc",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			first.name:  {PolicyObj: first, Processed: true},
			second.name: {PolicyObj: second, Processed: true},
		},
	})

	assert.Equal(t, PolicySetState{ID: psName, Revision: "rev-abc"}, registrySource{}.ActivePolicySet())
}

// Policies that do not carry resource names, such as file sourced ones, must
// not prevent derivation from the ones that do.
func TestRegistrySource_IgnoresNonResourceNames(t *testing.T) {
	const psName = "projects/demo-project/locations/us-central1/policySets/demo-ps"

	cp := stubPolicy{name: psName + "/policies/log-filter-0", policyType: "filter", class: googlepolicy.PolicyClassTransformation}
	local := stubPolicy{name: "default_gcp_destination", policyType: "gcp_destination", class: googlepolicy.PolicyClassDestination}

	setActivePolicySet(t, &googlepolicy.PolicySet{
		RevisionID: "rev-abc",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			cp.name:    {PolicyObj: cp, Processed: true},
			local.name: {PolicyObj: local, Processed: true},
		},
	})

	assert.Equal(t, PolicySetState{ID: psName, Revision: "rev-abc"}, registrySource{}.ActivePolicySet())
}

// If policies claim different policy sets there is no single correct answer.
// Reporting one of them would produce a wrong but plausible label, so report
// nothing instead.
func TestRegistrySource_ConflictingPolicySetsYieldEmpty(t *testing.T) {
	a := stubPolicy{name: "projects/p/locations/l/policySets/set-a/policies/x", policyType: "filter", class: googlepolicy.PolicyClassTransformation}
	b := stubPolicy{name: "projects/p/locations/l/policySets/set-b/policies/y", policyType: "filter", class: googlepolicy.PolicyClassTransformation}

	setActivePolicySet(t, &googlepolicy.PolicySet{
		RevisionID: "rev-abc",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			a.name: {PolicyObj: a, Processed: true},
			b.name: {PolicyObj: b, Processed: true},
		},
	})

	assert.Equal(t, PolicySetState{Revision: "rev-abc"}, registrySource{}.ActivePolicySet())
}

// A file sourced policy set is active but carries no identity.
func TestRegistrySource_NoDerivablePolicySetID(t *testing.T) {
	local := stubPolicy{name: "default_gcp_destination", policyType: "gcp_destination", class: googlepolicy.PolicyClassDestination}

	setActivePolicySet(t, &googlepolicy.PolicySet{
		RevisionID: "rev-file-hash",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			local.name: {PolicyObj: local, Processed: true},
		},
	})

	assert.Equal(t, PolicySetState{Revision: "rev-file-hash"}, registrySource{}.ActivePolicySet())
}
