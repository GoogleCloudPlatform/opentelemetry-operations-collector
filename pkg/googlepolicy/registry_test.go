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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetState(t *testing.T) {
	t.Helper()
	policySetMu.Lock()
	defer policySetMu.Unlock()
	activePolicySet = nil
	previousPolicySets = []*PolicySet{}
	watcherChannels = []chan struct{}{}
}

func TestActivePolicySet_InitialState(t *testing.T) {
	resetState(t)

	assert.Nil(t, ActivePolicySet())
	assert.Empty(t, PreviousPolicySets())
}

func TestSetActivePolicySet_NilHandling(t *testing.T) {
	resetState(t)

	SetActivePolicySet(nil)
	assert.Nil(t, ActivePolicySet())
	assert.Empty(t, PreviousPolicySets())
}

func TestSetActivePolicySet_TransitionsAndRollback(t *testing.T) {
	resetState(t)

	ps1 := &PolicySet{RevisionID: "rev-1", ReceivedAt: time.Now()}
	ps2 := &PolicySet{RevisionID: "rev-2", ReceivedAt: time.Now()}
	ps3 := &PolicySet{RevisionID: "rev-3", ReceivedAt: time.Now()}

	// 1. Set ps1
	SetActivePolicySet(ps1)
	assert.Equal(t, ps1, ActivePolicySet())
	assert.Empty(t, PreviousPolicySets())

	// 2. Duplicate revision is ignored
	ps1Dup := &PolicySet{RevisionID: "rev-1", ReceivedAt: time.Now()}
	SetActivePolicySet(ps1Dup)
	assert.Equal(t, ps1, ActivePolicySet())
	assert.Empty(t, PreviousPolicySets())

	// 3. Set ps2 -> ps1 archived
	SetActivePolicySet(ps2)
	assert.Equal(t, ps2, ActivePolicySet())
	prev := PreviousPolicySets()
	require.Len(t, prev, 1)
	assert.Equal(t, ps1, prev[0])

	// 4. Set ps3 -> ps2 archived at index 0, ps1 at index 1
	SetActivePolicySet(ps3)
	assert.Equal(t, ps3, ActivePolicySet())
	prev = PreviousPolicySets()
	require.Len(t, prev, 2)
	assert.Equal(t, ps2, prev[0])
	assert.Equal(t, ps1, prev[1])

	// 5. Rollback to ps2
	RollbackActivePolicySet()
	assert.Equal(t, ps2, ActivePolicySet())
	prev = PreviousPolicySets()
	require.Len(t, prev, 1)
	assert.Equal(t, ps1, prev[0])

	// 6. Rollback to ps1
	RollbackActivePolicySet()
	assert.Equal(t, ps1, ActivePolicySet())
	assert.Empty(t, PreviousPolicySets())

	// 7. Rollback with empty history -> active becomes nil
	RollbackActivePolicySet()
	assert.Nil(t, ActivePolicySet())
	assert.Empty(t, PreviousPolicySets())

	// 8. Rollback again stays nil
	RollbackActivePolicySet()
	assert.Nil(t, ActivePolicySet())
	assert.Empty(t, PreviousPolicySets())
}

func TestPolicySet_PreviousPolicySets_Isolation(t *testing.T) {
	resetState(t)

	ps1 := &PolicySet{RevisionID: "rev-1", ReceivedAt: time.Now()}
	ps2 := &PolicySet{RevisionID: "rev-2", ReceivedAt: time.Now()}

	SetActivePolicySet(ps1)
	SetActivePolicySet(ps2)

	prev := PreviousPolicySets()
	require.Len(t, prev, 1)

	// Mutating the returned slice must not affect internal state
	prev[0] = nil
	assert.NotNil(t, PreviousPolicySets()[0])
}

func TestActivePolicySet_Isolation(t *testing.T) {
	resetState(t)

	ps := &PolicySet{
		RevisionID: "rev-1",
		ReceivedAt: time.Now(),
		Policies: map[string]*PolicySetEntry{
			"policy-1": {
				Processed: false,
			},
		},
	}
	SetActivePolicySet(ps)

	active := ActivePolicySet()
	require.NotNil(t, active)
	assert.False(t, active.Policies["policy-1"].Processed)

	// Mutating the returned copy must not affect internal state
	active.RevisionID = "mutated-rev"
	active.Policies["policy-1"].Processed = true
	active.Policies["new-policy"] = &PolicySetEntry{}

	fresh := ActivePolicySet()
	assert.Equal(t, "rev-1", fresh.RevisionID)
	assert.False(t, fresh.Policies["policy-1"].Processed)
	assert.NotContains(t, fresh.Policies, "new-policy")
}

func TestPolicySet_ConcurrentAccess_RaceDetector(t *testing.T) {
	resetState(t)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Concurrently read ActivePolicySet and PreviousPolicySets
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := RegisterWatcherChannel()
			defer UnregisterWatcherChannel(ch)

			for {
				select {
				case <-stop:
					return
				case <-ch:
					_ = ActivePolicySet()
				default:
					_ = ActivePolicySet()
					prev := PreviousPolicySets()
					for _, p := range prev {
						if p != nil {
							_ = p.RevisionID
						}
					}
				}
			}
		}()
	}

	// Concurrently write updates and rollbacks
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ps := &PolicySet{
					RevisionID: fmt.Sprintf("rev-%d-%d", workerID, j),
					ReceivedAt: time.Now(),
				}
				SetActivePolicySet(ps)
				if j%3 == 0 {
					RollbackActivePolicySet()
				}
			}
		}(i)
	}

	// Allow writers to complete
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestWatcherChannel_ReceivesSignalOnSetActiveAndRollback(t *testing.T) {
	resetState(t)

	watcher := RegisterWatcherChannel()
	defer UnregisterWatcherChannel(watcher)

	// 1. Initial SetActive triggers signal
	ps1 := &PolicySet{RevisionID: "rev-1", ReceivedAt: time.Now()}
	SetActivePolicySet(ps1)

	select {
	case <-watcher:
		assert.Equal(t, ps1, ActivePolicySet())
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for watcher signal on SetActivePolicySet")
	}

	// 2. Duplicate revision does NOT trigger signal
	ps1Dup := &PolicySet{RevisionID: "rev-1", ReceivedAt: time.Now()}
	SetActivePolicySet(ps1Dup)

	select {
	case <-watcher:
		t.Fatal("unexpected watcher signal on duplicate revision")
	default:
		// Expected: no signal
	}

	// 3. Rollback triggers signal
	ps2 := &PolicySet{RevisionID: "rev-2", ReceivedAt: time.Now()}
	SetActivePolicySet(ps2)
	<-watcher // drain signal from ps2

	RollbackActivePolicySet()
	select {
	case <-watcher:
		assert.Equal(t, ps1, ActivePolicySet())
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for watcher signal on RollbackActivePolicySet")
	}

	// 4. Rollback to nil triggers signal
	RollbackActivePolicySet()
	select {
	case <-watcher:
		assert.Nil(t, ActivePolicySet())
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for watcher signal on rollback to nil")
	}

	// 5. Rollback when already nil does NOT trigger signal
	RollbackActivePolicySet()
	select {
	case <-watcher:
		t.Fatal("unexpected watcher signal on no-op rollback")
	default:
		// Expected: no signal
	}
}

func TestWatcherChannel_Unregister(t *testing.T) {
	resetState(t)

	watcher := RegisterWatcherChannel()
	UnregisterWatcherChannel(watcher)

	ps := &PolicySet{RevisionID: "rev-1", ReceivedAt: time.Now()}
	SetActivePolicySet(ps)

	select {
	case <-watcher:
		t.Fatal("unregistered watcher received a signal")
	default:
		// Expected: no signal
	}
}

func TestWatcherChannel_NonBlockingWhenFull(t *testing.T) {
	resetState(t)

	watcher := RegisterWatcherChannel()
	defer UnregisterWatcherChannel(watcher)

	// Send multiple updates without consuming from the channel
	for i := 0; i < 5; i++ {
		SetActivePolicySet(&PolicySet{
			RevisionID: fmt.Sprintf("rev-%d", i),
			ReceivedAt: time.Now(),
		})
	}

	// Channel buffer of 1 holds exactly one signal; subsequent calls do not block
	select {
	case <-watcher:
		// Expected: signal received
	default:
		t.Fatal("expected pending signal on watcher channel")
	}
}
