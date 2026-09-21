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

package googlepolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var (
	ErrPolicyNotFound           = errors.New("policy not found")
	ErrPolicyTypeFieldMissing   = errors.New("critical policy field 'type' was not found")
	ErrPolicyTypeFieldWrongType = errors.New("critical policy field 'type' is not a string")
)

// PolicyClass is the class of a policy. https://xkcd.com/703/
type PolicyClass string

const (
	PolicyClassDestination    PolicyClass = "destination"
	PolicyClassSource         PolicyClass = "source"
	PolicyClassTransformation PolicyClass = "transformation"
)

// Policy is an interface that all policies of all classes implement.
type Policy interface {
	PolicyName() string
	PolicyType() string
	PolicyClass() PolicyClass
	Validate() error
}

// EvalResult represents the outcome of evaluating a telemetry item against a policy.
type EvalResult uint8

const (
	// EvalNoMatch indicates the policy conditions did not match the item.
	EvalNoMatch EvalResult = iota
	// EvalKeep indicates the policy matched and explicitly keeps the item (ACTION_KEEP).
	EvalKeep
	// EvalDrop indicates the policy matched and drops the item (ACTION_DROP).
	EvalDrop
)

// LogContext holds the contextual information needed to evaluate a single log record.
type LogContext struct {
	Record            plog.LogRecord
	Resource          pcommon.Resource
	Scope             pcommon.InstrumentationScope
	ResourceSchemaURL string
	ScopeSchemaURL    string
}

// LogPolicyEvaluator is implemented by transformation policies that evaluate
// log records (e.g., LogFilterPolicy in pkg/googlepolicy/logfilter).
type LogPolicyEvaluator interface {
	TransformationPolicy
	EvaluateLog(ctx LogContext) EvalResult
}

// MetricContext holds the contextual information needed to evaluate a single
// metric datapoint. DatapointAttributes is empty when the policy set is
// evaluated at instrument level only.
type MetricContext struct {
	Metric                 pmetric.Metric
	DatapointAttributes    pcommon.Map
	AggregationTemporality pmetric.AggregationTemporality
	Resource               pcommon.Resource
	Scope                  pcommon.InstrumentationScope
	ResourceSchemaURL      string
	ScopeSchemaURL         string
}

// MetricPolicyEvaluator is implemented by transformation policies that evaluate
// metric datapoints (e.g., MetricFilterPolicy in pkg/googlepolicy/metricfilter).
type MetricPolicyEvaluator interface {
	TransformationPolicy
	EvaluateMetric(ctx MetricContext) EvalResult
	// IsDatapointLevel reports whether any matcher inspects datapoint
	// attributes. Policies that do not can be evaluated once per instrument
	// instead of once per datapoint.
	IsDatapointLevel() bool
}

// TraceContext holds the contextual information needed to evaluate a single span.
type TraceContext struct {
	Span              ptrace.Span
	Resource          pcommon.Resource
	Scope             pcommon.InstrumentationScope
	ResourceSchemaURL string
	ScopeSchemaURL    string
}

// TracePolicyEvaluator is implemented by transformation policies that evaluate
// spans (e.g., TraceFilterPolicy in pkg/googlepolicy/tracefilter).
type TracePolicyEvaluator interface {
	TransformationPolicy
	EvaluateTrace(ctx TraceContext) EvalResult
}

// ComponentPolicy is an extended interface that any policy that produces config
// will implement (currently Destination and Source policies).
type ComponentPolicy interface {
	Policy
	Evaluate(ctx context.Context) (*confmap.Conf, error)
}

// SourcePolicy is an extended interface that any Source policy will implement.
// It allows extra steps for producing individual pipelines out of the components
// generated during `Evaluate`.
type SourcePolicy interface {
	ComponentPolicy
	LogsPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Conf, error)
	MetricsPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Conf, error)
	TracesPipelines(preExportProcessors []component.ID, exporters []component.ID, extensions []component.ID) (*confmap.Conf, error)
}

// DestinationPolicy is an extended interface that any Destination policy
// will implement.
type DestinationPolicy interface {
	ComponentPolicy
	ExporterIDs() []component.ID
	PreProcessMetricIDs() []component.ID
	PreProcessLogIDs() []component.ID
	PreProcessTraceIDs() []component.ID
	ExtensionIDs() []component.ID
}

// Signal represents an OpenTelemetry telemetry signal type.
type Signal string

const (
	SignalLogs    Signal = "logs"
	SignalMetrics Signal = "metrics"
	SignalTraces  Signal = "traces"
)

// TransformationPolicy is an extended interface that any Transformation policy
// will implement. It represents policies that transform, filter, or sample
// telemetry data in the collector pipelines.
type TransformationPolicy interface {
	Policy
	TargetSignals() []Signal
}

// PolicyDriver is the interface that is used to load a policy
// object of a known policy type.
type PolicyDriver interface {
	LoadPolicy(raw map[string]any) (Policy, error)
}

// ProtoPolicyDriver is an optional extension of PolicyDriver for policies that
// have a wire proto. Implementing it registers the proto's message name as a
// route to the driver's policy type.
//
// This is what lets a policy delivered over xDS reach a driver at all. Such a
// policy arrives as a bare google.protobuf.Any whose body carries no "type"
// field -- LogFilterPolicy and friends have no such field -- so the message
// identity in the type URL is the only routing information available. Every
// other delivery path has the policy type written out by whoever authored the
// config, which is the assumption MakePolicySet documents.
//
// Drivers whose policy has no proto representation, such as the built-in
// source and destination policies that are plain Go structs, simply do not
// implement this and remain unreachable over xDS.
type ProtoPolicyDriver interface {
	PolicyDriver

	// PolicyProto returns an empty instance of the policy's proto message.
	// Only its descriptor is read; the value is never populated or retained.
	PolicyProto() proto.Message

	// LoadPolicyProto builds a Policy from an already-decoded policy proto.
	// The message is the one PolicyProto declares; a driver handed anything
	// else must reject it rather than guess.
	//
	// This is required rather than optional so that a driver which declares a
	// proto cannot silently fall back to the map path: that fallback would be
	// invisible, and it is the path this method exists to avoid.
	LoadPolicyProto(msg proto.Message) (Policy, error)
}

// PolicySet is the translation of a set of policies received from a given source
// into internal representations that the Collector can use to evaluate.
type PolicySet struct {
	Policies   map[string]*PolicySetEntry
	RevisionID string
	ReceivedAt time.Time
}

// PolicySetEntry contains the given policy object and a mark for whether it has
// been applied or not, and the error that ocurred if it failed evaluation.
type PolicySetEntry struct {
	PolicyObj Policy
	Processed bool
	Error     error
}

// MakePolicySet builds a PolicySet from raw policy configs on a best-effort
// basis: every config that cannot be turned into a valid Policy is skipped and
// recorded, and the successfully loaded ones are still returned.
//
// The returned PolicySet is always non-nil, so a non-nil error does not mean
// there is nothing usable. Callers decide how strict to be: compare
// len(ps.Policies) against the number of inputs to detect a partial set, and
// reject the whole thing if that is not acceptable.
func MakePolicySet(revisionID string, rawPolicyConfigs []map[string]any) (*PolicySet, error) {
	return makePolicySet(revisionID, rawPolicyConfigs, func(i int, rawPolicyConfig map[string]any) (Policy, error) {
		// HARD CODED ASSUMPTION: for each entry the map contains a top-level
		// field called `type` that contains the policy type. This will match up
		// with the registered PolicyDriver.
		//
		// This envelope is what authored config uses to say which driver it
		// means. Policies that arrive as a proto carry their identity in the
		// message type instead and go through MakePolicySetFromProtos, which
		// needs none of this.
		policyTypeRaw, ok := rawPolicyConfig["type"]
		if !ok {
			return nil, fmt.Errorf("%w for policy at index %d", ErrPolicyTypeFieldMissing, i)
		}
		policyType, ok := policyTypeRaw.(string)
		if !ok {
			return nil, fmt.Errorf("%w for policy at index %d", ErrPolicyTypeFieldWrongType, i)
		}
		return LoadPolicy(policyType, rawPolicyConfig)
	})
}

// MakePolicySetFromProtos builds a PolicySet from decoded policy protos, on the
// same best-effort terms as MakePolicySet: see its documentation for how to
// read the returned set and error together.
//
// Each policy is routed to a driver by its message type, so no "type" field is
// read from -- or needed in -- the policy body.
func MakePolicySetFromProtos(revisionID string, msgs []proto.Message) (*PolicySet, error) {
	return makePolicySet(revisionID, msgs, func(i int, msg proto.Message) (Policy, error) {
		p, err := LoadPolicyFromProto(msg)
		if err != nil {
			// A bare proto has no name of its own to report, so the position in
			// the revision is the only thing that identifies which one failed.
			return nil, fmt.Errorf("policy at index %d: %w", i, err)
		}
		return p, nil
	})
}

// makePolicySet accumulates whatever load can make sense of, collecting the
// failures rather than stopping at the first one. It is the shared body of
// MakePolicySet and MakePolicySetFromProtos, which differ only in how a single
// input is turned into a Policy.
func makePolicySet[T any](revisionID string, inputs []T, load func(int, T) (Policy, error)) (*PolicySet, error) {
	ps := &PolicySet{
		Policies:   make(map[string]*PolicySetEntry, len(inputs)),
		RevisionID: revisionID,
		ReceivedAt: time.Now(),
	}

	errs := make([]error, 0, len(inputs))

	for i, in := range inputs {
		p, err := load(i, in)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		ps.Policies[p.PolicyName()] = &PolicySetEntry{PolicyObj: p}
	}

	return ps, errors.Join(errs...)
}

// LoadPoliciesOfClass does what it says on the box.
func (ps *PolicySet) LoadPoliciesOfClass(class PolicyClass) []Policy {
	foundPolicies := make([]Policy, 0, len(ps.Policies))
	for _, pse := range ps.Policies {
		if pse.PolicyObj.PolicyClass() == class {
			foundPolicies = append(foundPolicies, pse.PolicyObj)
		}
	}
	slices.SortFunc(foundPolicies, func(a, b Policy) int {
		return strings.Compare(a.PolicyName(), b.PolicyName())
	})
	return foundPolicies
}

// TransformationPolicies returns all policies in the set that implement TransformationPolicy.
func (ps *PolicySet) TransformationPolicies() []TransformationPolicy {
	foundPolicies := make([]TransformationPolicy, 0)
	for _, pse := range ps.Policies {
		if pse.PolicyObj.PolicyClass() == PolicyClassTransformation {
			if tp, ok := pse.PolicyObj.(TransformationPolicy); ok {
				foundPolicies = append(foundPolicies, tp)
			}
		}
	}
	slices.SortFunc(foundPolicies, func(a, b TransformationPolicy) int {
		return strings.Compare(a.PolicyName(), b.PolicyName())
	})
	return foundPolicies
}

// MarkPolicySuccessful will mark a policy in the set as processed without error.
func (ps *PolicySet) MarkPolicySuccesful(p Policy) {
	ps.markPolicy(p, nil)
}

// MarkPolicyFailed will mark a policy in the set as processed with the given error.
func (ps *PolicySet) MarkPolicyFailed(p Policy, err error) {
	ps.markPolicy(p, err)
}

func (ps *PolicySet) markPolicy(p Policy, err error) {
	pse, ok := ps.Policies[p.PolicyName()]
	if !ok {
		panic("Attempted to mark a policy as applied when not present in the set. This code state should be impossible.")
	}
	pse.Processed = true
	pse.Error = err
}

// Clone returns a deep copy of the PolicySetEntry, or nil if pse is nil.
func (pse *PolicySetEntry) Clone() *PolicySetEntry {
	if pse == nil {
		return nil
	}
	cp := *pse
	return &cp
}

// Clone returns a deep copy of the PolicySet, or nil if ps is nil.
func (ps *PolicySet) Clone() *PolicySet {
	if ps == nil {
		return nil
	}
	clone := &PolicySet{
		RevisionID: ps.RevisionID,
		ReceivedAt: ps.ReceivedAt,
	}
	if ps.Policies != nil {
		clone.Policies = make(map[string]*PolicySetEntry, len(ps.Policies))
		for k, v := range ps.Policies {
			clone.Policies[k] = v.Clone()
		}
	}
	return clone
}

// GenericDriver is a driver that can be used
// to register policy support when all you need
// is a simple mapstructure unmarshal.
type GenericDriver[P Policy] struct{}

// LoadPolicy decodes the raw policy config into P. The "type" routing envelope
// key has already been removed by LoadPolicy in the registry, so strict
// mapstructure decoding succeeds.
func (gd *GenericDriver[P]) LoadPolicy(raw map[string]any) (Policy, error) {
	var p P
	conf := confmap.NewFromStringMap(raw)
	if err := conf.Unmarshal(&p); err != nil {
		return nil, err
	}
	return p, nil
}

// ProtoDriver registers a policy whose canonical form is a proto message,
// serving both delivery paths from one constructor: a policy that arrives as a
// proto is handed straight to New, and an authored config is unmarshalled into
// the same message first.
//
// It is generic over the message rather than written out per policy because
// every such driver was otherwise the same twenty lines, differing only in the
// message type and the constructor to call.
type ProtoDriver[P proto.Message] struct {
	// New compiles a validated proto into the package's Policy. It is a field
	// rather than a method because the constructor is package-specific and
	// cannot be reached from the type parameter alone.
	New func(P) (Policy, error)
}

// newP returns a fresh, empty P.
//
// P is a pointer type, so its zero value is a typed nil, and protobuf-go's
// generated ProtoReflect tolerates a nil receiver -- it resolves the descriptor
// from the type, not the value. That makes an instance reachable from the type
// parameter alone, with no zero-value field to store.
func (d ProtoDriver[P]) newP() P {
	var zero P
	return zero.ProtoReflect().New().Interface().(P)
}

// PolicyProto returns an empty instance of P for the registry to route on.
func (d ProtoDriver[P]) PolicyProto() proto.Message { return d.newP() }

// LoadPolicyProto builds the policy directly from the decoded message, which is
// the whole point of the type: no serialization happens on this path.
func (d ProtoDriver[P]) LoadPolicyProto(msg proto.Message) (Policy, error) {
	typed, ok := msg.(P)
	if !ok {
		return nil, fmt.Errorf("%w: expected %T, got %T", ErrPolicyProtoMismatch, d.newP(), msg)
	}
	return d.New(typed)
}

// LoadPolicy decodes an authored config into P and loads it. Unknown fields are
// discarded so that a config written against a newer schema still loads what
// this build understands, rather than being rejected outright.
func (d ProtoDriver[P]) LoadPolicy(raw map[string]any) (Policy, error) {
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal raw policy map to json: %w", err)
	}

	pb := d.newP()
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(jsonBytes, pb); err != nil {
		return nil, fmt.Errorf("failed to unmarshal into %T: %w", pb, err)
	}

	return d.New(pb)
}
