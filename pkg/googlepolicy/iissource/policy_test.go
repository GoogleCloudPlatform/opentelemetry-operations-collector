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

package iissource

import (
	"context"
	"errors"
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"google.golang.org/protobuf/proto"
)

func TestPolicy_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		proto   *policyv1alpha1.IisSourcePolicy
		wantErr error
	}{
		{
			name:    "nil proto",
			proto:   nil,
			wantErr: ErrNilProto,
		},
		{
			name: "empty id",
			proto: &policyv1alpha1.IisSourcePolicy{
				Id: "",
			},
			wantErr: ErrMissingID,
		},
		{
			name: "interval below minimum (0s)",
			proto: &policyv1alpha1.IisSourcePolicy{
				Id:                        "iis-too-frequent",
				CollectionIntervalSeconds: proto.Int32(0),
			},
			wantErr: ErrInvalidCollectionInterval,
		},
		{
			name: "interval below minimum (5s)",
			proto: &policyv1alpha1.IisSourcePolicy{
				Id:                        "iis-too-frequent-5",
				CollectionIntervalSeconds: proto.Int32(5),
			},
			wantErr: ErrInvalidCollectionInterval,
		},
		{
			name: "interval below minimum (9s)",
			proto: &policyv1alpha1.IisSourcePolicy{
				Id:                        "iis-too-frequent-9",
				CollectionIntervalSeconds: proto.Int32(9),
			},
			wantErr: ErrInvalidCollectionInterval,
		},
		{
			name: "interval at minimum boundary (10s)",
			proto: &policyv1alpha1.IisSourcePolicy{
				Id:                        "iis-minimum-boundary",
				CollectionIntervalSeconds: proto.Int32(10),
			},
			wantErr: nil,
		},
		{
			name: "valid policy with custom interval (30s)",
			proto: &policyv1alpha1.IisSourcePolicy{
				Id:                        "iis-valid-custom",
				CollectionIntervalSeconds: proto.Int32(30),
			},
			wantErr: nil,
		},
		{
			name: "valid policy with unset interval (defaults to 60s)",
			proto: &policyv1alpha1.IisSourcePolicy{
				Id: "iis-default-interval",
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
				assert.True(t, errors.Is(err, tc.wantErr) || err.Error() == tc.wantErr.Error(), "expected error %v, got %v", tc.wantErr, err)
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

	pol, err := NewPolicyFromProto(&policyv1alpha1.IisSourcePolicy{
		Id: "iis-test",
	})
	require.NoError(t, err)

	assert.Equal(t, []component.ID{component.MustNewID(DefaultReceiverID)}, pol.MetricReceiverIDs())
	assert.Nil(t, pol.LogReceiverIDs())
	assert.Nil(t, pol.TraceReceiverIDs())
}

func TestPolicy_EffectiveCollectionIntervalSeconds(t *testing.T) {
	t.Parallel()

	t.Run("unset interval defaults to 60", func(t *testing.T) {
		t.Parallel()
		pol, err := NewPolicyFromProto(&policyv1alpha1.IisSourcePolicy{
			Id: "iis-default",
		})
		require.NoError(t, err)
		assert.Equal(t, DefaultCollectionIntervalSeconds, pol.EffectiveCollectionIntervalSeconds())
	})

	t.Run("explicit interval returns set value", func(t *testing.T) {
		t.Parallel()
		pol, err := NewPolicyFromProto(&policyv1alpha1.IisSourcePolicy{
			Id:                        "iis-custom",
			CollectionIntervalSeconds: proto.Int32(25),
		})
		require.NoError(t, err)
		assert.Equal(t, int32(25), pol.EffectiveCollectionIntervalSeconds())
	})
}

func TestPolicy_Evaluate(t *testing.T) {
	t.Parallel()

	t.Run("default interval evaluation", func(t *testing.T) {
		t.Parallel()
		pol, err := NewPolicyFromProto(&policyv1alpha1.IisSourcePolicy{
			Id: "iis-default",
		})
		require.NoError(t, err)

		conf, err := pol.Evaluate(context.Background())
		require.NoError(t, err)
		require.NotNil(t, conf)

		raw := conf.ToStringMap()
		receivers, ok := raw["receivers"].(map[string]any)
		require.True(t, ok, "receivers key must be a map")

		iisCfg, ok := receivers[DefaultReceiverID].(map[string]any)
		require.True(t, ok, "iis receiver must be present")
		assert.Equal(t, "60s", iisCfg["collection_interval"])
	})

	t.Run("custom interval evaluation", func(t *testing.T) {
		t.Parallel()
		pol, err := NewPolicyFromProto(&policyv1alpha1.IisSourcePolicy{
			Id:                        "iis-custom",
			CollectionIntervalSeconds: proto.Int32(15),
		})
		require.NoError(t, err)

		conf, err := pol.Evaluate(context.Background())
		require.NoError(t, err)
		require.NotNil(t, conf)

		raw := conf.ToStringMap()
		receivers, ok := raw["receivers"].(map[string]any)
		require.True(t, ok)

		iisCfg, ok := receivers[DefaultReceiverID].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "15s", iisCfg["collection_interval"])
	})
}

func TestPolicy_Evaluate_InvalidPolicy(t *testing.T) {
	t.Parallel()

	pol := &Policy{
		proto: &policyv1alpha1.IisSourcePolicy{
			Id: "",
		},
	}
	conf, err := pol.Evaluate(context.Background())
	require.Error(t, err)
	assert.Nil(t, conf)
}

func TestDriver_LoadPolicy(t *testing.T) {
	t.Parallel()

	t.Run("load from valid map with interval", func(t *testing.T) {
		t.Parallel()
		p, err := driver.LoadPolicy(map[string]any{
			"id":                          "my-iis",
			"collection_interval_seconds": 30,
		})
		require.NoError(t, err)
		require.NotNil(t, p)
		assert.Equal(t, "my-iis", p.PolicyName())
		assert.Equal(t, PolicyType, p.PolicyType())
		ip, ok := p.(*Policy)
		require.True(t, ok)
		assert.Equal(t, int32(30), ip.EffectiveCollectionIntervalSeconds())
	})

	t.Run("load from valid map without interval", func(t *testing.T) {
		t.Parallel()
		p, err := driver.LoadPolicy(map[string]any{
			"id": "my-iis-default",
		})
		require.NoError(t, err)
		require.NotNil(t, p)
		ip, ok := p.(*Policy)
		require.True(t, ok)
		assert.Equal(t, DefaultCollectionIntervalSeconds, ip.EffectiveCollectionIntervalSeconds())
	})

	t.Run("load from map with invalid interval (<10)", func(t *testing.T) {
		t.Parallel()
		p, err := driver.LoadPolicy(map[string]any{
			"id":                          "my-iis-invalid",
			"collection_interval_seconds": 5,
		})
		require.Error(t, err)
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

	p60, err := NewPolicyFromProto(&policyv1alpha1.IisSourcePolicy{
		Id: "iis-60s",
	})
	require.NoError(t, err)

	p30, err := NewPolicyFromProto(&policyv1alpha1.IisSourcePolicy{
		Id:                        "iis-30s",
		CollectionIntervalSeconds: proto.Int32(30),
	})
	require.NoError(t, err)

	p15, err := NewPolicyFromProto(&policyv1alpha1.IisSourcePolicy{
		Id:                        "iis-15s",
		CollectionIntervalSeconds: proto.Int32(15),
	})
	require.NoError(t, err)

	p120, err := NewPolicyFromProto(&policyv1alpha1.IisSourcePolicy{
		Id:                        "iis-120s",
		CollectionIntervalSeconds: proto.Int32(120),
	})
	require.NoError(t, err)

	t.Run("empty slice returns receiver policy", func(t *testing.T) {
		t.Parallel()
		res, err := p60.Consolidate(nil)
		require.NoError(t, err)
		assert.Equal(t, p60, res)
	})

	t.Run("single policy returns receiver policy", func(t *testing.T) {
		t.Parallel()
		res, err := p30.Consolidate([]googlepolicy.SourcePolicy{p30})
		require.NoError(t, err)
		ip, ok := res.(*Policy)
		require.True(t, ok)
		assert.Equal(t, int32(30), ip.EffectiveCollectionIntervalSeconds())
	})

	t.Run("multiple policies choose minimum interval", func(t *testing.T) {
		t.Parallel()
		res, err := p60.Consolidate([]googlepolicy.SourcePolicy{p60, p30, p15})
		require.NoError(t, err)
		ip, ok := res.(*Policy)
		require.True(t, ok)
		assert.Equal(t, int32(15), ip.EffectiveCollectionIntervalSeconds())

		conf, err := res.(googlepolicy.ComponentPolicy).Evaluate(context.Background())
		require.NoError(t, err)
		raw := conf.ToStringMap()
		iisCfg := raw["receivers"].(map[string]any)[DefaultReceiverID].(map[string]any)
		assert.Equal(t, "15s", iisCfg["collection_interval"])
	})

	t.Run("unset interval (60s) with larger interval (120s) chooses 60s", func(t *testing.T) {
		t.Parallel()
		res, err := p60.Consolidate([]googlepolicy.SourcePolicy{p60, p120})
		require.NoError(t, err)
		ip, ok := res.(*Policy)
		require.True(t, ok)
		assert.Equal(t, int32(60), ip.EffectiveCollectionIntervalSeconds())
	})

	t.Run("order independent consolidation", func(t *testing.T) {
		t.Parallel()
		res1, err := p60.Consolidate([]googlepolicy.SourcePolicy{p60, p30})
		require.NoError(t, err)
		ip1, ok := res1.(*Policy)
		require.True(t, ok)

		res2, err := p30.Consolidate([]googlepolicy.SourcePolicy{p30, p60})
		require.NoError(t, err)
		ip2, ok := res2.(*Policy)
		require.True(t, ok)

		assert.Equal(t, ip1.EffectiveCollectionIntervalSeconds(), ip2.EffectiveCollectionIntervalSeconds())
		assert.Equal(t, int32(30), ip1.EffectiveCollectionIntervalSeconds())
	})

	t.Run("invalid receiver returns error", func(t *testing.T) {
		t.Parallel()
		badReceiver := &Policy{proto: &policyv1alpha1.IisSourcePolicy{Id: ""}}
		res, err := badReceiver.Consolidate(nil)
		require.ErrorIs(t, err, ErrMissingID)
		assert.Nil(t, res)

		nilReceiver := &Policy{}
		res, err = nilReceiver.Consolidate([]googlepolicy.SourcePolicy{p60})
		require.ErrorIs(t, err, ErrNilProto)
		assert.Nil(t, res)
	})

	t.Run("invalid policy in slice returns error", func(t *testing.T) {
		t.Parallel()
		bad := &Policy{proto: &policyv1alpha1.IisSourcePolicy{Id: ""}}
		res, err := p60.Consolidate([]googlepolicy.SourcePolicy{p60, bad})
		require.Error(t, err)
		assert.Nil(t, res)

		badInterval := &Policy{
			proto: &policyv1alpha1.IisSourcePolicy{
				Id:                        "bad-interval",
				CollectionIntervalSeconds: proto.Int32(2),
			},
		}
		res, err = p60.Consolidate([]googlepolicy.SourcePolicy{p60, badInterval})
		require.Error(t, err)
		assert.Nil(t, res)
	})

	t.Run("wrong type returns error", func(t *testing.T) {
		t.Parallel()
		res, err := p60.Consolidate([]googlepolicy.SourcePolicy{p60, nil})
		require.Error(t, err)
		assert.Nil(t, res)
	})
}
