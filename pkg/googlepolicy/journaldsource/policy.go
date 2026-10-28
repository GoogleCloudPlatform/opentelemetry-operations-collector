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

// Package journaldsource compiles and evaluates JournaldSourcePolicy configurations.
// It translates JournaldSourcePolicy definitions into OpenTelemetry Collector journald
// receiver configurations with automatic self-logging guardrails.
package journaldsource

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

// PolicyType is the registered policy type identifier for journald source policies.
const (
	PolicyType        = "journald_source"
	DefaultReceiverID = "journald"

	// collectorLogFilterExpr drops the collector's own log records across both Linux VM systemd
	// services and containerized environments (Kubernetes DaemonSet / Docker) to prevent self-logging loops.
	collectorLogFilterExpr = `(body._COMM != nil && body._COMM == "otelcol-google") || ` +
		`(body._SYSTEMD_UNIT != nil && body._SYSTEMD_UNIT == "otelcol-google.service") || ` +
		`(body.SYSLOG_IDENTIFIER != nil && body.SYSLOG_IDENTIFIER == "otelcol-google")`
)

var (
	// ErrNilProto is returned when a nil JournaldSourcePolicy protobuf is provided.
	ErrNilProto = errors.New("journald source policy proto cannot be nil")
	// ErrMissingID is returned when the policy ID is empty.
	ErrMissingID = errors.New("journald source policy id cannot be empty")
)

// driver loads a JournaldSourcePolicy from either shape it can arrive in: the proto
// a control plane sends, or the map an authored config decodes to.
var driver = &googlepolicy.ProtoDriver[*policyv1alpha1.JournaldSourcePolicy]{
	Class: googlepolicy.PolicyClassSource,
	New: func(pb *policyv1alpha1.JournaldSourcePolicy) (googlepolicy.Policy, error) {
		return NewPolicyFromProto(pb)
	},
}

func init() {
	if err := googlepolicy.RegisterPolicyDriver(PolicyType, driver); err != nil {
		panic(err)
	}
}

// Policy represents a compiled, validated JournaldSourcePolicy ready for evaluation into Collector configuration.
type Policy struct {
	proto *policyv1alpha1.JournaldSourcePolicy
}

var (
	_ googlepolicy.Policy                     = (*Policy)(nil)
	_ googlepolicy.ComponentPolicy            = (*Policy)(nil)
	_ googlepolicy.SourcePolicy               = (*Policy)(nil)
	_ googlepolicy.ConsolidatableSourcePolicy = (*Policy)(nil)
)

// NewPolicyFromProto validates and compiles a JournaldSourcePolicy protobuf into a Policy.
func NewPolicyFromProto(pb *policyv1alpha1.JournaldSourcePolicy) (*Policy, error) {
	if pb == nil {
		return nil, ErrNilProto
	}
	p := &Policy{proto: proto.Clone(pb).(*policyv1alpha1.JournaldSourcePolicy)}
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

// PolicyType returns the registered policy type identifier ("journald_source").
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
	return nil
}

// Consolidate implements googlepolicy.ConsolidatableSourcePolicy.
// Since all journald policies collect from the host system journal with the same
// self-logging filter guardrail, consolidating them produces a single shared journald receiver.
func (p *Policy) Consolidate(policies []googlepolicy.SourcePolicy) (googlepolicy.SourcePolicy, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		return p, nil
	}
	for _, sp := range policies {
		jp, ok := sp.(*Policy)
		if !ok {
			return nil, fmt.Errorf("cannot consolidate policy of type %T into journald source policy", sp)
		}
		if err := jp.Validate(); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Evaluate produces the confmap for the journald receiver.
// It configures an inline Stanza filter operator to automatically drop the collector's
// own log lines across both Linux VM systemd services and containerized environments,
// preventing self-logging amplification loops, and parses systemd/syslog priority (body.PRIORITY)
// into OpenTelemetry severity levels.
func (p *Policy) Evaluate(_ context.Context) (*confmap.Conf, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("policy %q failed validation: %w", p.PolicyName(), err)
	}

	cfg := map[string]any{
		"receivers": map[string]any{
			DefaultReceiverID: map[string]any{
				"operators": []any{
					map[string]any{
						"type": "filter",
						"id":   "exclude_collector_logs",
						"expr": collectorLogFilterExpr,
					},
					map[string]any{
						"type":       "severity_parser",
						"id":         "parse_journald_priority",
						"if":         "body.PRIORITY != nil",
						"parse_from": "body.PRIORITY",
						"mapping": map[string]any{
							"fatal":  "0",
							"error3": "1",
							"error2": "2",
							"error":  "3",
							"warn":   "4",
							"info2":  "5",
							"info":   "6",
							"debug":  "7",
						},
					},
				},
			},
		},
	}
	return confmap.NewFromStringMap(cfg), nil
}

// LogReceiverIDs returns the component IDs of log receivers synthesized by this policy.
func (p *Policy) LogReceiverIDs() []component.ID {
	return []component.ID{component.MustNewID(DefaultReceiverID)}
}

// MetricReceiverIDs returns nil because JournaldSourcePolicy only handles the logs signal.
func (p *Policy) MetricReceiverIDs() []component.ID {
	return nil
}

// TraceReceiverIDs returns nil because JournaldSourcePolicy only handles the logs signal.
func (p *Policy) TraceReceiverIDs() []component.ID {
	return nil
}
