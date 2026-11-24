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
	"go.opentelemetry.io/otel/metric"

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
	// IsControlPlaneActive reports whether the control plane is active.
	IsControlPlaneActive() bool
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

// xdsMetricsBinder receives the MeterProvider that the xDS client records its
// own metrics on.
//
// The xDS client is created by the config provider, before the collector has a
// MeterProvider, so the provider is handed over once this extension starts.
//
// This is deliberately not part of PolicyStateSource. That interface is
// read-only, and this is the one thing the extension pushes the other way.
type xdsMetricsBinder interface {
	// BindXDSMeterProvider sets the MeterProvider for the xDS client metrics.
	// A nil MeterProvider unbinds the current one.
	BindXDSMeterProvider(mp metric.MeterProvider)
}

// registrySource is the production PolicyStateSource and xdsMetricsBinder,
// backed by the process wide state in pkg/googlepolicy. All coupling to the
// policy engine is confined to this type.
type registrySource struct{}

var (
	_ PolicyStateSource = registrySource{}
	_ xdsMetricsBinder  = registrySource{}
)

// ActivePolicySet returns the identity and revision of the active policy set.
//
// An unnamed policy set with zero policies (ps.PolicySetID == "" &&
// len(ps.Policies) == 0) is treated as inactive, covering an empty local file
// policy directory or an xDS response indicating the fleet has no assigned
// policy set. By contrast, a control-plane policy set revision with an explicit
// PolicySetID (ps.PolicySetID != "") is reported even when len(ps.Policies) == 0
// so rollout convergence can distinguish a collector running an empty policy set
// revision from an unconfigured collector.
func (registrySource) ActivePolicySet() PolicySetState {
	ps := googlepolicy.ActivePolicySet()
	if ps == nil || (ps.PolicySetID == "" && len(ps.Policies) == 0) {
		return PolicySetState{}
	}
	return PolicySetState{
		ID:       ps.PolicySetID,
		Revision: ps.RevisionID,
	}
}

// IsControlPlaneActive reports whether the control plane policy engine is
// active in this process. When the extension is configured independently
// without the control plane provider, this returns false so no control plane
// metrics are emitted.
func (registrySource) IsControlPlaneActive() bool {
	return googlepolicy.IsControlPlaneActive()
}

// BindXDSMeterProvider hands the MeterProvider to the xDS policy managers in
// this process.
func (registrySource) BindXDSMeterProvider(mp metric.MeterProvider) {
	googlepolicy.SetXDSMeterProvider(mp)
}
