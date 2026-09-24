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
	"slices"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	ErrPolicyTypeAlreadyRegistered  = errors.New("policy type already registered")
	ErrPolicyProtoAlreadyRegistered = errors.New("policy proto already registered")
	ErrPolicyProtoInvalid           = errors.New("policy driver returned an invalid proto")
	ErrPolicyProtoMismatch          = errors.New("policy proto does not match the driver's message type")
	ErrPolicyTypeNotFound           = errors.New("no driver found for policy type")
	ErrPolicyFailedToLoad           = errors.New("failed to load policy")
	ErrPolicyFailedValidation       = errors.New("policy validation failed")
)

// policyRegistry is a central map that tracks all supported policies registered by any components.
//
// Unguarded by design; see RegisterPolicyDriver for the contract that makes
// that safe, and what to do before breaking it.
var policyRegistry = map[string]PolicyDriver{}

// policyProtoRegistry routes a policy proto's fully qualified message name to
// the policy type registered for it. Only drivers implementing
// ProtoPolicyDriver appear here, which is deliberate: it is the set of policies
// that can be delivered as a bare proto rather than as an authored config with
// an explicit "type" field.
//
// Unguarded by design, on the same terms as policyRegistry.
var policyProtoRegistry = map[protoreflect.FullName]string{}

// maxPreviousPolicySets bounds how many superseded policy sets are retained.
//
// A collector attached to a control plane takes a new revision for the lifetime
// of the process, and every superseded set was previously kept forever. Each one
// pins its whole Policies map -- every loaded policy object, with whatever
// compiled matchers and statements it holds -- so an untrimmed history is a slow
// leak proportional to how often the fleet's policies are edited, on a process
// expected to run for months.
//
// The depth is not correctness-bearing: rollback only ever walks back one set at
// a time, and nothing outside tests reads the history today. It is sized to
// leave room to inspect recent revisions when debugging, not to guarantee that
// any particular revision is still reachable.
//
// Declared as a var so tests can shrink it without pushing hundreds of
// revisions through the registry; defaultMaxPreviousPolicySets lets them put it
// back without restating the number.
const defaultMaxPreviousPolicySets = 10

var maxPreviousPolicySets = defaultMaxPreviousPolicySets

var (
	policySetMu        sync.RWMutex
	activePolicySet    *PolicySet
	previousPolicySets = []*PolicySet{}
	watcherChannels    = []chan struct{}{}
)

// WatcherChannel is a receive-only channel that receives a signal when activePolicySet changes.
type WatcherChannel <-chan struct{}

// RegisterPolicyDriver is how a component registers support for a new policy by providing
// its own PolicyDriver and Policy.
//
// If the driver also implements ProtoPolicyDriver, its proto message name is
// registered as a route to policyType, so a policy arriving as a bare proto can
// be matched to this driver.
//
// Call this from a package init function, and only from there.
//
// The two registries it writes are deliberately unguarded by any mutex. That is
// safe only because of when the writes happen: package initialisation completes
// before main runs, so every write is ordered before any goroutine the program
// later starts, including the xDS stream goroutine that reads both maps on each
// revision (see LoadPolicyFromProto). Registering after startup -- lazily on
// first use, or from a component's Start -- would put a map write next to those
// reads. Go does not treat that as a recoverable error: it kills the process
// with "fatal error: concurrent map read and map write", intermittently and in
// production. Guard both maps with a sync.RWMutex before relaxing this, taking
// a single read lock across both lookups in LoadPolicyFromProto so the two
// registries cannot be observed disagreeing.
func RegisterPolicyDriver(policyType string, driver PolicyDriver) error {
	if _, ok := policyRegistry[policyType]; ok {
		return fmt.Errorf("%w: %s", ErrPolicyTypeAlreadyRegistered, policyType)
	}

	// Resolved before anything is written, so a rejected proto claim does not
	// leave the driver half-registered.
	protoDriver, hasProto := driver.(ProtoPolicyDriver)
	var protoName protoreflect.FullName
	if hasProto {
		msg := protoDriver.PolicyProto()
		if msg == nil {
			return fmt.Errorf("%w: %s returned a nil proto", ErrPolicyProtoInvalid, policyType)
		}
		protoName = msg.ProtoReflect().Descriptor().FullName()
		if existing, dup := policyProtoRegistry[protoName]; dup {
			return fmt.Errorf("%w: %s is already routed to policy type %s", ErrPolicyProtoAlreadyRegistered, protoName, existing)
		}
	}

	policyRegistry[policyType] = driver
	if hasProto {
		policyProtoRegistry[protoName] = policyType
	}
	return nil
}

// PolicyTypeForProto returns the policy type registered for a policy proto's
// fully qualified message name, and whether one was found.
func PolicyTypeForProto(name protoreflect.FullName) (string, bool) {
	policyType, ok := policyProtoRegistry[name]
	return policyType, ok
}

// LoadPolicy will attempt to load a policy given a policy type and raw policy config.
// It checks the registry for a PolicyDriver for the given policyType and attempts to
// load a Policy object using the raw policy config provided.
//
// The "type" routing envelope key is removed before the driver sees the config:
// it selects the driver in the registry and is stripped so component policies
// (which use strict struct unmarshaling) do not reject it as an unmapped field.
func LoadPolicy(policyType string, rawPolicy map[string]any) (Policy, error) {
	driver, ok := policyRegistry[policyType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPolicyTypeNotFound, policyType)
	}
	p, err := driver.LoadPolicy(stripTypeKey(rawPolicy))
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrPolicyFailedToLoad, policyType, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("%w for policy %s: %w", ErrPolicyFailedValidation, p.PolicyName(), err)
	}
	return p, nil
}

// LoadPolicyFromProto loads a policy that arrived as a proto, routing it to a
// driver by the message's identity rather than by a "type" string in its body.
//
// This is the sibling of LoadPolicy for the xDS path, and enforces the same
// contract: the driver builds the policy and the result is validated before any
// caller sees it. What it does not do is serialize anything. The message is
// handed to the driver as-is, which is both faster and the reason a policy
// proto is free to define a field named "type" without colliding with the
// routing envelope that authored config relies on.
func LoadPolicyFromProto(msg proto.Message) (Policy, error) {
	protoName := msg.ProtoReflect().Descriptor().FullName()

	policyType, ok := PolicyTypeForProto(protoName)
	if !ok {
		// Either the control plane sent a policy this collector was not built
		// with, or a driver exists but never declared this proto. The message
		// name is what tells those apart, so it is always reported.
		return nil, fmt.Errorf("%w: no driver is registered for proto %q", ErrPolicyTypeNotFound, protoName)
	}

	// A type present in policyProtoRegistry was put there by
	// RegisterPolicyDriver off the back of this same assertion, so this only
	// fails if the two registries have drifted.
	driver, ok := policyRegistry[policyType].(ProtoPolicyDriver)
	if !ok {
		return nil, fmt.Errorf("%w: %s is routed from proto %q but does not load from one", ErrPolicyTypeNotFound, policyType, protoName)
	}

	p, err := driver.LoadPolicyProto(msg)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrPolicyFailedToLoad, policyType, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("%w for policy %s: %w", ErrPolicyFailedValidation, p.PolicyName(), err)
	}
	return p, nil
}

// stripTypeKey returns a copy of raw without the "type" routing envelope key,
// leaving the caller's map untouched.
func stripTypeKey(raw map[string]any) map[string]any {
	stripped := make(map[string]any, len(raw))
	for k, v := range raw {
		if k == "type" {
			continue
		}
		stripped[k] = v
	}
	return stripped
}

// ActivePolicySet returns a copy of the currently active policy set, or nil if none is set.
// It returns a copy so that it can't be modified without guaranteeing a write lock is taken.
func ActivePolicySet() *PolicySet {
	policySetMu.RLock()
	defer policySetMu.RUnlock()
	if activePolicySet == nil {
		return nil
	}
	return activePolicySet.Clone()
}

// TakeActiveFailedPolicies atomically claims and clears any FailedPolicies on
// the currently active PolicySet so that per-policy load errors in a revision
// are reported at most once across the provider and processors.
func TakeActiveFailedPolicies() (string, []FailedPolicy) {
	policySetMu.Lock()
	defer policySetMu.Unlock()
	if activePolicySet == nil || len(activePolicySet.FailedPolicies) == 0 {
		return "", nil
	}
	failed := activePolicySet.FailedPolicies
	activePolicySet.FailedPolicies = nil
	return activePolicySet.RevisionID, failed
}

// ActivePolicySetRevisionID returns the revision ID of the active policy set.
// Sometimes the only thing we need to know about the active policy set is the
// revision ID, so rather than forcing callers to get a reference to the entire
// set, they can just get a string.
func ActivePolicySetRevisionID() string {
	policySetMu.RLock()
	defer policySetMu.RUnlock()
	if activePolicySet == nil {
		return ""
	}
	return activePolicySet.RevisionID
}

// PreviousPolicySets returns a shallow copy of past policy sets, ordered newest
// to oldest. At most maxPreviousPolicySets are retained, so an older revision
// may have already been dropped.
func PreviousPolicySets() []*PolicySet {
	policySetMu.RLock()
	defer policySetMu.RUnlock()
	return slices.Clone(previousPolicySets)
}

// RegisterWatcherChannel registers and returns a new channel that will receive
// a signal whenever the active policy set changes (either updated or rolled back).
func RegisterWatcherChannel() WatcherChannel {
	policySetMu.Lock()
	defer policySetMu.Unlock()

	ch := make(chan struct{}, 1)
	watcherChannels = append(watcherChannels, ch)
	return ch
}

// UnregisterWatcherChannel removes a previously registered WatcherChannel.
func UnregisterWatcherChannel(ch WatcherChannel) {
	policySetMu.Lock()
	defer policySetMu.Unlock()

	for i, c := range watcherChannels {
		if WatcherChannel(c) == ch {
			watcherChannels = slices.Delete(watcherChannels, i, i+1)
			return
		}
	}
}

func notifyWatchers() {
	for _, ch := range watcherChannels {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// SetActivePolicySet will set a new active policy set, moving the current
// active policy set to the previous sets and dropping the oldest of those once
// maxPreviousPolicySets is exceeded.
func SetActivePolicySet(policySet *PolicySet) {
	policySetMu.Lock()
	defer policySetMu.Unlock()

	if policySet == nil {
		if activePolicySet != nil {
			activePolicySet = nil
			notifyWatchers()
		}
		return
	}

	if activePolicySet != nil && activePolicySet.RevisionID == policySet.RevisionID {
		return
	}
	if activePolicySet != nil {
		previousPolicySets = slices.Insert(previousPolicySets, 0, activePolicySet)
		// Trim from the tail, dropping the oldest. slices.Delete clears the
		// vacated elements, which matters here: re-slicing alone would leave
		// the dropped sets reachable from the backing array and defeat the
		// bound entirely.
		if len(previousPolicySets) > maxPreviousPolicySets {
			previousPolicySets = slices.Delete(previousPolicySets, maxPreviousPolicySets, len(previousPolicySets))
		}
	}
	activePolicySet = policySet
	notifyWatchers()
}

// RollbackActivePolicySet will take the last policy set from the previous known
// policy sets and set it as active (removing it from history).
func RollbackActivePolicySet() {
	policySetMu.Lock()
	defer policySetMu.Unlock()

	if len(previousPolicySets) == 0 {
		// If there are no policy sets to rollback to, go to
		// a no active policy set state.
		if activePolicySet == nil {
			return
		}
		activePolicySet = nil
		notifyWatchers()
		return
	}

	activePolicySet = previousPolicySets[0]
	previousPolicySets = slices.Delete(previousPolicySets, 0, 1)
	notifyWatchers()
}
