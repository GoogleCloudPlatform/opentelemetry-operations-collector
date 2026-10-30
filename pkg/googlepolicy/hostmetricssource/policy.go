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

// Package hostmetricssource compiles and evaluates HostMetricsSourcePolicy configurations.
// It translates HostMetricsSourcePolicy definitions into OpenTelemetry Collector hostmetrics
// receiver configurations with default upstream scrapers.
package hostmetricssource

import (
	"context"
	"errors"
	"fmt"
	"time"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for host metrics source policies.
const (
	PolicyType = "host_metrics_source"

	DefaultCollectionIntervalSeconds int32 = 60
	MinCollectionIntervalSeconds     int32 = 10
)

var receiverID = component.MustNewIDWithName("hostmetrics", "googlecontrolplane")

var (
	// ErrNilProto is returned when a nil HostMetricsSourcePolicy protobuf is provided.
	ErrNilProto = errors.New("host metrics source policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("host metrics source policy id cannot be empty")
	// ErrInvalidCollectionInterval is returned when collection_interval_seconds is less than 10.
	ErrInvalidCollectionInterval = errors.New("collection_interval_seconds must be at least 10")
)

// defaultScrapers lists the upstream default scrapers enabled by HostMetricsSourcePolicy.
var defaultScrapers = []string{
	"cpu",
	"disk",
	"filesystem",
	"load",
	"memory",
	"network",
	"paging",
	"processes",
}

// driver loads a HostMetricsSourcePolicy from either shape it can arrive in: the proto
// a control plane sends, or the map an authored config decodes to.
var driver = &googlepolicy.ProtoDriver[*policyv1alpha1.HostMetricsSourcePolicy]{
	Class: googlepolicy.PolicyClassSource,
	New: func(pb *policyv1alpha1.HostMetricsSourcePolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(err)
	}
}

// Policy represents a compiled, validated HostMetricsSourcePolicy ready for evaluation into Collector configuration.
type Policy struct {
	proto *policyv1alpha1.HostMetricsSourcePolicy
}

var (
	_ googlepolicy.Policy                     = (*Policy)(nil)
	_ googlepolicy.ComponentPolicy            = (*Policy)(nil)
	_ googlepolicy.SourcePolicy               = (*Policy)(nil)
	_ googlepolicy.ConsolidatableSourcePolicy = (*Policy)(nil)
)

// NewPolicyFromProto validates and compiles a HostMetricsSourcePolicy protobuf into a Policy.
func NewPolicyFromProto(pb *policyv1alpha1.HostMetricsSourcePolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}
	p := &Policy{proto: proto.Clone(pb).(*policyv1alpha1.HostMetricsSourcePolicy)}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// PolicyName returns the policy ID.
func (p *Policy) PolicyName() string {
	if p.proto == nil {
		return ""
	}
	return p.proto.GetId()
}

// PolicyType returns the registered policy type identifier ("host_metrics_source").
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns googlepolicy.PolicyClassSource.
func (p *Policy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}

// Validate checks the fields of the policy.
func (p *Policy) Validate() error {
	if p.proto == nil {
		return ErrNilProto
	}
	if p.proto.GetId() == "" {
		return ErrMissingID
	}
	if p.proto.CollectionIntervalSeconds != nil && *p.proto.CollectionIntervalSeconds < MinCollectionIntervalSeconds {
		return fmt.Errorf("%w: got %d", ErrInvalidCollectionInterval, *p.proto.CollectionIntervalSeconds)
	}
	return nil
}

// EffectiveCollectionIntervalSeconds returns the scrape interval in seconds.
// If unset, it defaults to DefaultCollectionIntervalSeconds (60).
func (p *Policy) EffectiveCollectionIntervalSeconds() int32 {
	if p.proto == nil || p.proto.CollectionIntervalSeconds == nil {
		return DefaultCollectionIntervalSeconds
	}
	return *p.proto.CollectionIntervalSeconds
}

// CollectionInterval returns the scrape interval as a time.Duration.
func (p *Policy) CollectionInterval() time.Duration {
	return time.Duration(p.EffectiveCollectionIntervalSeconds()) * time.Second
}

// Consolidate implements googlepolicy.ConsolidatableSourcePolicy.
// Multiple valid HostMetricsSourcePolicy instances merge into a single `hostmetrics` receiver
// using the minimum effective collection_interval_seconds (order-independent and commutative).
func (p *Policy) Consolidate(policies []googlepolicy.SourcePolicy) (googlepolicy.SourcePolicy, error) {
	if p == nil || p.proto == nil {
		return nil, ErrNilProto
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(policies) == 0 || (len(policies) == 1 && policies[0] == p) {
		return p, nil
	}

	minEffectiveInterval := p.EffectiveCollectionIntervalSeconds()
	for _, sp := range policies {
		if sp == nil {
			return nil, ErrNilProto
		}
		if err := sp.Validate(); err != nil {
			return nil, fmt.Errorf("cannot consolidate invalid policy %q: %w", sp.PolicyName(), err)
		}
		hp, ok := sp.(*Policy)
		if !ok {
			return nil, fmt.Errorf("cannot consolidate policy of type %T into host metrics source policy", sp)
		}
		if otherInterval := hp.EffectiveCollectionIntervalSeconds(); otherInterval < minEffectiveInterval {
			minEffectiveInterval = otherInterval
		}
	}

	cloned := proto.Clone(p.proto).(*policyv1alpha1.HostMetricsSourcePolicy)
	cloned.CollectionIntervalSeconds = &minEffectiveInterval
	return NewPolicyFromProto(cloned)
}

// Evaluate produces the confmap for the hostmetrics receiver with upstream default scrapers.
func (p *Policy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("policy %q failed validation: %w", p.PolicyName(), err)
	}

	scrapers := make(map[string]any, len(defaultScrapers))
	for _, s := range defaultScrapers {
		scrapers[s] = map[string]any{}
	}

	cfg := map[string]any{
		"receivers": map[string]any{
			receiverID.String(): map[string]any{
				"collection_interval": fmt.Sprintf("%ds", p.EffectiveCollectionIntervalSeconds()),
				"scrapers":            scrapers,
			},
		},
	}
	return confmap.NewFromStringMap(cfg), nil
}

// LogReceiverIDs returns nil because HostMetricsSourcePolicy only handles the metrics signal.
func (p *Policy) LogReceiverIDs() []component.ID {
	return nil
}

// MetricReceiverIDs returns the component IDs of metric receivers synthesized by this policy.
func (p *Policy) MetricReceiverIDs() []component.ID {
	return []component.ID{receiverID}
}

// TraceReceiverIDs returns nil because HostMetricsSourcePolicy only handles the metrics signal.
func (p *Policy) TraceReceiverIDs() []component.ID {
	return nil
}
