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
// receiver configurations.
package windowseventlogsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

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

	// DefaultSuppressRenderingInfo disables Win32 EvtFormatMessage rendering calls to avoid
	// single-threaded IPC bottlenecks on high-volume channels (e.g. Domain Controllers, WEC).
	DefaultSuppressRenderingInfo = true
	// DefaultMaxReads batches event retrieval to minimize Win32 EvtNext syscalls and bookmark transactions.
	DefaultMaxReads = 500
	// DefaultIgnoreChannelErrors fails open on missing or uninstalled host channels (preventing startup crashes).
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
	// ErrInvalidChannelName is returned when a channel name cannot produce a valid OpenTelemetry component ID.
	ErrInvalidChannelName = errors.New("windows event log channel name produces an invalid component ID")
)

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
		trimmed := strings.TrimSpace(ch)
		if trimmed == "" {
			return ErrEmptyChannelName
		}
		san := SanitizeChannelName(trimmed)
		if san == "" {
			return fmt.Errorf("%w: %q contains no valid characters", ErrInvalidChannelName, ch)
		}
		var id component.ID
		if err := id.UnmarshalText([]byte(ReceiverType + "/" + san)); err != nil {
			return fmt.Errorf("%w: %q: %v", ErrInvalidChannelName, ch, err)
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
		key := strings.ToUpper(trimmed)
		if !seen[key] {
			seen[key] = true
			result = append(result, trimmed)
		}
	}
	return result
}

// SanitizeChannelName converts a Windows Event Log channel name into a valid OpenTelemetry
// component ID name suffix by lower-casing and replacing any characters not permitted in component names
// (Unicode categories Z, C, S, slashes, backslashes, and colons) with underscores.
// The sanitized string is capped at 1024 characters per OpenTelemetry component naming constraints.
func SanitizeChannelName(channel string) string {
	s := strings.TrimSpace(channel)
	if s == "" {
		return ""
	}

	var sb strings.Builder
	lastUnderscore := false

	for _, r := range s {
		if unicode.Is(unicode.Z, r) || unicode.Is(unicode.C, r) || unicode.Is(unicode.S, r) || r == '/' || r == '\\' || r == ':' || r == '_' {
			if !lastUnderscore && sb.Len() > 0 {
				sb.WriteRune('_')
				lastUnderscore = true
			}
		} else {
			sb.WriteRune(unicode.ToLower(r))
			lastUnderscore = false
		}
	}

	res := strings.Trim(sb.String(), "_")
	if len(res) > 1024 {
		h := sha256.Sum256([]byte(channel))
		hashSuffix := "_" + hex.EncodeToString(h[:4]) // 9 characters
		res = res[:1024-len(hashSuffix)] + hashSuffix
	}
	return res
}

type channelReceiver struct {
	channel string
	id      component.ID
}

// channelReceivers returns a slice of channelReceiver mappings for this policy.
// If multiple distinct channels sanitize to the same component name, subsequent channels
// are disambiguated with a deterministic 8-character hex hash suffix.
func (p *Policy) channelReceivers() []channelReceiver {
	channels := p.Channels()
	if len(channels) == 0 {
		return nil
	}
	usedIDs := make(map[string]bool)
	result := make([]channelReceiver, 0, len(channels))
	for _, ch := range channels {
		base := SanitizeChannelName(ch)
		idName := base
		if usedIDs[idName] {
			for attempt := 0; usedIDs[idName]; attempt++ {
				var h [32]byte
				if attempt == 0 {
					h = sha256.Sum256([]byte(ch))
				} else {
					h = sha256.Sum256([]byte(fmt.Sprintf("%s:%d", ch, attempt)))
				}
				hashSuffix := hex.EncodeToString(h[:4])
				maxBase := 1024 - len(hashSuffix) - 1
				curBase := base
				if len(curBase) > maxBase {
					curBase = curBase[:maxBase]
				}
				idName = fmt.Sprintf("%s_%s", curBase, hashSuffix)
			}
		}
		usedIDs[idName] = true
		id := component.MustNewIDWithName(ReceiverType, idName)
		result = append(result, channelReceiver{channel: ch, id: id})
	}
	return result
}

// LogReceiverIDs returns the component IDs of log receivers synthesized by this policy.
// Each distinct channel receives an isolated receiver instance.
func (p *Policy) LogReceiverIDs() []component.ID {
	recs := p.channelReceivers()
	if len(recs) == 0 {
		return nil
	}
	recIDs := make([]component.ID, 0, len(recs))
	for _, r := range recs {
		recIDs = append(recIDs, r.id)
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

	seen := make(map[string]string) // key: uppercase trimmed channel, value: first-seen casing
	var orderedKeys []string

	// Register receiver's own channels first
	for _, ch := range p.Channels() {
		key := strings.ToUpper(strings.TrimSpace(ch))
		if _, exists := seen[key]; !exists {
			seen[key] = ch
			orderedKeys = append(orderedKeys, key)
		}
	}

	for _, other := range policies {
		if other == nil {
			return nil, ErrNilProto
		}
		if other == p {
			continue // skip self if present in slice
		}
		if err := other.Validate(); err != nil {
			return nil, fmt.Errorf("cannot consolidate invalid policy %q: %w", other.PolicyName(), err)
		}
		wp, ok := other.(*Policy)
		if !ok {
			return nil, fmt.Errorf("cannot consolidate policy of type %T into windows event log source policy", other)
		}
		for _, ch := range wp.Channels() {
			key := strings.ToUpper(strings.TrimSpace(ch))
			if _, exists := seen[key]; !exists {
				seen[key] = ch
				orderedKeys = append(orderedKeys, key)
			}
		}
	}

	// Sort channels deterministically by uppercase key
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
// For each distinct channel, it synthesizes an isolated receiver instance.
func (p *Policy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("policy %q failed validation: %w", p.PolicyName(), err)
	}

	recs := p.channelReceivers()
	receivers := make(map[string]any, len(recs))
	for _, r := range recs {
		recKey := fmt.Sprintf("%s/%s", ReceiverType, r.id.Name())
		receivers[recKey] = map[string]any{
			"channel":                 r.channel,
			"suppress_rendering_info": DefaultSuppressRenderingInfo,
			// TODO: Enable event_driven_scraping: true once upstream
			// open-telemetry/opentelemetry-collector-contrib#51962 is resolved and pinned.
			"max_reads":             DefaultMaxReads,
			"ignore_channel_errors": DefaultIgnoreChannelErrors,
		}
	}

	return confmap.NewFromStringMap(map[string]any{
		"receivers": receivers,
	}), nil
}
