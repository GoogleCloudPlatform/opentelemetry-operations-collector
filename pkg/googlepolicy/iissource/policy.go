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

// Package iissource compiles and evaluates IisSourcePolicy configurations.
// It translates IisSourcePolicy definitions into OpenTelemetry Collector iis
// receiver configurations for Microsoft IIS metric collection.
package iissource

import (
	"context"
	"errors"
	"fmt"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for IIS source policies.
const (
	PolicyType        = "iis_source"
	DefaultReceiverID = "iis"

	// DefaultCollectionIntervalSeconds is used when collection_interval_seconds is unset.
	DefaultCollectionIntervalSeconds int32 = 60
	// MinCollectionIntervalSeconds is the minimum allowed scrape interval.
	MinCollectionIntervalSeconds int32 = 10
)

var (
	// ErrNilProto is returned when a nil IisSourcePolicy protobuf is provided.
	ErrNilProto = errors.New("iis source policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("iis source policy id cannot be empty")
	// ErrInvalidCollectionInterval is returned when collection_interval_seconds is set to less than 10.
	ErrInvalidCollectionInterval = fmt.Errorf("collection_interval_seconds must be >= %d", MinCollectionIntervalSeconds)
)

// driver loads an IisSourcePolicy from either shape it can arrive in: the proto
// a control plane sends, or the map an authored config decodes to.
var driver = &googlepolicy.ProtoDriver[*policyv1alpha1.IisSourcePolicy]{
	Class: googlepolicy.PolicyClassSource,
	New: func(pb *policyv1alpha1.IisSourcePolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(err)
	}
}

// Policy represents a compiled, validated IisSourcePolicy ready for evaluation into Collector configuration.
type Policy struct {
	proto *policyv1alpha1.IisSourcePolicy
}

var (
	_ googlepolicy.Policy                     = (*Policy)(nil)
	_ googlepolicy.ComponentPolicy            = (*Policy)(nil)
	_ googlepolicy.SourcePolicy               = (*Policy)(nil)
	_ googlepolicy.ConsolidatableSourcePolicy = (*Policy)(nil)
)

// NewPolicyFromProto validates and compiles an IisSourcePolicy protobuf into a Policy.
// It defensively clones the input proto to prevent concurrent mutation by callers.
func NewPolicyFromProto(pb *policyv1alpha1.IisSourcePolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}
	p := &Policy{proto: proto.Clone(pb).(*policyv1alpha1.IisSourcePolicy)}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// PolicyName returns the unique identifier of the policy.
func (p *Policy) PolicyName() string {
	if p == nil || p.proto == nil {
		return ""
	}
	return p.proto.GetId()
}

// PolicyType returns the policy type string ("iis_source").
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns googlepolicy.PolicyClassSource.
func (p *Policy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}

// EffectiveCollectionIntervalSeconds returns the effective scrape interval in seconds.
// If collection_interval_seconds is unset, it defaults to DefaultCollectionIntervalSeconds (60).
func (p *Policy) EffectiveCollectionIntervalSeconds() int32 {
	if p == nil || p.proto == nil || p.proto.CollectionIntervalSeconds == nil {
		return DefaultCollectionIntervalSeconds
	}
	return *p.proto.CollectionIntervalSeconds
}

// Validate checks the fields of the policy.
// An IisSourcePolicy is valid if:
// 1. proto and ID are non-empty.
// 2. collection_interval_seconds, if set, is >= MinCollectionIntervalSeconds (10).
func (p *Policy) Validate() error {
	if p == nil || p.proto == nil {
		return ErrNilProto
	}
	if p.proto.GetId() == "" {
		return ErrMissingID
	}
	if p.proto.CollectionIntervalSeconds != nil && *p.proto.CollectionIntervalSeconds < MinCollectionIntervalSeconds {
		return ErrInvalidCollectionInterval
	}
	return nil
}

// Consolidate implements googlepolicy.ConsolidatableSourcePolicy.
// Multiple valid IisSourcePolicy instances merge into a single `iis` receiver using
// the minimum effective collection_interval_seconds across all policies.
func (p *Policy) Consolidate(policies []googlepolicy.SourcePolicy) (googlepolicy.SourcePolicy, error) {
	if p == nil || p.proto == nil {
		return nil, ErrNilProto
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		return p, nil
	}

	minInterval := p.EffectiveCollectionIntervalSeconds()

	for _, other := range policies {
		if other == nil {
			return nil, ErrNilProto
		}
		if err := other.Validate(); err != nil {
			return nil, fmt.Errorf("cannot consolidate invalid policy %q: %w", other.PolicyName(), err)
		}
		ip, ok := other.(*Policy)
		if !ok {
			return nil, fmt.Errorf("cannot consolidate policy of type %T into iis source policy", other)
		}
		interval := ip.EffectiveCollectionIntervalSeconds()
		if interval < minInterval {
			minInterval = interval
		}
	}

	cloned := proto.Clone(p.proto).(*policyv1alpha1.IisSourcePolicy)
	cloned.CollectionIntervalSeconds = proto.Int32(minInterval)
	return NewPolicyFromProto(cloned)
}

// Evaluate produces the confmap for the iis receiver.
// It configures the collection_interval using the effective collection interval in seconds.
func (p *Policy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("policy %q failed validation: %w", p.PolicyName(), err)
	}

	cfg := map[string]any{
		"receivers": map[string]any{
			DefaultReceiverID: map[string]any{
				"collection_interval": fmt.Sprintf("%ds", p.EffectiveCollectionIntervalSeconds()),
			},
		},
	}
	return confmap.NewFromStringMap(cfg), nil
}

// MetricReceiverIDs returns the component IDs of metric receivers synthesized by this policy.
func (p *Policy) MetricReceiverIDs() []component.ID {
	return []component.ID{component.MustNewID(DefaultReceiverID)}
}

// LogReceiverIDs returns nil because IisSourcePolicy only handles the metrics signal.
func (p *Policy) LogReceiverIDs() []component.ID {
	return nil
}

// TraceReceiverIDs returns nil because IisSourcePolicy only handles the metrics signal.
func (p *Policy) TraceReceiverIDs() []component.ID {
	return nil
}
