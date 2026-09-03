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
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/confmap"
)

var (
	ErrPolicyNotFound           = errors.New("policy not found")
	ErrPolicyTypeFieldMissing   = errors.New("critical policy field 'type' was not found")
	ErrPolicyTypeFieldWrongType = errors.New("critical policy field 'type' is not a string")
)

// PolicyClass is the class of a policy. https://xkcd.com/703/
type PolicyClass string

const (
	PolicyClassDestination    PolicyClass = "destination"
	PolicyClassSource         PolicyClass = "source"
	PolicyClassTransformation PolicyClass = "transformation"
)

// Policy is an interface that all policies of all classes implement.
type Policy interface {
	PolicyName() string
	PolicyType() string
	PolicyClass() PolicyClass
	Validate() error
}

// ComponentPolicy is an extended interface that any policy that produces config
// will implement (currently Destination and Source policies).
type ComponentPolicy interface {
	Policy
	Evaluate() (confmap.Retrieved, error)
}

// PolicyDriver is the interface that is used to load a policy
// object of a known policy type.
type PolicyDriver interface {
	LoadPolicy(raw map[string]any) (Policy, error)
}

// PolicySet is the translation of a set of policies received from a given source
// into internal representations that the Collector can use to evaluate.
type PolicySet struct {
	Policies   map[string]*PolicySetEntry
	RevisionID string
	ReceivedAt time.Time
}

// PolicySetEntry contains the given policy object and a mark for whether it has
// been applied or not, and the error that ocurred if it failed evaluation.
type PolicySetEntry struct {
	PolicyObj Policy
	Processed bool
	Error     error
}

func MakePolicySet(revisionID string, rawPolicyConfigs []map[string]any) (*PolicySet, error) {
	ps := &PolicySet{
		Policies:   make(map[string]*PolicySetEntry, len(rawPolicyConfigs)),
		RevisionID: revisionID,
		ReceivedAt: time.Now(),
	}

	errs := make([]error, 0, len(rawPolicyConfigs))

	for i, rawPolicyConfig := range rawPolicyConfigs {
		// HARD CODED ASSUMPTION: Whether unmarshalled from a set of protos or a set of JSON objects,
		// for each entry the map will contain a top-level field called `type` that contains the policy
		// type. This will match up with the registered PolicyDriver.
		policyTypeRaw, ok := rawPolicyConfig["type"]
		if !ok {
			return nil, fmt.Errorf("%w for policy at index %d", ErrPolicyTypeFieldMissing, i)
		}
		policyType, ok := policyTypeRaw.(string)
		if !ok {
			return nil, fmt.Errorf("%w for policy at index %d", ErrPolicyTypeFieldWrongType, i)
		}

		p, err := LoadPolicy(policyType, rawPolicyConfig)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		ps.Policies[p.PolicyName()] = &PolicySetEntry{PolicyObj: p}
	}

	return ps, errors.Join(errs...)
}

// LoadPoliciesOfClass does what it says on the box.
func (ps *PolicySet) LoadPoliciesOfClass(class PolicyClass) []Policy {
	foundPolicies := make([]Policy, 0, len(ps.Policies))
	for _, pse := range ps.Policies {
		if pse.PolicyObj.PolicyClass() == class {
			foundPolicies = append(foundPolicies, pse.PolicyObj)
		}
	}
	return foundPolicies
}

// MarkPolicySuccessful will mark a policy in the set as processed without error.
func (ps *PolicySet) MarkPolicySuccesful(p Policy) {
	ps.markPolicy(p, nil)
}

// MarkPolicyFailed will mark a policy in the set as processed with the given error.
func (ps *PolicySet) MarkPolicyFailed(p Policy, err error) {
	ps.markPolicy(p, err)
}

func (ps *PolicySet) markPolicy(p Policy, err error) {
	pse, ok := ps.Policies[p.PolicyName()]
	if !ok {
		panic("Attempted to mark a policy as applied when not present in the set. This code state should be impossible.")
	}
	pse.Processed = true
	pse.Error = err
}

// Clone returns a deep copy of the PolicySetEntry, or nil if pse is nil.
func (pse *PolicySetEntry) Clone() *PolicySetEntry {
	if pse == nil {
		return nil
	}
	cp := *pse
	return &cp
}

// Clone returns a deep copy of the PolicySet, or nil if ps is nil.
func (ps *PolicySet) Clone() *PolicySet {
	if ps == nil {
		return nil
	}
	clone := &PolicySet{
		RevisionID: ps.RevisionID,
		ReceivedAt: ps.ReceivedAt,
	}
	if ps.Policies != nil {
		clone.Policies = make(map[string]*PolicySetEntry, len(ps.Policies))
		for k, v := range ps.Policies {
			clone.Policies[k] = v.Clone()
		}
	}
	return clone
}

// type GCPPolicy struct {
// 	Name           string `json:"name"`
// 	ProjectID      string `json:"project_id"`
// 	UniverseDomain string `json:"universe_domain"`
// 	AuthFile       string `json:"auth_file"`
// }

// var _ ComponentPolicy = (*GCPPolicy)(nil)

// func (p *GCPPolicy) PolicyName() string {
// 	return p.Name
// }

// func (p *GCPPolicy) PolicyType() string {
// 	return "gcp_destination"
// }

// func (p *GCPPolicy) PolicyClass() PolicyClass {
// 	return PolicyClassDestination
// }

// func (p *GCPPolicy) Evaluate() (confmap.Retrieved, error) {
// 	authenticator := googleclientauthextension.Config{}
// 	// ugly hardcoded lol
// 	authType, _ := component.NewType("googleclientauth")
// 	authID := component.NewIDWithName(authType, p.Name)

// 	otlpExporter := &otlpexporter.Config{
// 		ClientConfig: configgrpc.ClientConfig{
// 			Endpoint: "telemetry.googleapis.com:443",
// 			Auth: configoptional.Some(configauth.Config{
// 				AuthenticatorID: authID,
// 			}),
// 		},
// 	}
// }

// func (p *GCPPolicy) Validate() error {
// 	if p.Name == "" {
// 		return errors.New("policy must be named")
// 	}
// 	return nil
// }
