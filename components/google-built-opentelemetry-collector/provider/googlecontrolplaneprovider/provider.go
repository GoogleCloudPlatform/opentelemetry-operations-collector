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

package googlecontrolplaneprovider

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/processor/googlepolicyprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider/policies/gcpdestination"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider/policies/selfmetrics"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/event"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/journaldsource"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/logfilter"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/logtransform"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/metricfilter"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/tracefilter"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/tracesampling"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/featuregate"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/pipeline"
	"go.opentelemetry.io/collector/service"
	"go.opentelemetry.io/collector/service/pipelines"
	"go.uber.org/zap"
)

const (
	schemeName               = "googlecontrolplane"
	mergeAppendFeatureGateID = "confmap.enableMergeAppendOption"
)

var (
	// ErrURINotSupported is returned when the URI scheme does not match googlecontrolplane.
	ErrURINotSupported = errors.New("uri is not supported by googlecontrolplane provider")

	ErrURIInvalidScheme = errors.New("uri had an invalid confmap scheme")

	ErrURINoScheme = errors.New("uri had no scheme")

	ErrURIInvalidInnerURI = errors.New("uri is invalid")

	ErrURIInvalidInnerScheme = errors.New("inner scheme is unsupported")

	ErrURIMissingProtocol = errors.New("not protocol was specified")

	// ErrEmptyURI is returned when the URI is empty or missing a target.
	ErrEmptyURI = errors.New("uri cannot be empty")

	ErrManagerAlreadyConfigured = errors.New("the provider is already configured to manage policies")

	ErrMultipleDestinationPolicies = errors.New("more than one destination policy found")
)

var (
	BuiltInDestinationPolicy = &gcpdestination.GCPDestinationPolicy{Name: "default_gcp_destination"}
	BuiltInSelfMetricsPolicy = &selfmetrics.SelfMetricsPolicy{Name: "default_self_metrics", Port: 18888}
)

const (
	innerSchemeFile      = "file"
	innerSchemeComponent = "component"
	innerSchemeXDS       = "xds"
)

var _ confmap.Provider = (*provider)(nil)

type provider struct {
	logger *zap.Logger

	evalMu sync.Mutex

	mu          sync.Mutex
	stopped     bool
	notified    bool
	cachedRet   *confmap.Retrieved
	cachedRevID string
	manager     googlepolicy.Manager
	watcher     confmap.WatcherFunc
	lastCtx     context.Context
	lastConf    map[string]any
	watcherCh   googlepolicy.WatcherChannel
	stopCh      chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

// NewFactory returns a new confmap.ProviderFactory that creates a Google Control Plane configuration provider.
func NewFactory() confmap.ProviderFactory {
	return confmap.NewProviderFactory(newProvider)
}

func newProvider(set confmap.ProviderSettings) confmap.Provider {
	// Enable confmap.enableMergeAppendOption so confmap.Merge appends and
	// deduplicates service::extensions, service::**::receivers, and
	// service::**::exporters slices across policies and across multiple
	// confmap providers (e.g. file: + googlecontrolplane:).
	if err := featuregate.GlobalRegistry().Set(mergeAppendFeatureGateID, true); err != nil && set.Logger != nil {
		set.Logger.Warn("failed to enable confmap merge append feature gate", zap.Error(err))
	}
	return &provider{
		logger: set.Logger,
	}
}

func (p *provider) recordPolicyEvaluateError(ctx context.Context, policyID string, ps *googlepolicy.PolicySet, err error, extraOpts ...event.PolicyEvaluateErrorEventOption) {
	if p.logger == nil {
		return
	}
	var policySetID, revisionID string
	if ps != nil {
		policySetID = ps.PolicySetID
		revisionID = ps.RevisionID
	}
	opts := make([]event.PolicyEvaluateErrorEventOption, 0, 2+len(extraOpts))
	opts = append(opts,
		event.WithPolicyEvaluateErrorEventContext(ctx),
		event.WithPolicyEvaluateErrorEventErrorType(googlepolicy.ClassifyPolicyError(err)),
	)
	opts = append(opts, extraOpts...)
	event.RecordPolicyEvaluateErrorEvent(
		p.logger,
		err,
		policyID,
		policySetID,
		revisionID,
		opts...,
	)
}

func (p *provider) recordPolicySetInvalid(ctx context.Context, ps *googlepolicy.PolicySet, err error) {
	if p.logger == nil {
		return
	}
	var policySetID, revisionID string
	if ps != nil {
		policySetID = ps.PolicySetID
		revisionID = ps.RevisionID
	}
	event.RecordPolicySetInvalidEvent(
		p.logger,
		err,
		policySetID,
		revisionID,
		event.WithPolicySetInvalidEventContext(ctx),
	)
}

// Retrieve retrieves the configuration from the Google Control Plane provider for the given URI.
func (p *provider) Retrieve(ctx context.Context, uri string, watcher confmap.WatcherFunc) (*confmap.Retrieved, error) {
	// Try to generate a Collector ID. If it already exists, this is a no-op.
	if err := GenerateCollectorID(); err != nil {
		return nil, err
	}

	confmapScheme, uri, found := strings.Cut(uri, ":")
	if !found {
		return nil, fmt.Errorf("%q: %w: %w", uri, ErrURINotSupported, ErrURINoScheme)
	}
	if confmapScheme != schemeName {
		return nil, fmt.Errorf("%q: %w: %w: %s", uri, ErrURINotSupported, ErrURIInvalidScheme, confmapScheme)
	}

	if uri == "" {
		return nil, ErrEmptyURI
	}

	target, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("%q: %w: %w", uri, ErrURIInvalidInnerURI, err)
	}

	p.mu.Lock()
	manager := p.manager
	p.mu.Unlock()

	// If there is already a manager and a Retrieve is requested, it can be because:
	//
	// * some other provider has triggered a full confmap resolution (the URI will be the same)
	// * the user configured two `googlecontrolplane` providers with different URIs
	//
	// The user can configure as many `component://` URIs as they want, but if it's anything
	// else we need to disambiguate between the two cases above, and fail if the user is trying
	// to configure additional policy source URIs.
	if target.Scheme != innerSchemeComponent && manager != nil {
		if manager.URI().String() != target.String() {
			return nil, fmt.Errorf("%q: %w: %s", uri, ErrManagerAlreadyConfigured, target)
		}
		ret, err := p.getOrEvaluateActivePolicySet(ctx)
		if err != nil {
			return nil, err
		}
		p.setupWatcher(watcher, ctx, ret)
		return ret, nil
	}

	switch target.Scheme {
	case innerSchemeFile:
		mgr, err := googlepolicy.NewFilePolicyManager(p.logger, target)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", uri, err)
		}
		if err := mgr.Start(); err != nil {
			return nil, fmt.Errorf("%q: %w", uri, err)
		}
		p.mu.Lock()
		p.manager = mgr
		p.mu.Unlock()
	case innerSchemeXDS:
		// The manager derives everything else it needs -- control plane address,
		// fleet ID and project -- from the URI itself.
		mgr, err := googlepolicy.NewXDSPolicyManager(p.logger, target, CollectorID)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", uri, err)
		}
		// Start does not block on the control plane being reachable; an
		// unreachable one is retried in the background so the collector can
		// still come up on its built-in policies.
		if err := mgr.Start(); err != nil {
			return nil, fmt.Errorf("%q: %w", uri, err)
		}
		p.mu.Lock()
		p.manager = mgr
		p.mu.Unlock()
	case innerSchemeComponent:
	default:
		return nil, fmt.Errorf("%q: %w: %s", uri, ErrURIInvalidInnerScheme, target.Scheme)
	}

	ret, err := p.getOrEvaluateActivePolicySet(ctx)
	if err != nil {
		return nil, err
	}

	p.setupWatcher(watcher, ctx, ret)
	return ret, nil
}

func (p *provider) getOrEvaluateActivePolicySet(ctx context.Context) (*confmap.Retrieved, error) {
	p.mu.Lock()
	if p.cachedRet != nil && p.cachedRevID == googlepolicy.ActivePolicySetRevisionID() {
		ret := p.cachedRet
		p.cachedRet = nil
		p.cachedRevID = ""
		p.mu.Unlock()
		return ret, nil
	}
	p.cachedRet = nil
	p.cachedRevID = ""
	p.mu.Unlock()

	return p.evaluateActivePolicySet(ctx)
}

func (p *provider) evaluateActivePolicySet(ctx context.Context) (*confmap.Retrieved, error) {
	p.evalMu.Lock()
	defer p.evalMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	collectorID := CollectorID
	if v, ok := ctx.Value("COLLECTOR_ID").(string); ok && v != "" {
		collectorID = v
	}

	p.mu.Lock()
	manager := p.manager
	p.mu.Unlock()

	// Environment, then context, then the URI. The manager resolves the fleet
	// in the same order off the same query parameter, and must arrive at the
	// same answer: its value selects which fleet's policies this collector
	// receives, while this one attributes the collector's own telemetry.
	fleetID := os.Getenv("FLEET_ID")
	if v, ok := ctx.Value("FLEET_ID").(string); ok && v != "" {
		fleetID = v
	}
	if fleetID == "" && manager != nil {
		fleetID = googlepolicy.FleetIDFromURI(manager.URI())
	}
	if fleetID != "" {
		ctx = context.WithValue(ctx, "FLEET_ID", fleetID)
	}

	var projectID string
	if v, ok := ctx.Value("PROJECT_ID").(string); ok && v != "" {
		projectID = v
	}
	if projectID == "" && manager != nil && manager.URI() != nil {
		// A numeric value in ?project= is a GCP project number for the xdstp://
		// resource subscription, not an alphanumeric project ID. Leave PROJECT_ID
		// unset in that case so the self-metrics pipeline populates gcp.project_id
		// from the GCE resource detector's cloud.account.id.
		if candidate := manager.URI().Query().Get(googlepolicy.ProjectQueryParam); !googlepolicy.IsNumericProject(candidate) {
			projectID = candidate
		}
	}
	if projectID != "" {
		ctx = context.WithValue(ctx, "PROJECT_ID", projectID)
	}

	for {
		_, _, failed := googlepolicy.TakeActiveFailedPolicies()
		activePolicySet := googlepolicy.ActivePolicySet()
		isBuiltinFallback := activePolicySet == nil
		if isBuiltinFallback {
			activePolicySet = &googlepolicy.PolicySet{}
		} else if len(failed) > 0 {
			activePolicySet.FailedPolicies = failed
		}

		ret, isBuiltinErr, err := p.evaluatePolicySet(ctx, collectorID, fleetID, activePolicySet)
		if err == nil {
			if !isBuiltinFallback && manager != nil {
				manager.PolicyEvaluationResult(activePolicySet.RevisionID, nil)
			}
			return ret, nil
		}

		if isBuiltinErr {
			return nil, err
		}

		if !isBuiltinFallback && manager != nil {
			manager.PolicyEvaluationResult(activePolicySet.RevisionID, err)
		}

		if isBuiltinFallback {
			return nil, err
		}

		googlepolicy.RollbackActivePolicySet()
	}
}

func (p *provider) evaluatePolicySet(ctx context.Context, collectorID string, fleetID string, activePolicySet *googlepolicy.PolicySet) (*confmap.Retrieved, bool, error) {
	for _, fp := range activePolicySet.FailedPolicies {
		var extraOpts []event.PolicyEvaluateErrorEventOption
		if fp.Class != "" {
			extraOpts = append(extraOpts, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClass(fp.Class)))
		}
		p.recordPolicyEvaluateError(ctx, fp.ID, activePolicySet, fp.Err, extraOpts...)
	}

	// Root conf object, each policy evaluation will merge into this confmap.
	conf := confmap.New()

	// Load any destination policies from activePolicySet. There can be 0 or 1. If there are more than
	// that, return an error.
	destPolicies := activePolicySet.LoadPoliciesOfClass(googlepolicy.PolicyClassDestination)
	if len(destPolicies) > 1 {
		err := fmt.Errorf("%w: found %d destination policies", ErrMultipleDestinationPolicies, len(destPolicies))
		p.recordPolicySetInvalid(ctx, activePolicySet, err)
		return nil, false, err
	}

	var destPolicy googlepolicy.DestinationPolicy
	var destConf *confmap.Conf

	if len(destPolicies) == 1 {
		var err error
		destPolicy, destConf, err = p.evaluateDestinationPolicy(ctx, destPolicies[0])
		if err != nil {
			p.recordPolicyEvaluateError(ctx, destPolicies[0].PolicyName(), activePolicySet, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassDestination))
			destPolicy = nil
		} else if err := conf.Merge(destConf); err != nil {
			err = fmt.Errorf("failed to merge config for destination policy %q: %w", destPolicy.PolicyName(), err)
			p.recordPolicyEvaluateError(ctx, destPolicy.PolicyName(), activePolicySet, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassDestination))
			destPolicy = nil
		}
	}

	if destPolicy == nil {
		var err error
		destPolicy, destConf, err = p.evaluateDestinationPolicy(ctx, BuiltInDestinationPolicy)
		if err != nil {
			p.recordPolicyEvaluateError(ctx, BuiltInDestinationPolicy.PolicyName(), nil, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassDestination))
			return nil, true, err
		}
		if err := conf.Merge(destConf); err != nil {
			err = fmt.Errorf("failed to merge config for destination policy %q: %w", BuiltInDestinationPolicy.PolicyName(), err)
			p.recordPolicyEvaluateError(ctx, BuiltInDestinationPolicy.PolicyName(), nil, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassDestination))
			return nil, true, err
		}
	}

	googlePolicyFactory := googlepolicyprocessor.NewFactory()
	googlePolicyID := component.NewID(googlePolicyFactory.Type())

	googlePolicyConf := &otelcol.Config{
		Processors: map[component.ID]component.Config{
			googlePolicyID: googlePolicyFactory.CreateDefaultConfig(),
		},
	}
	gpCm := confmap.New()
	if err := gpCm.Marshal(googlePolicyConf); err != nil {
		return nil, true, fmt.Errorf("failed to marshal googlepolicy processor config: %w", err)
	}
	if err := conf.Merge(cleanConf(gpCm)); err != nil {
		return nil, true, fmt.Errorf("failed to merge googlepolicy processor config: %w", err)
	}

	preProcessLogIDs := append([]component.ID{googlePolicyID}, destPolicy.PreProcessLogIDs()...)
	preProcessMetricIDs := append([]component.ID{googlePolicyID}, destPolicy.PreProcessMetricIDs()...)
	preProcessTraceIDs := append([]component.ID{googlePolicyID}, destPolicy.PreProcessTraceIDs()...)

	// Configure GCP resource detection and transform for canonical workload pipelines
	rdType, _ := component.NewType("resourcedetection")
	rdID := component.NewIDWithName(rdType, "googlecontrolplane")
	rdCfg := map[string]any{
		"detectors": []string{"gcp"},
		"override":  false,
		"timeout":   10 * time.Second,
	}

	transformType, _ := component.NewType("transform")
	transformID := component.NewIDWithName(transformType, "googlecontrolplane")
	transformCfg := map[string]any{
		"error_mode": "ignore",
		"log_statements": []map[string]any{
			{
				"context": "resource",
				"statements": []string{
					`set(attributes["gcp.project_id"], attributes["cloud.account.id"]) where attributes["gcp.project_id"] == nil and attributes["cloud.account.id"] != nil`,
				},
			},
		},
		"metric_statements": []map[string]any{
			{
				"context": "resource",
				"statements": []string{
					`set(attributes["gcp.project_id"], attributes["cloud.account.id"]) where attributes["gcp.project_id"] == nil and attributes["cloud.account.id"] != nil`,
				},
			},
		},
		"trace_statements": []map[string]any{
			{
				"context": "resource",
				"statements": []string{
					`set(attributes["gcp.project_id"], attributes["cloud.account.id"]) where attributes["gcp.project_id"] == nil and attributes["cloud.account.id"] != nil`,
				},
			},
		},
	}

	workloadProcessorsConf := &otelcol.Config{
		Processors: map[component.ID]component.Config{
			rdID:        component.Config(rdCfg),
			transformID: component.Config(transformCfg),
		},
	}
	wpCm := confmap.New()
	if err := wpCm.Marshal(workloadProcessorsConf); err != nil {
		return nil, true, fmt.Errorf("failed to marshal workload resource processors config: %w", err)
	}
	if err := conf.Merge(cleanConf(wpCm)); err != nil {
		return nil, true, fmt.Errorf("failed to merge workload resource processors config: %w", err)
	}

	// Fallback nop receiver for canonical pipelines, allowing local config.yaml to append receivers
	nopType, _ := component.NewType("nop")
	nopReceiverID := component.NewIDWithName(nopType, "googlecontrolplane")
	nopConf := &otelcol.Config{
		Receivers: map[component.ID]component.Config{
			nopReceiverID: map[string]any{},
		},
	}
	nopCm := confmap.New()
	if err := nopCm.Marshal(nopConf); err != nil {
		return nil, true, fmt.Errorf("failed to marshal nop receiver config: %w", err)
	}
	if err := conf.Merge(cleanConf(nopCm)); err != nil {
		return nil, true, fmt.Errorf("failed to merge nop receiver config: %w", err)
	}

	activeLogReceivers := []component.ID{nopReceiverID}
	activeMetricReceivers := []component.ID{nopReceiverID}
	activeTraceReceivers := []component.ID{nopReceiverID}

	// Load all source policies from the active policy set.
	sourcePolicies := p.consolidateSourcePolicies(ctx, activePolicySet, activePolicySet.LoadPoliciesOfClass(googlepolicy.PolicyClassSource))

	appliedSelfMetrics := false
	for _, sp := range sourcePolicies {
		srcPolicy, ok := sp.(googlepolicy.SourcePolicy)
		if !ok {
			p.recordPolicyEvaluateError(ctx, sp.PolicyName(), activePolicySet, fmt.Errorf("source policy %q does not implement SourcePolicy", sp.PolicyName()), event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			continue
		}

		if dedicated, ok := srcPolicy.(googlepolicy.DedicatedPipelineSourcePolicy); ok {
			policyConf, err := p.evaluateDedicatedSourcePolicy(ctx, collectorID, fleetID, dedicated, destPolicy, preProcessLogIDs, preProcessMetricIDs, preProcessTraceIDs)
			if err != nil {
				p.recordPolicyEvaluateError(ctx, sp.PolicyName(), activePolicySet, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
				continue
			}
			if err := conf.Merge(policyConf); err != nil {
				p.recordPolicyEvaluateError(ctx, sp.PolicyName(), activePolicySet, fmt.Errorf("failed to merge config for source policy %q: %w", sp.PolicyName(), err), event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
				continue
			}
			if sp.PolicyType() == selfmetrics.PolicyType {
				appliedSelfMetrics = true
			}
			continue
		}

		policyConf, err := srcPolicy.Evaluate(ctx)
		if err != nil {
			p.recordPolicyEvaluateError(ctx, sp.PolicyName(), activePolicySet, fmt.Errorf("failed to evaluate source policy %q: %w", sp.PolicyName(), err), event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			continue
		}
		if err := conf.Merge(cleanConf(policyConf)); err != nil {
			p.recordPolicyEvaluateError(ctx, sp.PolicyName(), activePolicySet, fmt.Errorf("failed to merge config for source policy %q: %w", sp.PolicyName(), err), event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			continue
		}

		activeLogReceivers = append(activeLogReceivers, srcPolicy.LogReceiverIDs()...)
		activeMetricReceivers = append(activeMetricReceivers, srcPolicy.MetricReceiverIDs()...)
		activeTraceReceivers = append(activeTraceReceivers, srcPolicy.TraceReceiverIDs()...)
	}

	// If no custom self_metrics policy was present or if it failed evaluation,
	// fall back to BuiltInSelfMetricsPolicy.
	if !appliedSelfMetrics {
		policyConf, err := p.evaluateDedicatedSourcePolicy(ctx, collectorID, fleetID, BuiltInSelfMetricsPolicy, destPolicy, preProcessLogIDs, preProcessMetricIDs, preProcessTraceIDs)
		if err != nil {
			p.recordPolicyEvaluateError(ctx, BuiltInSelfMetricsPolicy.PolicyName(), nil, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			return nil, true, err
		}
		if err := conf.Merge(policyConf); err != nil {
			err = fmt.Errorf("failed to merge config for source policy %q: %w", BuiltInSelfMetricsPolicy.PolicyName(), err)
			p.recordPolicyEvaluateError(ctx, BuiltInSelfMetricsPolicy.PolicyName(), nil, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			return nil, true, err
		}
	}

	// Synthesize canonical shared workload pipelines
	canonicalLogsPipelineID := pipeline.NewIDWithName(pipeline.SignalLogs, "googlecontrolplane")
	canonicalMetricsPipelineID := pipeline.NewIDWithName(pipeline.SignalMetrics, "googlecontrolplane")
	canonicalTracesPipelineID := pipeline.NewIDWithName(pipeline.SignalTraces, "googlecontrolplane")

	canonicalLogProcessors := append([]component.ID{rdID, transformID, googlePolicyID}, destPolicy.PreProcessLogIDs()...)
	canonicalMetricProcessors := append([]component.ID{rdID, transformID, googlePolicyID}, destPolicy.PreProcessMetricIDs()...)
	canonicalTraceProcessors := append([]component.ID{rdID, transformID, googlePolicyID}, destPolicy.PreProcessTraceIDs()...)

	canonicalServiceConf := &otelcol.Config{
		Service: service.Config{
			Pipelines: pipelines.Config{
				canonicalLogsPipelineID: &pipelines.PipelineConfig{
					Receivers:  deduplicateComponentIDs(activeLogReceivers),
					Processors: canonicalLogProcessors,
					Exporters:  destPolicy.ExporterIDs(),
				},
				canonicalMetricsPipelineID: &pipelines.PipelineConfig{
					Receivers:  deduplicateComponentIDs(activeMetricReceivers),
					Processors: canonicalMetricProcessors,
					Exporters:  destPolicy.ExporterIDs(),
				},
				canonicalTracesPipelineID: &pipelines.PipelineConfig{
					Receivers:  deduplicateComponentIDs(activeTraceReceivers),
					Processors: canonicalTraceProcessors,
					Exporters:  destPolicy.ExporterIDs(),
				},
			},
		},
	}
	canonicalCm := confmap.New()
	if err := canonicalCm.Marshal(canonicalServiceConf); err != nil {
		return nil, true, fmt.Errorf("failed to marshal canonical pipelines config: %w", err)
	}
	if err := conf.Merge(cleanConf(canonicalCm)); err != nil {
		return nil, true, fmt.Errorf("failed to merge canonical pipelines config: %w", err)
	}

	// Validate the fully merged confmap.
	if err := confmap.Validate(conf); err != nil {
		err = fmt.Errorf("failed to validate merged configuration: %w", err)
		p.recordPolicySetInvalid(ctx, activePolicySet, err)
		return nil, false, err
	}

	confMap := conf.ToStringMap()
	ret, err := confmap.NewRetrieved(confMap)
	return ret, false, err
}

func (p *provider) evaluateDestinationPolicy(ctx context.Context, dp googlepolicy.Policy) (googlepolicy.DestinationPolicy, *confmap.Conf, error) {
	destPolicy, ok := dp.(googlepolicy.DestinationPolicy)
	if !ok {
		return nil, nil, fmt.Errorf("destination policy %q does not implement DestinationPolicy", dp.PolicyName())
	}
	destConf, err := destPolicy.Evaluate(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to evaluate destination policy %q: %w", destPolicy.PolicyName(), err)
	}
	return destPolicy, cleanConf(destConf), nil
}

func (p *provider) evaluateDedicatedSourcePolicy(
	ctx context.Context,
	collectorID string,
	fleetID string,
	srcPolicy googlepolicy.DedicatedPipelineSourcePolicy,
	destPolicy googlepolicy.DestinationPolicy,
	preProcessLogIDs []component.ID,
	preProcessMetricIDs []component.ID,
	preProcessTraceIDs []component.ID,
) (*confmap.Conf, error) {
	var sourceConf *confmap.Conf
	var err error

	switch pol := srcPolicy.(type) {
	case *selfmetrics.SelfMetricsPolicy:
		evalCtx := pol.ContextSetup(ctx, collectorID, fleetID)
		sourceConf, err = pol.Evaluate(evalCtx)
	default:
		sourceConf, err = srcPolicy.Evaluate(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate source policy %q: %w", srcPolicy.PolicyName(), err)
	}

	policyConf := confmap.New()
	if err := policyConf.Merge(cleanConf(sourceConf)); err != nil {
		return nil, fmt.Errorf("failed to merge config for source policy %q: %w", srcPolicy.PolicyName(), err)
	}

	logsPipelines, err := srcPolicy.LogsPipelines(preProcessLogIDs, destPolicy.ExporterIDs(), destPolicy.ExtensionIDs())
	if err != nil {
		return nil, fmt.Errorf("failed to load logs pipelines for source policy %q: %w", srcPolicy.PolicyName(), err)
	}
	if logsPipelines != nil {
		if err := policyConf.Merge(cleanConf(logsPipelines)); err != nil {
			return nil, fmt.Errorf("failed to merge logs pipelines for source policy %q: %w", srcPolicy.PolicyName(), err)
		}
	}

	metricsPipelines, err := srcPolicy.MetricsPipelines(preProcessMetricIDs, destPolicy.ExporterIDs(), destPolicy.ExtensionIDs())
	if err != nil {
		return nil, fmt.Errorf("failed to load metrics pipelines for source policy %q: %w", srcPolicy.PolicyName(), err)
	}
	if metricsPipelines != nil {
		if err := policyConf.Merge(cleanConf(metricsPipelines)); err != nil {
			return nil, fmt.Errorf("failed to merge metrics pipelines for source policy %q: %w", srcPolicy.PolicyName(), err)
		}
	}

	tracesPipelines, err := srcPolicy.TracesPipelines(preProcessTraceIDs, destPolicy.ExporterIDs(), destPolicy.ExtensionIDs())
	if err != nil {
		return nil, fmt.Errorf("failed to load traces pipelines for source policy %q: %w", srcPolicy.PolicyName(), err)
	}
	if tracesPipelines != nil {
		if err := policyConf.Merge(cleanConf(tracesPipelines)); err != nil {
			return nil, fmt.Errorf("failed to merge traces pipelines for source policy %q: %w", srcPolicy.PolicyName(), err)
		}
	}

	return policyConf, nil
}

func deduplicateComponentIDs(ids []component.ID) []component.ID {
	seen := make(map[component.ID]bool, len(ids))
	result := make([]component.ID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

// consolidateSourcePolicies groups source policies by policy type and consolidates
// policies that implement ConsolidatableSourcePolicy into a single shared receiver policy.
func (p *provider) consolidateSourcePolicies(ctx context.Context, activePolicySet *googlepolicy.PolicySet, policies []googlepolicy.Policy) []googlepolicy.Policy {
	groups := make(map[string][]googlepolicy.SourcePolicy)
	var typeOrder []string
	for _, pol := range policies {
		sp, ok := pol.(googlepolicy.SourcePolicy)
		if !ok {
			p.recordPolicyEvaluateError(ctx, pol.PolicyName(), activePolicySet, fmt.Errorf("policy %q does not implement SourcePolicy", pol.PolicyName()), event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			continue
		}
		t := sp.PolicyType()
		if _, exists := groups[t]; !exists {
			typeOrder = append(typeOrder, t)
		}
		groups[t] = append(groups[t], sp)
	}

	var consolidated []googlepolicy.Policy
	for _, t := range typeOrder {
		group := groups[t]
		if len(group) == 0 {
			continue
		}
		if _, ok := group[0].(googlepolicy.ConsolidatableSourcePolicy); ok && len(group) > 1 {
			var validPolicies []googlepolicy.SourcePolicy
			for _, sp := range group {
				if err := sp.Validate(); err != nil {
					p.recordPolicyEvaluateError(ctx, sp.PolicyName(), activePolicySet, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
					continue
				}
				validPolicies = append(validPolicies, sp)
			}
			if len(validPolicies) == 0 {
				continue
			}
			if len(validPolicies) == 1 {
				consolidated = append(consolidated, validPolicies[0])
				continue
			}
			consolidatable, ok := validPolicies[0].(googlepolicy.ConsolidatableSourcePolicy)
			if !ok {
				for _, sp := range validPolicies {
					consolidated = append(consolidated, sp)
				}
				continue
			}
			merged, err := consolidatable.Consolidate(validPolicies)
			if err != nil {
				for _, sp := range validPolicies {
					p.recordPolicyEvaluateError(ctx, sp.PolicyName(), activePolicySet, fmt.Errorf("failed to consolidate %s policies: %w", t, err), event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
				}
				continue
			}
			consolidated = append(consolidated, merged)
		} else {
			for _, sp := range group {
				consolidated = append(consolidated, sp)
			}
		}
	}
	return consolidated
}

func cleanConf(c *confmap.Conf) *confmap.Conf {
	if c == nil {
		return confmap.New()
	}
	m := c.ToStringMap()
	cleanNilEntries(m)
	return confmap.NewFromStringMap(m)
}

func cleanNilEntries(m map[string]any) {
	for k, v := range m {
		if v == nil {
			delete(m, k)
			continue
		}
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Map, reflect.Slice, reflect.Ptr, reflect.Interface:
			if rv.IsNil() {
				delete(m, k)
				continue
			}
		}
		if subMap, ok := v.(map[string]any); ok {
			cleanNilEntries(subMap)
		}
	}
}

func (p *provider) setupWatcher(watcher confmap.WatcherFunc, ctx context.Context, ret *confmap.Retrieved) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	p.watcher = watcher
	p.lastCtx = ctx
	p.notified = false
	if ret != nil {
		if conf, err := ret.AsConf(); err == nil {
			p.lastConf = conf.ToStringMap()
		}
	}
	if p.stopCh == nil && watcher != nil {
		stopCh := make(chan struct{})
		watcherCh := googlepolicy.RegisterWatcherChannel()
		p.stopCh = stopCh
		p.watcherCh = watcherCh
		p.wg.Add(1)
		go p.watchPolicies(stopCh, watcherCh)
	}
}

func (p *provider) watchPolicies(stopCh <-chan struct{}, watcherCh googlepolicy.WatcherChannel) {
	defer p.wg.Done()
	for {
		select {
		case <-stopCh:
			return
		case _, ok := <-watcherCh:
			if !ok {
				return
			}
			select {
			case <-stopCh:
				return
			default:
			}
			p.onPolicySetUpdated()
		}
	}
}

func (p *provider) onPolicySetUpdated() {
	p.mu.Lock()
	if p.stopped || p.notified || p.watcher == nil {
		p.mu.Unlock()
		return
	}
	oldConf := p.lastConf
	ctx := p.lastCtx
	p.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	ret, err := p.evaluateActivePolicySet(ctx)
	if err != nil {
		if p.logger != nil {
			p.logger.Warn("failed to evaluate active policy set on update", zap.Error(err))
		}
		return
	}

	newConf, err := ret.AsConf()
	if err != nil {
		if p.logger != nil {
			p.logger.Warn("failed to extract conf from retrieved config on update", zap.Error(err))
		}
		return
	}
	newConfMap := newConf.ToStringMap()

	// Only trigger collector reload if the generated configuration actually changed.
	// If only transformation policies (log_filter, log_transform, etc.) changed,
	// the provider's confmap remains identical and googlepolicyprocessor handles
	// the update in-memory without restarting collector pipelines.
	if reflect.DeepEqual(oldConf, newConfMap) {
		return
	}

	p.mu.Lock()
	if p.stopped || p.notified {
		p.mu.Unlock()
		return
	}
	p.notified = true
	p.cachedRet = ret
	p.cachedRevID = googlepolicy.ActivePolicySetRevisionID()
	watcher := p.watcher
	p.mu.Unlock()

	if p.logger != nil {
		p.logger.Info("detected change in policy-generated collector configuration, triggering reload")
	}
	if watcher != nil {
		watcher(&confmap.ChangeEvent{})
	}
}

// Scheme returns the URI scheme supported by this provider.
func (*provider) Scheme() string {
	return schemeName
}

// Shutdown shuts down the provider and releases any held resources.
func (p *provider) Shutdown(context.Context) error {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.stopped = true
		p.cachedRet = nil
		p.cachedRevID = ""
		if p.stopCh != nil {
			close(p.stopCh)
			p.stopCh = nil
		}
		p.mu.Unlock()
		p.wg.Wait()
		if p.watcherCh != nil {
			googlepolicy.UnregisterWatcherChannel(p.watcherCh)
			p.watcherCh = nil
		}
	})
	p.mu.Lock()
	manager := p.manager
	p.manager = nil
	p.mu.Unlock()
	if manager != nil {
		manager.Stop()
	}
	return nil
}
