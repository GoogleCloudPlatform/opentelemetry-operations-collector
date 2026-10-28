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

// Package windowseventlogsource compiles and evaluates WindowsEventLogSourcePolicy configurations.
// It translates WindowsEventLogSourcePolicy definitions into OpenTelemetry Collector windowseventlog
// receiver configurations tuned for Google Cloud Observability and Google SecOps workloads.
package windowseventlogsource

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"google.golang.org/protobuf/proto"
)

// PolicyType is the registered policy type identifier for Windows Event Log source policies.
const (
	PolicyType   = "windows_event_log_source"
	ReceiverType = "windowseventlog"

	// Compiler defaults tuned for Enterprise SecOps workloads (Audit Findings #79 & #80):
	// DefaultSuppressRenderingInfo avoids Win32 EvtFormatMessage CPU/lock bottleneck (45x speedup).
	DefaultSuppressRenderingInfo = true
	// DefaultEventDrivenScraping uses Win32 WaitForMultipleObjects streaming (15.8x lower latency).
	DefaultEventDrivenScraping = true
	// DefaultMaxReads batches event retrieval to minimize EvtNext syscalls and bookmark transactions.
	DefaultMaxReads = 500
	// DefaultIgnoreChannelErrors fails open on missing host channels (preventing startup crashes).
	DefaultIgnoreChannelErrors = true
)

var (
	// ErrNilProto is returned when a nil WindowsEventLogSourcePolicy protobuf is provided.
	ErrNilProto = errors.New("windows event log source policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("windows event log source policy id cannot be empty")
	// ErrMissingChannels is returned when no channels are specified.
	ErrMissingChannels = errors.New("windows event log source policy must specify at least one channel")
	// ErrEmptyChannelName is returned when a channel name is empty or consists only of whitespace.
	ErrEmptyChannelName = errors.New("windows event log channel name cannot be empty")
)

// invalidChannelCharsRegex matches characters in channel names that must be sanitized for component IDs.
// Slashes (/ and \), whitespace (spaces, tabs, newlines), colons, and punctuation become underscores.
var invalidChannelCharsRegex = regexp.MustCompile(`[\s/\\:]+`)

// driver loads a WindowsEventLogSourcePolicy from either shape it can arrive in: the proto
// a control plane sends, or the map an authored config decodes to.
var driver = &googlepolicy.ProtoDriver[*policyv1alpha1.WindowsEventLogSourcePolicy]{
	Class: googlepolicy.PolicyClassSource,
	New: func(pb *policyv1alpha1.WindowsEventLogSourcePolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(err)
	}
}

// Policy represents a compiled, validated WindowsEventLogSourcePolicy ready for evaluation into Collector configuration.
type Policy struct {
	proto *policyv1alpha1.WindowsEventLogSourcePolicy
}

var (
	_ googlepolicy.Policy                     = (*Policy)(nil)
	_ googlepolicy.ComponentPolicy            = (*Policy)(nil)
	_ googlepolicy.SourcePolicy               = (*Policy)(nil)
	_ googlepolicy.ConsolidatableSourcePolicy = (*Policy)(nil)
)

// NewPolicyFromProto validates and compiles a WindowsEventLogSourcePolicy protobuf into a Policy.
// It defensively clones the input proto to prevent concurrent mutation by callers.
func NewPolicyFromProto(pb *policyv1alpha1.WindowsEventLogSourcePolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}
	p := &Policy{proto: proto.Clone(pb).(*policyv1alpha1.WindowsEventLogSourcePolicy)}
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

// PolicyType returns the policy type string.
func (p *Policy) PolicyType() string {
	return PolicyType
}

// PolicyClass returns PolicyClassSource.
func (p *Policy) PolicyClass() googlepolicy.PolicyClass {
	return googlepolicy.PolicyClassSource
}

// Validate checks whether the policy definition meets all semantic requirements.
func (p *Policy) Validate() error {
	if p == nil || p.proto == nil {
		return ErrNilProto
	}
	if strings.TrimSpace(p.proto.GetId()) == "" {
		return ErrMissingID
	}
	channels := p.proto.GetChannels()
	if len(channels) == 0 {
		return ErrMissingChannels
	}
	for _, ch := range channels {
		if strings.TrimSpace(ch) == "" || SanitizeChannelName(ch) == "" {
			return ErrEmptyChannelName
		}
	}
	return nil
}

// Channels returns the deduplicated, case-insensitively preserved channels configured in this policy,
// preserving the first-seen casing for each channel.
func (p *Policy) Channels() []string {
	if p == nil || p.proto == nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for _, ch := range p.proto.GetChannels() {
		trimmed := strings.TrimSpace(ch)
		if trimmed == "" {
			continue
		}
		key := SanitizeChannelName(trimmed)
		if key == "" {
			continue
		}
		if !seen[key] {
			seen[key] = true
			result = append(result, trimmed)
		}
	}
	return result
}

// SanitizeChannelName converts a Windows Event Log channel name into a valid, safe OpenTelemetry
// component ID name suffix by lower-casing and replacing slashes, backslashes, spaces, and colons with underscores.
func SanitizeChannelName(channel string) string {
	s := strings.TrimSpace(channel)
	s = strings.ToLower(s)
	s = invalidChannelCharsRegex.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}

// LogReceiverIDs returns the component IDs of log receivers synthesized by this policy.
// In Win32, each channel subscription requires an isolated subscription handle.
func (p *Policy) LogReceiverIDs() []component.ID {
	channels := p.Channels()
	if len(channels) == 0 {
		return nil
	}
	recIDs := make([]component.ID, 0, len(channels))
	for _, ch := range channels {
		sanitized := SanitizeChannelName(ch)
		recIDs = append(recIDs, component.MustNewIDWithName(ReceiverType, sanitized))
	}
	// Sort receiver IDs deterministically by their component name
	slices.SortFunc(recIDs, func(a, b component.ID) int {
		return strings.Compare(a.Name(), b.Name())
	})
	return recIDs
}

// MetricReceiverIDs returns nil as WindowsEventLogSourcePolicy only produces log receivers.
func (p *Policy) MetricReceiverIDs() []component.ID {
	return nil
}

// TraceReceiverIDs returns nil as WindowsEventLogSourcePolicy only produces log receivers.
func (p *Policy) TraceReceiverIDs() []component.ID {
	return nil
}

// Consolidate merges multiple WindowsEventLogSourcePolicy instances into a single consolidated Policy.
// Windows Event Log channel names are case-insensitive (e.g. "Security" == "security").
// Multiple policies merge via the case-insensitive set union of their channels, preserving the
// first-seen channel casing and sorting channels deterministically.
func (p *Policy) Consolidate(policies []googlepolicy.SourcePolicy) (googlepolicy.SourcePolicy, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		return p, nil
	}

	seen := make(map[string]string) // key: sanitized channel key, value: first-seen casing
	var orderedKeys []string

	// Register receiver's own channels first
	for _, ch := range p.Channels() {
		key := SanitizeChannelName(ch)
		if _, exists := seen[key]; !exists {
			seen[key] = ch
			orderedKeys = append(orderedKeys, key)
		}
	}

	for _, other := range policies {
		if other == nil {
			return nil, ErrNilProto
		}
		if err := other.Validate(); err != nil {
			return nil, fmt.Errorf("cannot consolidate invalid policy %q: %w", other.PolicyName(), err)
		}
		wp, ok := other.(*Policy)
		if !ok {
			return nil, fmt.Errorf("cannot consolidate policy of type %T into windows event log source policy", other)
		}
		for _, ch := range wp.Channels() {
			key := SanitizeChannelName(ch)
			if _, exists := seen[key]; !exists {
				seen[key] = ch
				orderedKeys = append(orderedKeys, key)
			}
		}
	}

	// Sort channels deterministically by sanitized key
	slices.Sort(orderedKeys)
	mergedChannels := make([]string, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		mergedChannels = append(mergedChannels, seen[key])
	}

	cloned := proto.Clone(p.proto).(*policyv1alpha1.WindowsEventLogSourcePolicy)
	cloned.Channels = mergedChannels
	return NewPolicyFromProto(cloned)
}

// Evaluate produces the confmap for all synthesized windowseventlog receivers.
// For each distinct channel, it synthesizes an isolated receiver instance with SecOps-tuned compiler defaults:
// - suppress_rendering_info: true (avoids 45x Win32 EvtFormatMessage bottleneck)
// - event_driven_scraping: true (WaitForMultipleObjects streaming, 15.8x lower latency)
// - max_reads: 500 (reduces bookmark transactions and syscall overhead)
// - ignore_channel_errors: true (fails open on missing host channels)
func (p *Policy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("policy %q failed validation: %w", p.PolicyName(), err)
	}

	receivers := make(map[string]any)
	for _, ch := range p.Channels() {
		sanitized := SanitizeChannelName(ch)
		recKey := fmt.Sprintf("%s/%s", ReceiverType, sanitized)
		receivers[recKey] = map[string]any{
			"channel":                 ch,
			"suppress_rendering_info": DefaultSuppressRenderingInfo,
			"event_driven_scraping":   DefaultEventDrivenScraping,
			"max_reads":               DefaultMaxReads,
			"ignore_channel_errors":   DefaultIgnoreChannelErrors,
		}
	}

	return confmap.NewFromStringMap(map[string]any{
		"receivers": receivers,
	}), nil
}
