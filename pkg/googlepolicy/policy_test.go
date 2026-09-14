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
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

func TestPolicySet_SortingDeterminism(t *testing.T) {
	pZ := &mockTransformationPolicy{name: "z-policy", signals: []Signal{SignalLogs}}
	pA := &mockTransformationPolicy{name: "a-policy", signals: []Signal{SignalLogs}}
	pM := &mockTransformationPolicy{name: "m-policy", signals: []Signal{SignalLogs}}

	ps := &PolicySet{
		RevisionID: "rev-sort",
		Policies: map[string]*PolicySetEntry{
			"z-policy": {PolicyObj: pZ},
			"a-policy": {PolicyObj: pA},
			"m-policy": {PolicyObj: pM},
		},
	}

	trans := ps.TransformationPolicies()
	require.Len(t, trans, 3)
	assert.Equal(t, "a-policy", trans[0].PolicyName())
	assert.Equal(t, "m-policy", trans[1].PolicyName())
	assert.Equal(t, "z-policy", trans[2].PolicyName())

	byClass := ps.LoadPoliciesOfClass(PolicyClassTransformation)
	require.Len(t, byClass, 3)
	assert.Equal(t, "a-policy", byClass[0].PolicyName())
	assert.Equal(t, "m-policy", byClass[1].PolicyName())
	assert.Equal(t, "z-policy", byClass[2].PolicyName())
}

func TestPolicySet_MarkAndClone(t *testing.T) {
	var nilEntry *PolicySetEntry
	assert.Nil(t, nilEntry.Clone())

	var nilSet *PolicySet
	assert.Nil(t, nilSet.Clone())

	p1 := &mockTransformationPolicy{name: "p1", signals: []Signal{SignalLogs}}
	p2 := &mockTransformationPolicy{name: "p2", signals: []Signal{SignalMetrics}}
	unregistered := &mockTransformationPolicy{name: "unregistered"}

	ps := &PolicySet{
		RevisionID: "rev-1",
		ReceivedAt: time.Now(),
		Policies: map[string]*PolicySetEntry{
			"p1": {PolicyObj: p1},
			"p2": {PolicyObj: p2},
		},
	}

	ps.MarkPolicySuccesful(p1)
	assert.True(t, ps.Policies["p1"].Processed)
	assert.NoError(t, ps.Policies["p1"].Error)

	ps.MarkPolicyFailed(p2, assert.AnError)
	assert.True(t, ps.Policies["p2"].Processed)
	assert.ErrorIs(t, ps.Policies["p2"].Error, assert.AnError)

	assert.Panics(t, func() {
		ps.MarkPolicySuccesful(unregistered)
	})

	cloned := ps.Clone()
	require.NotNil(t, cloned)
	assert.Equal(t, ps.RevisionID, cloned.RevisionID)
	require.Len(t, cloned.Policies, 2)
	assert.True(t, cloned.Policies["p1"].Processed)
}

type driverTestPolicy struct {
	Name        string `mapstructure:"name"`
	Type        string `mapstructure:"type"`
	FailVal     bool   `mapstructure:"fail_val"`
	FailLoadErr bool   `mapstructure:"-"`
}

func (d *driverTestPolicy) PolicyName() string       { return d.Name }
func (d *driverTestPolicy) PolicyType() string       { return d.Type }
func (d *driverTestPolicy) PolicyClass() PolicyClass { return PolicyClassTransformation }
func (d *driverTestPolicy) Validate() error {
	if d.FailVal {
		return assert.AnError
	}
	return nil
}

func TestMakePolicySetAndGenericDriver(t *testing.T) {
	driver := &GenericDriver[*driverTestPolicy]{}
	RegisterPolicyDriver("driver_test_type", driver)

	SetActivePolicySet(nil)
	assert.Equal(t, "", ActivePolicySetRevisionID())

	// Valid policy
	ps, err := MakePolicySet("rev-make", []map[string]any{
		{"type": "driver_test_type", "name": "test-pol-1"},
	})
	require.NoError(t, err)
	require.NotNil(t, ps)
	assert.Contains(t, ps.Policies, "test-pol-1")

	SetActivePolicySet(ps)
	assert.Equal(t, "rev-make", ActivePolicySetRevisionID())
	SetActivePolicySet(nil)

	// Validation failure
	_, err = MakePolicySet("rev-val-err", []map[string]any{
		{"type": "driver_test_type", "name": "bad-pol", "fail_val": true},
	})
	assert.ErrorIs(t, err, ErrPolicyFailedValidation)

	// Load failure
	_, err = LoadPolicy("driver_test_type", map[string]any{
		"name": []int{1, 2},
	})
	assert.ErrorIs(t, err, ErrPolicyFailedToLoad)

	// Missing type field
	_, err = MakePolicySet("rev-err1", []map[string]any{
		{"name": "no-type"},
	})
	assert.ErrorIs(t, err, ErrPolicyTypeFieldMissing)

	// Wrong type field type
	_, err = MakePolicySet("rev-err2", []map[string]any{
		{"type": 12345},
	})
	assert.ErrorIs(t, err, ErrPolicyTypeFieldWrongType)

	// Unknown policy type -> returns joined error
	_, err = MakePolicySet("rev-err3", []map[string]any{
		{"type": "unknown_type_xyz", "name": "test"},
	})
	assert.Error(t, err)

	// GenericDriver unmarshal error
	_, err = driver.LoadPolicy(map[string]any{
		"name": []int{1, 2, 3}, // invalid type for string field
	})
	assert.Error(t, err)
}

func TestFilePolicyManager(t *testing.T) {
	RegisterPolicyDriver("driver_test_type", &GenericDriver[*driverTestPolicy]{})

	tmpDir := t.TempDir()
	u, err := url.Parse("file://" + tmpDir)
	require.NoError(t, err)

	// Windows path edge case check (/C:/...)
	winURL := &url.URL{Scheme: "file", Path: "/C:/nonexistent_dir_12345"}
	_, err = NewFilePolicyManager(zap.NewNop(), winURL)
	assert.Error(t, err)

	// Non-existent dir
	badURL, _ := url.Parse("file://" + filepath.Join(tmpDir, "does-not-exist"))
	_, err = NewFilePolicyManager(zap.NewNop(), badURL)
	assert.Error(t, err)

	// File instead of dir
	tmpFile := filepath.Join(tmpDir, "not-a-dir.txt")
	require.NoError(t, os.WriteFile(tmpFile, []byte("hello"), 0600))
	fileURL, _ := url.Parse("file://" + tmpFile)
	_, err = NewFilePolicyManager(zap.NewNop(), fileURL)
	assert.Error(t, err)

	// Valid dir with JSON policy file + invalid JSON file + non-object JSON file
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "pol1.json"), []byte(`{"type":"driver_test_type","name":"pol-1"}`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "invalid.json"), []byte(`{bad json`), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "null.json"), []byte(`null`), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755))

	mgr, err := NewFilePolicyManager(zap.NewNop(), u)
	require.NoError(t, err)
	assert.Equal(t, u, mgr.URI())
	mgr.PolicyEvaluationResult("rev", nil)

	require.NoError(t, mgr.Start())
	assert.Error(t, mgr.Start()) // already started

	// Calling loadPolicySet again with no file changes returns ErrPolicySetUnchanged
	fpm := mgr.(*filePolicyManager)
	assert.ErrorIs(t, fpm.loadPolicySet(), ErrPolicySetUnchanged)

	ps := ActivePolicySet()
	require.NotNil(t, ps)
	assert.Contains(t, ps.Policies, "pol-1")

	// Trigger fsnotify write event
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "pol2.json"), []byte(`{"type":"driver_test_type","name":"pol-2"}`), 0600))
	assert.Eventually(t, func() bool {
		cur := ActivePolicySet()
		return cur != nil && cur.Policies["pol-2"] != nil
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, mgr.Stop())
	require.NoError(t, mgr.Stop()) // double stop safe
}
