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

package journaldsource

import (
	"context"
	"errors"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
)

func TestPolicy_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		proto   *policyv1alpha1.JournaldSourcePolicy
		wantErr error
	}{
		{
			name:    "nil proto",
			proto:   nil,
			wantErr: ErrNilProto,
		},
		{
			name: "empty id",
			proto: &policyv1alpha1.JournaldSourcePolicy{
				Id: "",
			},
			wantErr: ErrMissingID,
		},
		{
			name: "valid policy",
			proto: &policyv1alpha1.JournaldSourcePolicy{
				Id: "system-journal",
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pol, err := NewPolicyFromProto(tc.proto)
			if tc.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tc.wantErr), "expected error %v, got %v", tc.wantErr, err)
				assert.Nil(t, pol)
			} else {
				require.NoError(t, err)
				require.NotNil(t, pol)
				assert.Equal(t, tc.proto.GetId(), pol.PolicyName())
				assert.Equal(t, PolicyType, pol.PolicyType())
				assert.Equal(t, googlepolicy.PolicyClassSource, pol.PolicyClass())
			}
		})
	}
}

func TestPolicy_ReceiverIDs(t *testing.T) {
	t.Parallel()

	pol, err := NewPolicyFromProto(&policyv1alpha1.JournaldSourcePolicy{
		Id: "journald-test",
	})
	require.NoError(t, err)

	assert.Equal(t, []component.ID{component.MustNewID(DefaultReceiverID)}, pol.LogReceiverIDs())
	assert.Nil(t, pol.MetricReceiverIDs())
	assert.Nil(t, pol.TraceReceiverIDs())
}

func TestPolicy_Evaluate(t *testing.T) {
	t.Parallel()

	pol, err := NewPolicyFromProto(&policyv1alpha1.JournaldSourcePolicy{
		Id: "journald-sys",
	})
	require.NoError(t, err)

	conf, err := pol.Evaluate(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conf)

	raw := conf.ToStringMap()
	receivers, ok := raw["receivers"].(map[string]any)
	require.True(t, ok, "receivers key must be a map")

	journaldCfg, ok := receivers[DefaultReceiverID].(map[string]any)
	require.True(t, ok, "journald receiver must be present")

	operators, ok := journaldCfg["operators"].([]any)
	require.True(t, ok, "operators key must be a slice")
	require.Len(t, operators, 1)

	op, ok := operators[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "filter", op["type"])
	assert.Equal(t, "exclude_collector_logs", op["id"])
	assert.Equal(t, collectorLogFilterExpr, op["expr"])
}

func TestPolicy_Evaluate_InvalidPolicy(t *testing.T) {
	t.Parallel()

	pol := &Policy{
		proto: &policyv1alpha1.JournaldSourcePolicy{
			Id: "",
		},
	}
	conf, err := pol.Evaluate(context.Background())
	require.Error(t, err)
	assert.Nil(t, conf)
}

func TestDriver_LoadPolicy(t *testing.T) {
	t.Parallel()

	t.Run("load from valid map", func(t *testing.T) {
		t.Parallel()
		p, err := driver.LoadPolicy(map[string]any{
			"id": "my-journald",
		})
		require.NoError(t, err)
		require.NotNil(t, p)
		assert.Equal(t, "my-journald", p.PolicyName())
		assert.Equal(t, PolicyType, p.PolicyType())
	})

	t.Run("load from missing id", func(t *testing.T) {
		t.Parallel()
		p, err := driver.LoadPolicy(map[string]any{})
		require.Error(t, err)
		assert.Nil(t, p)
	})

	t.Run("load from nil map", func(t *testing.T) {
		t.Parallel()
		p, err := driver.LoadPolicy(nil)
		require.Error(t, err)
		assert.Nil(t, p)
	})
}

func TestPolicy_Consolidate(t *testing.T) {
	t.Parallel()

	p1, err := NewPolicyFromProto(&policyv1alpha1.JournaldSourcePolicy{Id: "journald-1"})
	require.NoError(t, err)

	p2, err := NewPolicyFromProto(&policyv1alpha1.JournaldSourcePolicy{Id: "journald-2"})
	require.NoError(t, err)

	t.Run("empty slice returns receiver policy", func(t *testing.T) {
		t.Parallel()
		res, err := p1.Consolidate(nil)
		require.NoError(t, err)
		assert.Equal(t, p1, res)
	})

	t.Run("single policy returns receiver policy", func(t *testing.T) {
		t.Parallel()
		res, err := p1.Consolidate([]googlepolicy.SourcePolicy{p1})
		require.NoError(t, err)
		assert.Equal(t, p1, res)
	})

	t.Run("multiple valid policies consolidate into single policy", func(t *testing.T) {
		t.Parallel()
		res, err := p1.Consolidate([]googlepolicy.SourcePolicy{p1, p2})
		require.NoError(t, err)
		assert.Equal(t, p1, res)
		assert.Equal(t, "journald-1", res.PolicyName())
		jp, ok := res.(*Policy)
		require.True(t, ok)
		assert.Equal(t, []component.ID{component.MustNewID(DefaultReceiverID)}, jp.LogReceiverIDs())
	})

	t.Run("invalid policy in slice returns error", func(t *testing.T) {
		t.Parallel()
		bad := &Policy{proto: &policyv1alpha1.JournaldSourcePolicy{Id: ""}}
		res, err := p1.Consolidate([]googlepolicy.SourcePolicy{p1, bad})
		require.ErrorIs(t, err, ErrMissingID)
		assert.Nil(t, res)
	})

	t.Run("wrong type returns error", func(t *testing.T) {
		t.Parallel()
		res, err := p1.Consolidate([]googlepolicy.SourcePolicy{p1, nil})
		require.Error(t, err)
		assert.Nil(t, res)
	})
}

func FuzzPolicy_LoadAndValidate(f *testing.F) {
	f.Add("journald-1")
	f.Add("")
	f.Add("special/id@chars!")

	f.Fuzz(func(t *testing.T, id string) {
		pb := &policyv1alpha1.JournaldSourcePolicy{Id: id}
		pol, err := NewPolicyFromProto(pb)
		if id == "" {
			require.ErrorIs(t, err, ErrMissingID)
			assert.Nil(t, pol)
		} else {
			require.NoError(t, err)
			require.NotNil(t, pol)
			assert.Equal(t, id, pol.PolicyName())

			conf, err := pol.Evaluate(context.Background())
			require.NoError(t, err)
			assert.NotNil(t, conf)
		}
	})
}
