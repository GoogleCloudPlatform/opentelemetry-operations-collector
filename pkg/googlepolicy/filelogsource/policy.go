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
	"strings"

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
	DefaultPipelineID = "logs/filelog"
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

// forbiddenPrefixes contains forbidden sensitive system paths on Linux/Unix.
var forbiddenPrefixes = []string{
	"/etc",
	"/root",
	"/proc",
	"/sys",
	"/var/run",
	"/run",
	"/dev",
	"/boot",
	"/bin",
	"/sbin",
	"/usr/bin",
	"/usr/sbin",
}

// driver loads a FileLogSourcePolicy from either shape it can arrive in: the proto
// a control plane sends, or the map an authored config decodes to.
var driver = googlepolicy.ProtoDriver[*policyv1alpha1.FileLogSourcePolicy]{
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
	// ErrEmptyIncludePattern is returned when an include pattern is empty.
	ErrEmptyIncludePattern = errors.New("include pattern cannot be empty")
	// ErrPathTraversal is returned when an include pattern contains prohibited path traversal ('..').
	ErrPathTraversal = errors.New("include pattern contains prohibited path traversal ('..')")
	// ErrSensitiveDirectory is returned when an include pattern targets a forbidden sensitive directory.
	ErrSensitiveDirectory = errors.New("include pattern targets a forbidden sensitive directory")
	// ErrDisallowedLogRoot is returned when an include pattern is outside permitted OS log roots.
	ErrDisallowedLogRoot = errors.New("include pattern is outside permitted OS log roots")
	// ErrRelativePath is returned when an include pattern is not an absolute path.
	ErrRelativePath = errors.New("include pattern must be an absolute path")
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
func NewPolicyFromProto(pb *policyv1alpha1.FileLogSourcePolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}
	p := &Policy{proto: proto.Clone(pb).(*policyv1alpha1.FileLogSourcePolicy)}
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
	if p.proto == nil {
		return ErrNilProto
	}
	if p.proto.GetId() == "" {
		return ErrMissingID
	}
	if len(p.proto.GetInclude()) == 0 {
		return ErrMissingInclude
	}
	for i, pattern := range p.proto.GetInclude() {
		if err := validateIncludePattern(pattern); err != nil {
			return fmt.Errorf("include[%d]: %w", i, err)
		}
	}
	return nil
}

// Evaluate produces the confmap for the filelog receiver.
func (p *Policy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("policy %q failed validation: %w", p.PolicyName(), err)
	}

	cfg := map[string]any{
		"receivers": map[string]any{
			DefaultReceiverID: map[string]any{
				"include":           slices.Clone(p.proto.GetInclude()),
				"exclude":           slices.Clone(defaultExcludePatterns),
				"include_file_path": true,
			},
		},
	}
	return confmap.NewFromStringMap(cfg), nil
}

// LogsPipelines constructs the logs pipeline connecting this policy's filelog receiver
// through the specified processors to the exporters.
func (p *Policy) LogsPipelines(preExportProcessors []component.ID, exporters []component.ID, _ []component.ID) (*confmap.Conf, error) {
	procStrs := make([]string, 0, len(preExportProcessors))
	for _, proc := range preExportProcessors {
		procStrs = append(procStrs, proc.String())
	}

	expStrs := make([]string, 0, len(exporters))
	for _, exp := range exporters {
		expStrs = append(expStrs, exp.String())
	}

	cfg := map[string]any{
		"service": map[string]any{
			"pipelines": map[string]any{
				DefaultPipelineID: map[string]any{
					"receivers":  []string{DefaultReceiverID},
					"processors": procStrs,
					"exporters":  expStrs,
				},
			},
		},
	}
	return confmap.NewFromStringMap(cfg), nil
}

// MetricsPipelines returns nil because FileLogSourcePolicy only handles the logs signal.
func (p *Policy) MetricsPipelines([]component.ID, []component.ID, []component.ID) (*confmap.Conf, error) {
	return nil, nil
}

// TracesPipelines returns nil because FileLogSourcePolicy only handles the logs signal.
func (p *Policy) TracesPipelines([]component.ID, []component.ID, []component.ID) (*confmap.Conf, error) {
	return nil, nil
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
		for _, inc := range pol.Include() {
			if !seen[inc] {
				seen[inc] = true
				mergedIncludes = append(mergedIncludes, inc)
			}
		}
	}
	slices.Sort(mergedIncludes)

	return NewPolicyFromProto(&policyv1alpha1.FileLogSourcePolicy{
		Id:      "filelog_consolidated",
		Include: mergedIncludes,
	})
}

// Consolidate implements googlepolicy.ConsolidatableSourcePolicy.
func (p *Policy) Consolidate(policies []googlepolicy.SourcePolicy) (googlepolicy.SourcePolicy, error) {
	if len(policies) == 0 {
		return nil, nil
	}
	typedPolicies := make([]*Policy, 0, len(policies))
	for _, sp := range policies {
		pol, ok := sp.(*Policy)
		if !ok {
			return nil, fmt.Errorf("cannot consolidate policy of type %T with FileLogSourcePolicy", sp)
		}
		typedPolicies = append(typedPolicies, pol)
	}
	return Merge(typedPolicies...)
}

// Include returns a clone of the include patterns configured in the policy.
func (p *Policy) Include() []string {
	if p.proto == nil {
		return nil
	}
	return slices.Clone(p.proto.GetInclude())
}

// validateIncludePattern validates that a single pattern adheres to path security guardrails.
func validateIncludePattern(pattern string) error {
	if pattern == "" {
		return ErrEmptyIncludePattern
	}

	// Reject any path traversal segments.
	if strings.Contains(pattern, "..") {
		return fmt.Errorf("%w: %q", ErrPathTraversal, pattern)
	}

	// Check for absolute path prefix (Linux '/' or Windows drive letter / env var).
	isLinuxAbs := strings.HasPrefix(pattern, "/")
	isWindowsAbs := isWindowsAbsolute(pattern)
	if !isLinuxAbs && !isWindowsAbs {
		return fmt.Errorf("%w: %q", ErrRelativePath, pattern)
	}

	// Check forbidden sensitive directories on Linux/Unix.
	if isLinuxAbs {
		for _, f := range forbiddenPrefixes {
			if pattern == f || strings.HasPrefix(pattern, f+"/") || strings.HasPrefix(pattern, f+"\\") {
				return fmt.Errorf("%w: %q", ErrSensitiveDirectory, pattern)
			}
		}

		// Allowed Linux roots:
		// 1. /var/log/**
		// 2. /opt/**/logs/**
		// 3. /srv/**/logs/**
		if pattern == "/var/log" || strings.HasPrefix(pattern, "/var/log/") {
			return nil
		}
		if (strings.HasPrefix(pattern, "/opt/") || strings.HasPrefix(pattern, "/srv/")) &&
			(strings.Contains(pattern, "/logs/") || strings.HasSuffix(pattern, "/logs")) {
			return nil
		}

		return fmt.Errorf("%w: %q", ErrDisallowedLogRoot, pattern)
	}

	// Windows paths: normalize separators to '/' and lowercase for check.
	winNorm := strings.ToLower(strings.ReplaceAll(pattern, "\\", "/"))

	// Allowed Windows roots:
	// 1. %ProgramData%\**\logs\** or <Drive>:\ProgramData\**\logs\**
	// 2. %SystemDrive%\inetpub\logs\** or <Drive>:\inetpub\logs\**
	if (strings.HasPrefix(winNorm, "%programdata%/") || isDrivePathPrefix(winNorm, "programdata")) &&
		(strings.Contains(winNorm, "/logs/") || strings.HasSuffix(winNorm, "/logs")) {
		return nil
	}

	if strings.HasPrefix(winNorm, "%systemdrive%/inetpub/logs/") ||
		strings.HasPrefix(winNorm, "%systemroot%/inetpub/logs/") ||
		isDrivePathPrefix(winNorm, "inetpub/logs") {
		return nil
	}

	return fmt.Errorf("%w: %q", ErrDisallowedLogRoot, pattern)
}

func isWindowsAbsolute(pattern string) bool {
	if len(pattern) >= 3 && isAlpha(pattern[0]) && pattern[1] == ':' && (pattern[2] == '/' || pattern[2] == '\\') {
		return true
	}
	if strings.HasPrefix(pattern, "%") {
		return true
	}
	return false
}

func isDrivePathPrefix(normalizedPath string, subDir string) bool {
	if len(normalizedPath) < 3 || !isAlpha(normalizedPath[0]) || normalizedPath[1] != ':' || normalizedPath[2] != '/' {
		return false
	}
	rest := normalizedPath[3:]
	return rest == subDir || strings.HasPrefix(rest, subDir+"/")
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
