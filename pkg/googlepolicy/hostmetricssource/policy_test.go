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

package hostmetricssource

import (
	"context"
	"errors"
	"testing"
	"time"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
)

func int32Ptr(i int32) *int32 {
	return &i
}

func TestPolicy_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		proto        *policyv1alpha1.HostMetricsSourcePolicy
		wantErr      error
		wantInterval int32
	}{
		{
			name:    "nil proto",
			proto:   nil,
			wantErr: ErrNilProto,
		},
		{
			name: "empty id",
			proto: &policyv1alpha1.HostMetricsSourcePolicy{
				Id: "",
			},
			wantErr: ErrMissingID,
		},
		{
			name: "valid policy with unset interval defaults to 60",
			proto: &policyv1alpha1.HostMetricsSourcePolicy{
				Id: "system-host-metrics",
			},
			wantErr:      nil,
			wantInterval: 60,
		},
		{
			name: "valid policy with explicit 10s interval (minimum allowed)",
			proto: &policyv1alpha1.HostMetricsSourcePolicy{
				Id:                        "system-host-metrics",
				CollectionIntervalSeconds: int32Ptr(10),
			},
			wantErr:      nil,
			wantInterval: 10,
		},
		{
			name: "valid policy with explicit 30s interval",
			proto: &policyv1alpha1.HostMetricsSourcePolicy{
				Id:                        "system-host-metrics",
				CollectionIntervalSeconds: int32Ptr(30),
			},
			wantErr:      nil,
			wantInterval: 30,
		},
		{
			name: "invalid policy with interval 9s (below minimum 10s)",
			proto: &policyv1alpha1.HostMetricsSourcePolicy{
				Id:                        "system-host-metrics",
				CollectionIntervalSeconds: int32Ptr(9),
			},
			wantErr: ErrInvalidCollectionInterval,
		},
		{
			name: "invalid policy with interval 0s",
			proto: &policyv1alpha1.HostMetricsSourcePolicy{
				Id:                        "system-host-metrics",
				CollectionIntervalSeconds: int32Ptr(0),
			},
			wantErr: ErrInvalidCollectionInterval,
		},
		{
			name: "invalid policy with negative interval",
			proto: &policyv1alpha1.HostMetricsSourcePolicy{
				Id:                        "system-host-metrics",
				CollectionIntervalSeconds: int32Ptr(-5),
			},
			wantErr: ErrInvalidCollectionInterval,
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
				assert.Equal(t, tc.wantInterval, pol.EffectiveCollectionIntervalSeconds())
				assert.Equal(t, time.Duration(tc.wantInterval)*time.Second, pol.CollectionInterval())
			}
		})
	}
}

func TestPolicy_ReceiverIDs(t *testing.T) {
	t.Parallel()

	pol, err := NewPolicyFromProto(&policyv1alpha1.HostMetricsSourcePolicy{
		Id: "host-metrics-test",
	})
	require.NoError(t, err)

	assert.Nil(t, pol.LogReceiverIDs())
	assert.Equal(t, []component.ID{component.MustNewID(DefaultReceiverID)}, pol.MetricReceiverIDs())
	assert.Nil(t, pol.TraceReceiverIDs())
}

func TestPolicy_Evaluate(t *testing.T) {
	t.Parallel()

	t.Run("default interval 60s", func(t *testing.T) {
		t.Parallel()
		pol, err := NewPolicyFromProto(&policyv1alpha1.HostMetricsSourcePolicy{
			Id: "host-metrics-sys",
		})
		require.NoError(t, err)

		conf, err := pol.Evaluate(context.Background())
		require.NoError(t, err)
		require.NotNil(t, conf)

		raw := conf.ToStringMap()
		receivers, ok := raw["receivers"].(map[string]any)
		require.True(t, ok, "receivers key must be a map")

		hmCfg, ok := receivers[DefaultReceiverID].(map[string]any)
		require.True(t, ok, "hostmetrics receiver must be present")

		assert.Equal(t, "60s", hmCfg["collection_interval"])

		scrapers, ok := hmCfg["scrapers"].(map[string]any)
		require.True(t, ok, "scrapers key must be a map")

		expectedScrapers := []string{"cpu", "disk", "filesystem", "load", "memory", "network", "paging", "processes"}
		require.Len(t, scrapers, len(expectedScrapers))
		for _, s := range expectedScrapers {
			assert.Contains(t, scrapers, s)
		}
	})

	t.Run("custom interval 15s", func(t *testing.T) {
		t.Parallel()
		pol, err := NewPolicyFromProto(&policyv1alpha1.HostMetricsSourcePolicy{
			Id:                        "host-metrics-15s",
			CollectionIntervalSeconds: int32Ptr(15),
		})
		require.NoError(t, err)

		conf, err := pol.Evaluate(context.Background())
		require.NoError(t, err)
		require.NotNil(t, conf)

		raw := conf.ToStringMap()
		receivers := raw["receivers"].(map[string]any)
		hmCfg := receivers[DefaultReceiverID].(map[string]any)
		assert.Equal(t, "15s", hmCfg["collection_interval"])
	})
}

func TestPolicy_Evaluate_InvalidPolicy(t *testing.T) {
	t.Parallel()

	pol := &Policy{
		proto: &policyv1alpha1.HostMetricsSourcePolicy{
			Id: "",
		},
	}
	conf, err := pol.Evaluate(context.Background())
	require.Error(t, err)
	assert.Nil(t, conf)
}

func TestDriver_LoadPolicy(t *testing.T) {
	t.Parallel()

	t.Run("load from valid map with default interval", func(t *testing.T) {
		t.Parallel()
		p, err := driver.LoadPolicy(map[string]any{
			"id": "my-hostmetrics",
		})
		require.NoError(t, err)
		require.NotNil(t, p)
		assert.Equal(t, "my-hostmetrics", p.PolicyName())
		assert.Equal(t, PolicyType, p.PolicyType())
		hp := p.(*Policy)
		assert.Equal(t, int32(60), hp.EffectiveCollectionIntervalSeconds())
	})

	t.Run("load from valid map with custom interval", func(t *testing.T) {
		t.Parallel()
		p, err := driver.LoadPolicy(map[string]any{
			"id":                          "my-hostmetrics",
			"collection_interval_seconds": 20,
		})
		require.NoError(t, err)
		require.NotNil(t, p)
		hp := p.(*Policy)
		assert.Equal(t, int32(20), hp.EffectiveCollectionIntervalSeconds())
	})

	t.Run("load from map with invalid interval (< 10)", func(t *testing.T) {
		t.Parallel()
		p, err := driver.LoadPolicy(map[string]any{
			"id":                          "my-hostmetrics",
			"collection_interval_seconds": 5,
		})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInvalidCollectionInterval))
		assert.Nil(t, p)
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

	pDefault, err := NewPolicyFromProto(&policyv1alpha1.HostMetricsSourcePolicy{Id: "hm-default"})
	require.NoError(t, err)

	p30, err := NewPolicyFromProto(&policyv1alpha1.HostMetricsSourcePolicy{
		Id:                        "hm-30s",
		CollectionIntervalSeconds: int32Ptr(30),
	})
	require.NoError(t, err)

	p10, err := NewPolicyFromProto(&policyv1alpha1.HostMetricsSourcePolicy{
		Id:                        "hm-10s",
		CollectionIntervalSeconds: int32Ptr(10),
	})
	require.NoError(t, err)

	t.Run("empty slice returns receiver policy", func(t *testing.T) {
		t.Parallel()
		res, err := pDefault.Consolidate(nil)
		require.NoError(t, err)
		assert.Equal(t, pDefault, res)
	})

	t.Run("single policy returns receiver policy", func(t *testing.T) {
		t.Parallel()
		res, err := pDefault.Consolidate([]googlepolicy.SourcePolicy{pDefault})
		require.NoError(t, err)
		assert.Equal(t, pDefault, res)
	})

	t.Run("multiple valid policies consolidate into minimum interval (60s and 30s -> 30s)", func(t *testing.T) {
		t.Parallel()
		res, err := pDefault.Consolidate([]googlepolicy.SourcePolicy{pDefault, p30})
		require.NoError(t, err)
		hp, ok := res.(*Policy)
		require.True(t, ok)
		assert.Equal(t, int32(30), hp.EffectiveCollectionIntervalSeconds())
		assert.Equal(t, []component.ID{component.MustNewID(DefaultReceiverID)}, hp.MetricReceiverIDs())
	})

	t.Run("multiple valid policies consolidate into minimum interval (30s and 10s -> 10s)", func(t *testing.T) {
		t.Parallel()
		res, err := p30.Consolidate([]googlepolicy.SourcePolicy{p30, p10})
		require.NoError(t, err)
		hp, ok := res.(*Policy)
		require.True(t, ok)
		assert.Equal(t, int32(10), hp.EffectiveCollectionIntervalSeconds())
	})

	t.Run("consolidation is commutative (order-independent)", func(t *testing.T) {
		t.Parallel()
		res1, err := pDefault.Consolidate([]googlepolicy.SourcePolicy{pDefault, p30, p10})
		require.NoError(t, err)

		res2, err := p10.Consolidate([]googlepolicy.SourcePolicy{p10, pDefault, p30})
		require.NoError(t, err)

		hp1 := res1.(*Policy)
		hp2 := res2.(*Policy)
		assert.Equal(t, hp1.EffectiveCollectionIntervalSeconds(), hp2.EffectiveCollectionIntervalSeconds())
		assert.Equal(t, int32(10), hp1.EffectiveCollectionIntervalSeconds())
	})

	t.Run("invalid receiver returns error", func(t *testing.T) {
		t.Parallel()
		badReceiver := &Policy{proto: &policyv1alpha1.HostMetricsSourcePolicy{Id: ""}}
		res, err := badReceiver.Consolidate(nil)
		require.ErrorIs(t, err, ErrMissingID)
		assert.Nil(t, res)

		nilReceiver := &Policy{}
		res, err = nilReceiver.Consolidate([]googlepolicy.SourcePolicy{pDefault})
		require.ErrorIs(t, err, ErrNilProto)
		assert.Nil(t, res)
	})

	t.Run("invalid policy in slice returns error", func(t *testing.T) {
		t.Parallel()
		bad := &Policy{proto: &policyv1alpha1.HostMetricsSourcePolicy{
			Id:                        "bad",
			CollectionIntervalSeconds: int32Ptr(5),
		}}
		res, err := pDefault.Consolidate([]googlepolicy.SourcePolicy{pDefault, bad})
		require.ErrorIs(t, err, ErrInvalidCollectionInterval)
		assert.Nil(t, res)
	})

	t.Run("wrong type returns error", func(t *testing.T) {
		t.Parallel()
		res, err := pDefault.Consolidate([]googlepolicy.SourcePolicy{pDefault, nil})
		require.Error(t, err)
		assert.Nil(t, res)
	})
}

func FuzzPolicy_LoadAndValidate(f *testing.F) {
	f.Add("hostmetrics-1", int32(60), true)
	f.Add("", int32(60), true)
	f.Add("hostmetrics-2", int32(10), true)
	f.Add("hostmetrics-3", int32(9), true)
	f.Add("hostmetrics-4", int32(0), false) // interval unset

	f.Fuzz(func(t *testing.T, id string, interval int32, setInterval bool) {
		pb := &policyv1alpha1.HostMetricsSourcePolicy{Id: id}
		if setInterval {
			pb.CollectionIntervalSeconds = &interval
		}
		pol, err := NewPolicyFromProto(pb)
		if id == "" {
			require.ErrorIs(t, err, ErrMissingID)
			assert.Nil(t, pol)
		} else if setInterval && interval < MinCollectionIntervalSeconds {
			require.ErrorIs(t, err, ErrInvalidCollectionInterval)
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
