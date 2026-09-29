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

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/processor/googlepolicyprocessor"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider/policies/gcpdestination"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplaneprovider/policies/selfmetrics"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/event"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/logfilter"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/metricfilter"
	_ "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy/tracefilter"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/otelcol"
	"go.uber.org/zap"
)

const (
	schemeName = "googlecontrolplane"

	defaultFleetID = "test-fleet-01"
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

	manager googlepolicy.Manager
}

// NewFactory returns a new confmap.ProviderFactory that creates a Google Control Plane configuration provider.
func NewFactory() confmap.ProviderFactory {
	return confmap.NewProviderFactory(newProvider)
}

func newProvider(set confmap.ProviderSettings) confmap.Provider {
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

	// If there is already a manager and a Retrieve is requested, it can be because:
	//
	// * some other provider has triggered a full confmap resolution (the URI will be the same)
	// * the user configured two `googlecontrolplane` providers with different URIs
	//
	// The user can configure as many `component://` URIs as they want, but if it's anything
	// else we need to disambiguate between the two cases above, and fail if the user is trying
	// to configure additional policy source URIs.
	if target.Scheme != innerSchemeComponent && p.manager != nil {
		if p.manager.URI().String() != target.String() {
			return nil, fmt.Errorf("%q: %w: %s", uri, ErrManagerAlreadyConfigured, target)
		} else {
			return p.evaluateActivePolicySet(ctx)
		}
	}

	switch target.Scheme {
	case innerSchemeFile:
		p.manager, err = googlepolicy.NewFilePolicyManager(p.logger, target)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", uri, err)
		}
		if err := p.manager.Start(); err != nil {
			return nil, fmt.Errorf("%q: %w", uri, err)
		}
	case innerSchemeXDS:
		// The manager derives everything else it needs -- control plane address,
		// fleet ID and project -- from the URI itself.
		p.manager, err = googlepolicy.NewXDSPolicyManager(p.logger, target, CollectorID)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", uri, err)
		}
		// Start does not block on the control plane being reachable; an
		// unreachable one is retried in the background so the collector can
		// still come up on its built-in policies.
		if err := p.manager.Start(); err != nil {
			return nil, fmt.Errorf("%q: %w", uri, err)
		}
	case innerSchemeComponent:
	default:
		return nil, fmt.Errorf("%q: %w: %s", uri, ErrURIInvalidInnerScheme, target.Scheme)
	}

	return p.evaluateActivePolicySet(ctx)
}

func (p *provider) evaluateActivePolicySet(ctx context.Context) (*confmap.Retrieved, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	collectorID := CollectorID
	if v, ok := ctx.Value("COLLECTOR_ID").(string); ok && v != "" {
		collectorID = v
	}

	// Environment, then context, then the URI. The manager resolves the fleet
	// in the same order off the same query parameter, and must arrive at the
	// same answer: its value selects which fleet's policies this collector
	// receives, while this one attributes the collector's own telemetry.
	fleetID := os.Getenv("FLEET_ID")
	if v, ok := ctx.Value("FLEET_ID").(string); ok && v != "" {
		fleetID = v
	}
	if fleetID == "" && p.manager != nil {
		fleetID = googlepolicy.FleetIDFromURI(p.manager.URI())
	}
	if fleetID != "" {
		ctx = context.WithValue(ctx, "FLEET_ID", fleetID)
	}

	var projectID string
	if v, ok := ctx.Value("PROJECT_ID").(string); ok && v != "" {
		projectID = v
	}
	if projectID == "" && p.manager != nil && p.manager.URI() != nil {
		// A numeric value in ?project= is a GCP project number for the xdstp://
		// resource subscription, not an alphanumeric project ID. Leave PROJECT_ID
		// unset in that case so the self-metrics pipeline populates gcp.project_id
		// from the GCE resource detector's cloud.account.id.
		if candidate := p.manager.URI().Query().Get(googlepolicy.ProjectQueryParam); !googlepolicy.IsNumericProject(candidate) {
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
			if !isBuiltinFallback && p.manager != nil {
				p.manager.PolicyEvaluationResult(activePolicySet.RevisionID, nil)
			}
			return ret, nil
		}

		if isBuiltinErr {
			return nil, err
		}

		if !isBuiltinFallback && p.manager != nil {
			p.manager.PolicyEvaluationResult(activePolicySet.RevisionID, err)
		}

		if isBuiltinFallback {
			return nil, err
		}

		googlepolicy.RollbackActivePolicySet()
	}
}

func (p *provider) evaluatePolicySet(ctx context.Context, collectorID string, fleetID string, activePolicySet *googlepolicy.PolicySet) (*confmap.Retrieved, bool, error) {
	for _, fp := range activePolicySet.FailedPolicies {
		p.recordPolicyEvaluateError(ctx, fp.ID, activePolicySet, fp.Err)
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
		} else if err := mergeConf(conf, destConf); err != nil {
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
		if err := mergeConf(conf, destConf); err != nil {
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

	// Load all source policies from the active policy set.
	sourcePolicies := activePolicySet.LoadPoliciesOfClass(googlepolicy.PolicyClassSource)

	appliedSelfMetrics := false
	for _, sp := range sourcePolicies {
		policyConf, err := p.evaluateSingleSourcePolicy(ctx, collectorID, fleetID, sp, destPolicy, preProcessLogIDs, preProcessMetricIDs, preProcessTraceIDs)
		if err != nil {
			p.recordPolicyEvaluateError(ctx, sp.PolicyName(), activePolicySet, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			continue
		}
		if err := mergeConf(conf, policyConf); err != nil {
			err = fmt.Errorf("failed to merge config for source policy %q: %w", sp.PolicyName(), err)
			p.recordPolicyEvaluateError(ctx, sp.PolicyName(), activePolicySet, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			continue
		}
		if sp.PolicyType() == selfmetrics.PolicyType {
			appliedSelfMetrics = true
		}
	}

	// If no custom self_metrics policy was present or if it failed evaluation,
	// fall back to BuiltInSelfMetricsPolicy.
	if !appliedSelfMetrics {
		policyConf, err := p.evaluateSingleSourcePolicy(ctx, collectorID, fleetID, BuiltInSelfMetricsPolicy, destPolicy, preProcessLogIDs, preProcessMetricIDs, preProcessTraceIDs)
		if err != nil {
			p.recordPolicyEvaluateError(ctx, BuiltInSelfMetricsPolicy.PolicyName(), nil, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			return nil, true, err
		}
		if err := mergeConf(conf, policyConf); err != nil {
			err = fmt.Errorf("failed to merge config for source policy %q: %w", BuiltInSelfMetricsPolicy.PolicyName(), err)
			p.recordPolicyEvaluateError(ctx, BuiltInSelfMetricsPolicy.PolicyName(), nil, err, event.WithPolicyEvaluateErrorEventPolicyClass(event.PolicyClassSource))
			return nil, true, err
		}
	}

	// Validate the fully merged confmap.
	if err := confmap.Validate(conf); err != nil {
		err = fmt.Errorf("failed to validate merged configuration: %w", err)
		p.recordPolicySetInvalid(ctx, activePolicySet, err)
		return nil, false, err
	}

	ret, err := confmap.NewRetrieved(conf.ToStringMap())
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

func (p *provider) evaluateSingleSourcePolicy(
	ctx context.Context,
	collectorID string,
	fleetID string,
	sp googlepolicy.Policy,
	destPolicy googlepolicy.DestinationPolicy,
	preProcessLogIDs []component.ID,
	preProcessMetricIDs []component.ID,
	preProcessTraceIDs []component.ID,
) (*confmap.Conf, error) {
	srcPolicy, ok := sp.(googlepolicy.SourcePolicy)
	if !ok {
		return nil, fmt.Errorf("source policy %q does not implement SourcePolicy", sp.PolicyName())
	}

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
		return nil, fmt.Errorf("failed to evaluate source policy %q: %w", sp.PolicyName(), err)
	}

	policyConf := confmap.New()
	if err := mergeConf(policyConf, sourceConf); err != nil {
		return nil, fmt.Errorf("failed to merge config for source policy %q: %w", sp.PolicyName(), err)
	}

	var logProcIDs, metricProcIDs, traceProcIDs []component.ID
	if sp.PolicyType() == selfmetrics.PolicyType {
		logProcIDs = destPolicy.PreProcessLogIDs()
		metricProcIDs = destPolicy.PreProcessMetricIDs()
		traceProcIDs = destPolicy.PreProcessTraceIDs()
	} else {
		logProcIDs = preProcessLogIDs
		metricProcIDs = preProcessMetricIDs
		traceProcIDs = preProcessTraceIDs
	}

	logsPipelines, err := srcPolicy.LogsPipelines(logProcIDs, destPolicy.ExporterIDs(), destPolicy.ExtensionIDs())
	if err != nil {
		return nil, fmt.Errorf("failed to load logs pipelines for source policy %q: %w", sp.PolicyName(), err)
	}
	if logsPipelines != nil {
		if err := mergeConf(policyConf, logsPipelines); err != nil {
			return nil, fmt.Errorf("failed to merge logs pipelines for source policy %q: %w", sp.PolicyName(), err)
		}
	}

	metricsPipelines, err := srcPolicy.MetricsPipelines(metricProcIDs, destPolicy.ExporterIDs(), destPolicy.ExtensionIDs())
	if err != nil {
		return nil, fmt.Errorf("failed to load metrics pipelines for source policy %q: %w", sp.PolicyName(), err)
	}
	if metricsPipelines != nil {
		if err := mergeConf(policyConf, metricsPipelines); err != nil {
			return nil, fmt.Errorf("failed to merge metrics pipelines for source policy %q: %w", sp.PolicyName(), err)
		}
	}

	tracesPipelines, err := srcPolicy.TracesPipelines(traceProcIDs, destPolicy.ExporterIDs(), destPolicy.ExtensionIDs())
	if err != nil {
		return nil, fmt.Errorf("failed to load traces pipelines for source policy %q: %w", sp.PolicyName(), err)
	}
	if tracesPipelines != nil {
		if err := mergeConf(policyConf, tracesPipelines); err != nil {
			return nil, fmt.Errorf("failed to merge traces pipelines for source policy %q: %w", sp.PolicyName(), err)
		}
	}

	return policyConf, nil
}

// mergeConf merges src into dst, preserving the union of service::extensions.
//
// confmap.Merge replaces slices rather than appending to them, so when a second
// policy declares service::extensions it overwrites, rather than extends, the
// list the first one set. That failure is silent and destructive: the
// destination policy's authenticator disappears from the list, the collector
// never instantiates it, and startup fails with "authenticator not found" while
// the extension is still plainly visible under the top level extensions key.
//
// Maps do merge correctly, so only the service::extensions list needs this.
func mergeConf(dst *confmap.Conf, src *confmap.Conf) error {
	before := serviceExtensions(dst)
	if err := dst.Merge(cleanConf(src)); err != nil {
		return err
	}

	union := mergeStringsUnique(before, serviceExtensions(dst))
	if len(union) == 0 {
		return nil
	}

	// Merging again replaces the list a final time, now with the union.
	return dst.Merge(confmap.NewFromStringMap(map[string]any{
		"service": map[string]any{"extensions": union},
	}))
}

// serviceExtensions reads service::extensions as a list of component ID
// strings. The value arrives as []any after a round trip through
// confmap.ToStringMap, but may still be []string when set directly.
func serviceExtensions(c *confmap.Conf) []string {
	switch list := c.Get("service::extensions").(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, e := range list {
			out = append(out, fmt.Sprint(e))
		}
		return out
	default:
		return nil
	}
}

// mergeStringsUnique concatenates lists, dropping duplicates and preserving
// first-seen order so the generated config stays stable across runs.
func mergeStringsUnique(lists ...[]string) []any {
	seen := map[string]struct{}{}
	var out []any
	for _, list := range lists {
		for _, s := range list {
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
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

// Scheme returns the URI scheme supported by this provider.
func (*provider) Scheme() string {
	return schemeName
}

// Shutdown shuts down the provider and releases any held resources.
func (p *provider) Shutdown(context.Context) error {
	if p.manager != nil {
		p.manager.Stop()
	}
	return nil
}
