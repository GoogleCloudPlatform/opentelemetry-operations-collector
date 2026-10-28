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

// Package filelogsource compiles and evaluates FileLogSourcePolicy configurations.
// It translates FileLogSourcePolicy definitions into OpenTelemetry Collector filelog
// receiver configurations and logs pipelines with strict OS log root guardrails.
package filelogsource

import (
	"context"
	"errors"
	"fmt"
	"slices"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for file log source policies.
const (
	PolicyType        = "file_log_source"
	DefaultReceiverID = "filelog"
)

// defaultExcludePatterns contains the built-in exclude patterns for compressed binary archives.
var defaultExcludePatterns = []string{
	"**/*.gz",
	"**/*.bz2",
	"**/*.xz",
	"**/*.zst",
	"**/*.zip",
}

// DefaultExcludePatterns returns a copy of the default exclude patterns for compressed archives.
func DefaultExcludePatterns() []string {
	return slices.Clone(defaultExcludePatterns)
}

// driver loads a FileLogSourcePolicy from either shape it can arrive in: the proto
// a control plane sends, or the map an authored config decodes to.
var driver = &googlepolicy.ProtoDriver[*policyv1alpha1.FileLogSourcePolicy]{
	Class: googlepolicy.PolicyClassSource,
	New: func(pb *policyv1alpha1.FileLogSourcePolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(err)
	}
}

var (
	// ErrNilProto is returned when a nil FileLogSourcePolicy protobuf is provided.
	ErrNilProto = errors.New("file log source policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("file log source policy id cannot be empty")
	// ErrMissingInclude is returned when the policy contains no include patterns.
	ErrMissingInclude = errors.New("file log source policy must specify at least one include pattern")
	// ErrDuplicateIncludePattern is returned when an include pattern appears more than once in a policy.
	ErrDuplicateIncludePattern = errors.New("duplicate include pattern")
)

// Policy represents a compiled, validated FileLogSourcePolicy ready for evaluation into Collector configuration.
type Policy struct {
	proto *policyv1alpha1.FileLogSourcePolicy
}

var (
	_ googlepolicy.SourcePolicy               = (*Policy)(nil)
	_ googlepolicy.ConsolidatableSourcePolicy = (*Policy)(nil)
)

// NewPolicyFromProto validates and compiles a FileLogSourcePolicy protobuf into a Policy.
// Windows environment variable placeholders (e.g. %ProgramData%, %SystemDrive%)
// in included_paths are expanded to concrete paths and normalized so that paths are canonical
// and deterministic across evaluation and consolidation.
func NewPolicyFromProto(pb *policyv1alpha1.FileLogSourcePolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}
	cloned := proto.Clone(pb).(*policyv1alpha1.FileLogSourcePolicy)
	for i, inc := range cloned.GetIncludedPaths() {
		cloned.IncludedPaths[i] = NormalizePattern(inc)
	}
	p := &Policy{proto: cloned}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// PolicyName returns the policy ID.
func (p *Policy) PolicyName() string {
	if p == nil || p.proto == nil {
		return ""
	}
	return p.proto.GetId()
}

// PolicyType returns the registered policy type identifier ("file_log_source").
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns googlepolicy.PolicyClassSource.
func (p *Policy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}

// Validate checks the fields of the policy and enforces path guardrails.
func (p *Policy) Validate() error {
	if p == nil || p.proto == nil {
		return ErrNilProto
	}
	if p.proto.GetId() == "" {
		return ErrMissingID
	}
	if len(p.proto.GetIncludedPaths()) == 0 {
		return ErrMissingInclude
	}
	seen := make(map[string]bool, len(p.proto.GetIncludedPaths()))
	for i, pattern := range p.proto.GetIncludedPaths() {
		if err := ValidateIncludePattern(pattern); err != nil {
			return fmt.Errorf("included_paths[%d]: %w", i, err)
		}
		key := pathDedupeKey(pattern)
		if seen[key] {
			return fmt.Errorf("included_paths[%d]: %w: %q", i, ErrDuplicateIncludePattern, pattern)
		}
		seen[key] = true
	}
	return nil
}

// Evaluate produces the confmap for the filelog receiver.
func (p *Policy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("policy %q failed validation: %w", p.PolicyName(), err)
	}

	includes := make([]string, len(p.proto.GetIncludedPaths()))
	for i, inc := range p.proto.GetIncludedPaths() {
		includes[i] = NormalizePattern(inc)
	}

	cfg := map[string]any{
		"receivers": map[string]any{
			DefaultReceiverID: map[string]any{
				"include":           includes,
				"exclude":           slices.Clone(defaultExcludePatterns),
				"include_file_path": true,
			},
		},
	}
	return confmap.NewFromStringMap(cfg), nil
}

// LogReceiverIDs returns the component IDs of log receivers synthesized by this policy.
func (p *Policy) LogReceiverIDs() []component.ID {
	return []component.ID{component.MustNewID(DefaultReceiverID)}
}

// MetricReceiverIDs returns nil because FileLogSourcePolicy only handles the logs signal.
func (p *Policy) MetricReceiverIDs() []component.ID {
	return nil
}

// TraceReceiverIDs returns nil because FileLogSourcePolicy only handles the logs signal.
func (p *Policy) TraceReceiverIDs() []component.ID {
	return nil
}

// Merge consolidates multiple FileLogSourcePolicies into a single Policy whose
// include paths are the deduplicated, sorted set union of all include paths.
// If any policy is nil or fails validation, an error is returned.
// If policies is empty, nil is returned.
func Merge(policies ...*Policy) (*Policy, error) {
	if len(policies) == 0 {
		return nil, nil
	}
	if len(policies) == 1 {
		if policies[0] == nil {
			return nil, ErrNilProto
		}
		if err := policies[0].Validate(); err != nil {
			return nil, err
		}
		return policies[0], nil
	}

	seen := make(map[string]bool)
	var mergedIncludes []string
	for _, pol := range policies {
		if pol == nil {
			return nil, ErrNilProto
		}
		if err := pol.Validate(); err != nil {
			return nil, err
		}
		for _, inc := range pol.proto.GetIncludedPaths() {
			canonical := NormalizePattern(inc)
			if !seen[canonical] {
				seen[canonical] = true
				mergedIncludes = append(mergedIncludes, canonical)
			}
		}
	}
	slices.Sort(mergedIncludes)

	return NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:            "filelog_consolidated",
		IncludedPaths: mergedIncludes,
	})
}

// Consolidate implements googlepolicy.ConsolidatableSourcePolicy.
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
	hasSelf := false
	typedPolicies := make([]*Policy, 0, len(policies)+1)
	for _, sp := range policies {
		if sp == nil {
			return nil, ErrNilProto
		}
		if err := sp.Validate(); err != nil {
			return nil, fmt.Errorf("cannot consolidate invalid policy %q: %w", sp.PolicyName(), err)
		}
		pol, ok := sp.(*Policy)
		if !ok {
			return nil, fmt.Errorf("cannot consolidate policy of type %T with FileLogSourcePolicy", sp)
		}
		if pol == p {
			hasSelf = true
		}
		typedPolicies = append(typedPolicies, pol)
	}
	if !hasSelf {
		typedPolicies = append(typedPolicies, p)
	}
	return Merge(typedPolicies...)
}

// IncludedPaths returns a clone of the include patterns configured in the policy.
func (p *Policy) IncludedPaths() []string {
	if p == nil || p.proto == nil {
		return nil
	}
	return slices.Clone(p.proto.GetIncludedPaths())
}

// pathDedupeKey returns a canonical key for path comparison and deduplication.
func pathDedupeKey(pattern string) string {
	return NormalizePattern(pattern)
}
