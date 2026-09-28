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
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
)

// Per-policy state is deliberately not reported here.
//
// This extension reports one fact: which policy set is active. Whether an
// individual policy within that set applied successfully is recorded as an
// OTLP event at the point the policy is applied, not as a time series.
//
// An event is the better fit. Policy application is a discrete occurrence with
// a cause, and the interesting payload is the error text, which a metric label
// cannot carry. Encoding it as a gauge also put policy identity and revision
// into series identity, so cardinality grew with the size of the policy set and
// churned on every rollout, to express something that is almost always "fine".

// PolicyStateSource is the narrow, read-only view of control plane state that
// this extension needs.
//
// This is deliberately NOT googlepolicy.Manager. The Manager interface also
// exposes Start, Stop, URI and PolicyEvaluationResult, none of which a
// self-observability component has any business calling:
//
//   - Start/Stop are owned by the config provider. A second caller driving the
//     manager's lifecycle would race with it.
//   - PolicyEvaluationResult is how the provider reports back to the control
//     plane that it rendered a revision. This extension does not perform that
//     work and must not report on it.
//   - URI exposes where policies were loaded from (file path, project, fleet),
//     which is a config-resolution concern that should not leak into a
//     component that merely observes.
//
// Keeping the dependency this narrow means the extension can only read, never
// drive, the control plane.
type PolicyStateSource interface {
	// ActivePolicySet returns the identity of the active policy set.
	ActivePolicySet() PolicySetState
}

// PolicySetState identifies the active policy set.
//
// Both fields are read from one snapshot of the registry, so they always
// describe the same policy set. Reading them separately would let a concurrent
// rollout pair one revision's ID with another's revision.
type PolicySetState struct {
	// ID is the PolicySet resource name that the active policies belong to,
	// or the empty string if it cannot be determined. See
	// googlepolicy.PolicySet.PolicySetID for how it is derived.
	ID string
	// Revision is the revision ID of the active policy set, or the empty
	// string if no policy set is active.
	Revision string
}

// registrySource is the production PolicyStateSource, backed by the process
// wide policy registry in pkg/googlepolicy. All coupling to the policy engine
// is confined to this type.
type registrySource struct{}

var _ PolicyStateSource = registrySource{}

// ActivePolicySet treats a policy set with no policies the same as no policy
// set. Both managers activate such a set with a non-empty revision: the file
// manager for an empty policy directory, and the xDS manager when the control
// plane sends an empty revision to clear all policies. Nothing is enforced in
// either case, so reporting it as active would be misleading.
func (registrySource) ActivePolicySet() PolicySetState {
	ps := googlepolicy.ActivePolicySet()
	if ps == nil || len(ps.Policies) == 0 {
		return PolicySetState{}
	}
	return PolicySetState{
		ID:       ps.PolicySetID,
		Revision: ps.RevisionID,
	}
}
