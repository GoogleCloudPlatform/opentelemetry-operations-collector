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
)

var (
	ErrPolicyTypeAlreadyRegistered = errors.New("policy type already registered")
	ErrPolicyTypeNotFound          = errors.New("no driver found for policy type")
	ErrPolicyFailedToLoad          = errors.New("failed to load policy")
	ErrPolicyFailedValidation      = errors.New("policy validation failed")
)

// policyRegistry is a central map that tracks all supported policies registered by any components.
var policyRegistry = map[string]PolicyDriver{}

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
func RegisterPolicyDriver(policyType string, driver PolicyDriver) error {
	if _, ok := policyRegistry[policyType]; ok {
		return fmt.Errorf("%w: %s", ErrPolicyTypeAlreadyRegistered, policyType)
	}
	policyRegistry[policyType] = driver
	return nil
}

// LoadPolicy will attempt to load a policy given a policy type and raw policy config.
// It checks the registry for a PolicyDriver for the given policyType and attempts to
// load a Policy object using the raw policy config provided.
func LoadPolicy(policyType string, rawPolicy map[string]any) (Policy, error) {
	driver, ok := policyRegistry[policyType]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPolicyTypeNotFound, policyType)
	}
	p, err := driver.LoadPolicy(rawPolicy)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrPolicyFailedToLoad, policyType, err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("%w for policy %s: %w", ErrPolicyFailedValidation, p.PolicyName(), err)
	}
	return p, nil
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

// ActivePolicySetRevisionID returns the revision ID of the active policy set.
// Sometimes the only thing we need to know about the active policy set is the
// revision ID, so rather than forcing callers to get a reference to the entire
// set, they can just get a string.
func ActivePolicySetRevisionID() string {
	policySetMu.RLock()
	defer policySetMu.RUnlock()
	return activePolicySet.RevisionID
}

// PreviousPolicySets returns a shallow copy of past policy sets, ordered newest to oldest.
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
// active policy set to the previous sets.
func SetActivePolicySet(policySet *PolicySet) {
	if policySet == nil {
		return
	}

	policySetMu.Lock()
	defer policySetMu.Unlock()

	if activePolicySet != nil && activePolicySet.RevisionID == policySet.RevisionID {
		return
	}
	if activePolicySet != nil {
		previousPolicySets = slices.Insert(previousPolicySets, 0, activePolicySet)
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
