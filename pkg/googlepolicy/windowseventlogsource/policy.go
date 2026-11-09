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
	"unicode/utf8"

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
	// DefaultExcludeProvider specifies the collector's own Windows Event Log provider name
	// to prevent self-amplifying feedback loops when the collector logs to the Application channel.
	DefaultExcludeProvider = "otelcol-google"
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
	_ googlepolicy.Policy                        = (*Policy)(nil)
	_ googlepolicy.ComponentPolicy               = (*Policy)(nil)
	_ googlepolicy.SourcePolicy                  = (*Policy)(nil)
	_ googlepolicy.ConsolidatableSourcePolicy    = (*Policy)(nil)
	_ googlepolicy.PersistentStorageSourcePolicy = (*Policy)(nil)
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

// canonicalChannel returns the trimmed, case-folded form of a Windows Event Log channel name.
// Case folding follows Windows invariant ordinal uppercase rules so that deduplication,
// consolidation, and receiver ID hash derivation use the exact same canonical representation.
func canonicalChannel(channel string) string {
	return caseFoldChannel(strings.TrimSpace(channel))
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
	seenIDs := make(map[string]string)
	for _, ch := range channels {
		trimmed := strings.TrimSpace(ch)
		if trimmed == "" {
			return ErrEmptyChannelName
		}
		if strings.ContainsRune(trimmed, '\x00') {
			return fmt.Errorf("%w: %q contains NUL character", ErrInvalidChannelName, ch)
		}
		san := SanitizeChannelName(trimmed)
		if san == "" {
			return fmt.Errorf("%w: %q contains no valid characters", ErrInvalidChannelName, ch)
		}
		var id component.ID
		if err := id.UnmarshalText([]byte(ReceiverType + "/" + san)); err != nil {
			return fmt.Errorf("%w: %q: %v", ErrInvalidChannelName, ch, err)
		}
		// Guard against hash collisions across distinct channels
		key := canonicalChannel(trimmed)
		if prevChan, exists := seenIDs[san]; exists && canonicalChannel(prevChan) != key {
			return fmt.Errorf("%w: receiver ID collision between %q and %q", ErrInvalidChannelName, prevChan, ch)
		}
		seenIDs[san] = ch
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
		key := canonicalChannel(trimmed)
		if !seen[key] {
			seen[key] = true
			result = append(result, trimmed)
		}
	}
	return result
}

const (
	// maxPrefixBytes is the maximum byte length for the readable prefix of a synthesized receiver ID.
	maxPrefixBytes = 64
)

// SanitizeChannelName converts a Windows Event Log channel name into a deterministic, unique
// OpenTelemetry component ID name suffix.
// The resulting ID has the format: <safe_prefix>_<16_hex_hash>
// - safe_prefix: lowercased, disallowed characters replaced by '_', capped at 64 bytes on a rune boundary.
// - 16_hex_hash: 64-bit truncated SHA-256 hash of the canonical (case-folded, trimmed) channel name.
// This guarantees that receiver IDs are a pure function of the channel, ensuring stable IDs
// across policy set revisions for persistent bookmark storage.
func SanitizeChannelName(channel string) string {
	trimmed := strings.TrimSpace(channel)
	if trimmed == "" || strings.ContainsRune(trimmed, '\x00') {
		return ""
	}

	canonical := canonicalChannel(trimmed)
	h := sha256.Sum256([]byte(canonical))
	hashSuffix := hex.EncodeToString(h[:8]) // 16 hex characters = 64 bits of entropy

	var sb strings.Builder
	lastUnderscore := false

	// Build prefix from canonical to guarantee prefix is 100% invariant to input casing variants
	for _, r := range canonical {
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

	prefix := strings.Trim(sb.String(), "_")
	if prefix == "" {
		return ""
	}
	if len(prefix) > maxPrefixBytes {
		maxBase := maxPrefixBytes
		for maxBase > 0 && !utf8.RuneStart(prefix[maxBase]) {
			maxBase--
		}
		prefix = strings.TrimRight(prefix[:maxBase], "_")
	}

	return fmt.Sprintf("%s_%s", prefix, hashSuffix)
}

type channelReceiver struct {
	channel string
	id      component.ID
}

// channelReceivers returns a slice of channelReceiver mappings for this policy.
// Because SanitizeChannelName is a pure, 1-to-1 function of each channel (via its canonical hash),
// each distinct channel produces a deterministic receiver ID.
func (p *Policy) channelReceivers() []channelReceiver {
	channels := p.Channels()
	if len(channels) == 0 {
		return nil
	}
	result := make([]channelReceiver, 0, len(channels))
	for _, ch := range channels {
		idName := SanitizeChannelName(ch)
		id := component.MustNewIDWithName(ReceiverType, idName)
		result = append(result, channelReceiver{channel: ch, id: id})
	}
	return result
}

// LogReceiverIDs returns the component IDs of log receivers synthesized by this policy.
// Each distinct channel receives an isolated receiver instance.
// On non-Windows platforms, it returns nil because Windows Event Log is Windows-only.
func (p *Policy) LogReceiverIDs() []component.ID {
	if !isWindows {
		return nil
	}
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

// RequiresStorage returns true on Windows where windowseventlog receivers require persistent bookmark storage.
// On non-Windows platforms, it returns false because Windows Event Log is Windows-only.
func (p *Policy) RequiresStorage() bool {
	return isWindows
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

	seen := make(map[string]string) // key: canonical channel, value: first-seen casing
	var orderedKeys []string

	// Register receiver's own channels first
	for _, ch := range p.Channels() {
		key := canonicalChannel(ch)
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
			key := canonicalChannel(ch)
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

// receiverConfigs returns the map representation of synthesized windowseventlog receiver configurations.
// It is unexported and has no OS dependency so that configuration generation can be fully tested on all platforms.
func (p *Policy) receiverConfigs() map[string]any {
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
			"exclude_providers":     []string{DefaultExcludeProvider},
			"storage":               googlepolicy.StorageExtensionID.String(),
		}
	}
	return map[string]any{
		"receivers": receivers,
	}
}

// Evaluate produces the confmap for all synthesized windowseventlog receivers.
// For each distinct channel, it synthesizes an isolated receiver instance.
// On non-Windows platforms, it returns an empty configuration because Windows Event Log is Windows-only.
func (p *Policy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("policy %q failed validation: %w", p.PolicyName(), err)
	}

	if !isWindows {
		return confmap.New(), nil
	}

	return confmap.NewFromStringMap(p.receiverConfigs()), nil
}
