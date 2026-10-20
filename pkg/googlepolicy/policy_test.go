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
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
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
		PolicySetID: "projects/my-proj/locations/us-central1/policySets/ps-1",
		RevisionID:  "rev-1",
		ReceivedAt:  time.Now(),
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
	assert.Equal(t, ps.PolicySetID, cloned.PolicySetID)
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
	assert.Equal(t, "", ps.PolicySetID, "plain local policy name does not have a control-plane policySet prefix")

	SetActivePolicySet(ps)
	assert.Equal(t, "rev-make", ActivePolicySetRevisionID())
	SetActivePolicySet(nil)

	// Control-plane resource name derives PolicySetID
	ps, err = MakePolicySet("rev-cp", []map[string]any{
		{"type": "driver_test_type", "name": "projects/my-proj/locations/us-central1/policySets/my-set/policies/pol-1"},
		{"type": "driver_test_type", "name": "projects/my-proj/locations/us-central1/policySets/my-set/policies/pol-2"},
	})
	require.NoError(t, err)
	require.NotNil(t, ps)
	assert.Equal(t, "projects/my-proj/locations/us-central1/policySets/my-set", ps.PolicySetID)

	// Failed policies with control-plane resource names still populate PolicySetID
	ps, err = MakePolicySet("rev-cp-failed", []map[string]any{
		{"type": "unknown_type_xyz", "id": "projects/my-proj/locations/us-central1/policySets/failed-set/policies/bad-pol"},
	})
	assert.Error(t, err)
	require.NotNil(t, ps)
	assert.Equal(t, "projects/my-proj/locations/us-central1/policySets/failed-set", ps.PolicySetID)

	// Conflicting policySet prefixes across policies yield empty PolicySetID
	ps, err = MakePolicySet("rev-cp-conflict", []map[string]any{
		{"type": "driver_test_type", "name": "projects/my-proj/locations/us-central1/policySets/set-a/policies/pol-1"},
		{"type": "driver_test_type", "name": "projects/my-proj/locations/us-central1/policySets/set-b/policies/pol-2"},
	})
	require.NoError(t, err)
	require.NotNil(t, ps)
	assert.Equal(t, "", ps.PolicySetID)

	// Malformed policy resource suffixes (empty policy ID or extra path segments) are ignored
	ps, err = MakePolicySet("rev-cp-malformed", []map[string]any{
		{"type": "driver_test_type", "name": "projects/my-proj/locations/us-central1/policySets/set-a/policies/"},
		{"type": "driver_test_type", "name": "projects/my-proj/locations/us-central1/policySets/set-a/policies/pol-1/extra"},
	})
	require.NoError(t, err)
	require.NotNil(t, ps)
	assert.Equal(t, "", ps.PolicySetID)

	// Validation failure
	ps, err = MakePolicySet("rev-val-err", []map[string]any{
		{"type": "driver_test_type", "name": "bad-pol", "fail_val": true},
	})
	assert.ErrorIs(t, err, ErrPolicyFailedValidation)
	assert.NotErrorIs(t, err, ErrPolicyFailedToLoad)
	assert.Equal(t, "policy_failed_validation", ClassifyPolicyError(err))
	require.Len(t, ps.FailedPolicies, 1)
	assert.Equal(t, "index_0", ps.FailedPolicies[0].ID)
	assert.Equal(t, PolicyClassTransformation, ps.FailedPolicies[0].Class)

	// Load failure
	ps, err = MakePolicySet("rev-load-err", []map[string]any{
		{"type": "driver_test_type", "id": "load-err-pol", "name": []int{1, 2}},
	})
	assert.ErrorIs(t, err, ErrPolicyFailedToLoad)
	assert.NotErrorIs(t, err, ErrPolicyFailedValidation)
	assert.Equal(t, "policy_failed_to_load", ClassifyPolicyError(err))
	require.Len(t, ps.FailedPolicies, 1)
	assert.Equal(t, "load-err-pol", ps.FailedPolicies[0].ID)
	assert.Equal(t, PolicyClassTransformation, ps.FailedPolicies[0].Class)

	// Missing type field
	ps, err = MakePolicySet("rev-err1", []map[string]any{
		{"name": "no-type"},
	})
	assert.ErrorIs(t, err, ErrPolicyTypeFieldMissing)
	require.Len(t, ps.FailedPolicies, 1)
	assert.Equal(t, PolicyClass(""), ps.FailedPolicies[0].Class)

	// Wrong type field type
	ps, err = MakePolicySet("rev-err2", []map[string]any{
		{"type": 12345},
	})
	assert.ErrorIs(t, err, ErrPolicyTypeFieldWrongType)
	require.Len(t, ps.FailedPolicies, 1)
	assert.Equal(t, PolicyClass(""), ps.FailedPolicies[0].Class)

	// Unknown policy type -> returns joined error with empty class
	ps, err = MakePolicySet("rev-err3", []map[string]any{
		{"type": "unknown_type_xyz", "name": "test"},
	})
	assert.ErrorIs(t, err, ErrPolicyTypeNotFound)
	assert.Equal(t, "policy_type_not_found", ClassifyPolicyError(err))
	require.Len(t, ps.FailedPolicies, 1)
	assert.Equal(t, PolicyClass(""), ps.FailedPolicies[0].Class)

	// Partial success: the unusable entries are reported and skipped, but the
	// policies that did load are still returned.
	ps, err = MakePolicySet("rev-partial", []map[string]any{
		{"name": "no-type"},
		{"type": "driver_test_type", "name": "good-pol"},
		{"type": "driver_test_type", "name": "bad-pol", "fail_val": true},
	})
	assert.ErrorIs(t, err, ErrPolicyTypeFieldMissing)
	assert.ErrorIs(t, err, ErrPolicyFailedValidation)
	require.NotNil(t, ps)
	assert.Contains(t, ps.Policies, "good-pol")
	assert.Len(t, ps.Policies, 1, "only the usable policy is kept")
	require.Len(t, ps.FailedPolicies, 2)
	assert.Equal(t, PolicyClass(""), ps.FailedPolicies[0].Class)
	assert.Equal(t, PolicyClassTransformation, ps.FailedPolicies[1].Class)

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

func TestParsePolicySetRevision(t *testing.T) {
	testCases := []struct {
		name          string
		input         string
		wantPolicySet string
		wantRevision  string
		wantOK        bool
	}{
		{
			name:          "valid canonical name",
			input:         "projects/my-project/locations/global/policySets/my-ps/revisions/r1",
			wantPolicySet: "projects/my-project/locations/global/policySets/my-ps",
			wantRevision:  "r1",
			wantOK:        true,
		},
		{
			name:          "valid with numeric project and regional location",
			input:         "projects/507934445817/locations/us-central1/policySets/fleet-ps/revisions/rev-42",
			wantPolicySet: "projects/507934445817/locations/us-central1/policySets/fleet-ps",
			wantRevision:  "rev-42",
			wantOK:        true,
		},
		{
			name:   "missing revisions segment",
			input:  "projects/my-project/locations/global/policySets/my-ps",
			wantOK: false,
		},
		{
			name:   "empty revision ID",
			input:  "projects/my-project/locations/global/policySets/my-ps/revisions/",
			wantOK: false,
		},
		{
			name:   "revision ID contains slash",
			input:  "projects/my-project/locations/global/policySets/my-ps/revisions/r1/extra",
			wantOK: false,
		},
		{
			name:   "malformed prefix before revisions",
			input:  "invalid/prefix/policySets/my-ps/revisions/r1",
			wantOK: false,
		},
		{
			name:   "completely empty string",
			input:  "",
			wantOK: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			psID, revID, ok := ParsePolicySetRevision(tc.input)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantPolicySet, psID)
			assert.Equal(t, tc.wantRevision, revID)
		})
	}
}

func TestPolicySet_CloneWithRevisionCreateTime(t *testing.T) {
	now := time.Now()
	createTime := now.Add(-10 * time.Minute)

	ps := &PolicySet{
		PolicySetID:        "projects/p/locations/l/policySets/ps",
		RevisionID:         "rev-1",
		RevisionCreateTime: createTime,
		ReceivedAt:         now,
		Policies: map[string]*PolicySetEntry{
			"pol-1": {
				PolicyObj: &mockOtherPolicy{name: "pol-1", class: PolicyClassDestination},
				Processed: true,
			},
		},
	}

	clone := ps.Clone()
	require.NotNil(t, clone)
	assert.Equal(t, ps.PolicySetID, clone.PolicySetID)
	assert.Equal(t, ps.RevisionID, clone.RevisionID)
	assert.Equal(t, ps.RevisionCreateTime, clone.RevisionCreateTime)
	assert.Equal(t, ps.ReceivedAt, clone.ReceivedAt)
	require.Contains(t, clone.Policies, "pol-1")
	assert.Equal(t, ps.Policies["pol-1"].PolicyObj.PolicyName(), clone.Policies["pol-1"].PolicyObj.PolicyName())
}

func TestProtoDriver_ValidationClassificationAndPolicyClass(t *testing.T) {
	errMissingID := errors.New("id cannot be empty")
	errMissingAction := errors.New("action must be specified")

	d := ProtoDriver[*policyv1alpha1.LogFilterPolicy]{
		New: func(pb *policyv1alpha1.LogFilterPolicy) (Policy, error) {
			if pb == nil || pb.GetId() == "" {
				return nil, errMissingID
			}
			if pb.GetAction() == policyv1alpha1.Action_ACTION_UNSPECIFIED {
				return nil, errMissingAction
			}
			return &mockTransformationPolicy{name: pb.GetId(), signals: []Signal{SignalLogs}}, nil
		},
	}

	protoName := (&policyv1alpha1.LogFilterPolicy{}).ProtoReflect().Descriptor().FullName()
	unregisterForTest(t, "test_proto_log_filter", protoName)
	require.NoError(t, RegisterPolicyDriver("test_proto_log_filter", d))

	assert.Equal(t, PolicyClassTransformation, d.PolicyClass(), "ProtoDriver defaults to PolicyClassTransformation when Class is unset")
	assert.Equal(t, PolicyClassSource, (ProtoDriver[*policyv1alpha1.LogFilterPolicy]{Class: PolicyClassSource}).PolicyClass())

	t.Run("LoadPolicyFromProto semantic validation error wraps ErrPolicyFailedValidation", func(t *testing.T) {
		_, err := LoadPolicyFromProto(&policyv1alpha1.LogFilterPolicy{Id: "bad-filter"})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrPolicyFailedValidation)
		assert.ErrorIs(t, err, errMissingAction)
		assert.NotErrorIs(t, err, ErrPolicyFailedToLoad)
		assert.Equal(t, "policy_failed_validation", ClassifyPolicyError(err))
	})

	t.Run("LoadPolicy semantic validation error wraps ErrPolicyFailedValidation", func(t *testing.T) {
		_, err := LoadPolicy("test_proto_log_filter", map[string]any{
			"type": "test_proto_log_filter",
			"id":   "bad-filter",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrPolicyFailedValidation)
		assert.ErrorIs(t, err, errMissingAction)
		assert.NotErrorIs(t, err, ErrPolicyFailedToLoad)
		assert.Equal(t, "policy_failed_validation", ClassifyPolicyError(err))
	})

	t.Run("LoadPolicy protojson unmarshal error wraps ErrPolicyFailedToLoad", func(t *testing.T) {
		_, err := LoadPolicy("test_proto_log_filter", map[string]any{
			"type":    "test_proto_log_filter",
			"id":      "bad-type",
			"matches": "not-an-array",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrPolicyFailedToLoad)
		assert.NotErrorIs(t, err, ErrPolicyFailedValidation)
		assert.Equal(t, "policy_failed_to_load", ClassifyPolicyError(err))
	})

	t.Run("LoadPolicyProto mismatched proto type wraps ErrPolicyProtoMismatch", func(t *testing.T) {
		_, err := d.LoadPolicyProto(&policyv1alpha1.MetricFilterPolicy{Id: "wrong-proto"})
		require.ErrorIs(t, err, ErrPolicyProtoMismatch)
		assert.NotErrorIs(t, err, ErrPolicyFailedValidation)
	})

	t.Run("LoadPolicyFromProto nil message wraps ErrPolicyFailedToLoad", func(t *testing.T) {
		_, err := LoadPolicyFromProto(nil)
		require.ErrorIs(t, err, ErrPolicyFailedToLoad)
		assert.Equal(t, "policy_failed_to_load", ClassifyPolicyError(err))
	})

	t.Run("MakePolicySetFromProtos populates FailedPolicy.Class and error classification", func(t *testing.T) {
		ps, err := MakePolicySetFromProtos("projects/p/locations/l/policySets/ps", "rev-1", []proto.Message{
			&policyv1alpha1.LogFilterPolicy{Id: "bad-filter"},
			&policyv1alpha1.MetricFilterPolicy{Id: "unregistered-metric"},
			&policyv1alpha1.LogFilterPolicy{
				Id:     "good-filter",
				Action: policyv1alpha1.Action_ACTION_DROP.Enum(),
			},
		})
		require.Error(t, err)
		require.NotNil(t, ps)
		assert.Contains(t, ps.Policies, "good-filter")
		require.Len(t, ps.FailedPolicies, 2)

		assert.Equal(t, "bad-filter", ps.FailedPolicies[0].ID)
		assert.Equal(t, PolicyClassTransformation, ps.FailedPolicies[0].Class)
		assert.ErrorIs(t, ps.FailedPolicies[0].Err, ErrPolicyFailedValidation)
		assert.NotErrorIs(t, ps.FailedPolicies[0].Err, ErrPolicyFailedToLoad)
		assert.Equal(t, "policy_failed_validation", ClassifyPolicyError(ps.FailedPolicies[0].Err))

		assert.Equal(t, "unregistered-metric", ps.FailedPolicies[1].ID)
		assert.Equal(t, PolicyClass(""), ps.FailedPolicies[1].Class)
		assert.ErrorIs(t, ps.FailedPolicies[1].Err, ErrPolicyTypeNotFound)
		assert.Equal(t, "policy_type_not_found", ClassifyPolicyError(ps.FailedPolicies[1].Err))
	})

	t.Run("MakePolicySet populates FailedPolicy.Class for ProtoDriver load and validation errors", func(t *testing.T) {
		ps, err := MakePolicySet("rev-raw-1", []map[string]any{
			{"type": "test_proto_log_filter", "id": "val-err"},
			{"type": "test_proto_log_filter", "id": "load-err", "matches": "not-an-array"},
		})
		require.Error(t, err)
		require.NotNil(t, ps)
		require.Len(t, ps.FailedPolicies, 2)

		assert.Equal(t, "val-err", ps.FailedPolicies[0].ID)
		assert.Equal(t, PolicyClassTransformation, ps.FailedPolicies[0].Class)
		assert.Equal(t, "policy_failed_validation", ClassifyPolicyError(ps.FailedPolicies[0].Err))

		assert.Equal(t, "load-err", ps.FailedPolicies[1].ID)
		assert.Equal(t, PolicyClassTransformation, ps.FailedPolicies[1].Class)
		assert.Equal(t, "policy_failed_to_load", ClassifyPolicyError(ps.FailedPolicies[1].Err))
	})
}
