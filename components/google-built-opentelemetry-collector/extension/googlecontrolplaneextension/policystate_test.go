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

// The ID is taken from the registry snapshot as is, alongside the revision,
// so the two always describe the same policy set.
func TestRegistrySource_ReportsPolicySetIDAndRevision(t *testing.T) {
	const psName = "projects/demo-project/locations/us-central1/policySets/demo-ps"

	p := stubPolicy{name: psName + "/policies/log-filter-0", policyType: "filter", class: googlepolicy.PolicyClassTransformation}

	setActivePolicySet(t, &googlepolicy.PolicySet{
		PolicySetID: psName,
		RevisionID:  "rev-abc",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			p.name: {PolicyObj: p, Processed: true},
		},
	})

	assert.Equal(t, PolicySetState{ID: psName, Revision: "rev-abc"}, registrySource{}.ActivePolicySet())
}

// A file sourced policy set is active but carries no identity.
func TestRegistrySource_NoPolicySetID(t *testing.T) {
	local := stubPolicy{name: "default_gcp_destination", policyType: "gcp_destination", class: googlepolicy.PolicyClassDestination}

	setActivePolicySet(t, &googlepolicy.PolicySet{
		RevisionID: "rev-file-hash",
		Policies: map[string]*googlepolicy.PolicySetEntry{
			local.name: {PolicyObj: local, Processed: true},
		},
	})

	assert.Equal(t, PolicySetState{Revision: "rev-file-hash"}, registrySource{}.ActivePolicySet())
}
