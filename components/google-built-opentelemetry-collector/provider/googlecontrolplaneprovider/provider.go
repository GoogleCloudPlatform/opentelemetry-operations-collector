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

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplane/policies/gcpdestination"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/components/google-built-opentelemetry-collector/provider/googlecontrolplane/policies/selfmetrics"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/confmap"
	"go.uber.org/zap"
)

const (
	schemeName = "googlecontrolplane"
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
	BuiltInSelfMetricsPolicy = &selfmetrics.SelfMetricsPolicy{Name: "default_self_metrics"}
)

const (
	innerSchemeFile      = "file"
	innerSchemeComponent = "component"
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

	fleetID := os.Getenv("FLEET_ID")
	if v, ok := ctx.Value("FLEET_ID").(string); ok && v != "" {
		fleetID = v
	}

	// Get a copy of the current active policy set from `pkg/googlepolicy`. If there is
	// no active policy set detected, we will only evaluate the built-in policies.
	activePolicySet := googlepolicy.ActivePolicySet()
	if activePolicySet == nil {
		activePolicySet = &googlepolicy.PolicySet{}
	}

	// Root conf object, each policy evaluation will merge into this confmap.
	conf := confmap.New()

	// Load any destination policies from activePolicySet. There can be 0 or 1. If there are more than
	// that, return an error.
	destPolicies := activePolicySet.LoadPoliciesOfClass(googlepolicy.PolicyClassDestination)
	if len(destPolicies) > 1 {
		return nil, fmt.Errorf("%w: found %d destination policies", ErrMultipleDestinationPolicies, len(destPolicies))
	}

	// Apply the active destination policy. If the previous step did not yield any from the set,
	// use the built-in one.
	var destPolicy googlepolicy.DestinationPolicy = BuiltInDestinationPolicy
	if len(destPolicies) == 1 {
		dp, ok := destPolicies[0].(googlepolicy.DestinationPolicy)
		if !ok {
			return nil, fmt.Errorf("destination policy %q does not implement DestinationPolicy", destPolicies[0].PolicyName())
		}
		destPolicy = dp
	}

	destConf, err := destPolicy.Evaluate(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate destination policy %q: %w", destPolicy.PolicyName(), err)
	}
	if err := conf.Merge(cleanConf(destConf)); err != nil {
		return nil, fmt.Errorf("failed to merge config for destination policy %q: %w", destPolicy.PolicyName(), err)
	}

	// Load all source policies from the active policy set.
	sourcePolicies := activePolicySet.LoadPoliciesOfClass(googlepolicy.PolicyClassSource)

	// If a selfmetrics policy is not found among the sources, add the built-in selfmetrics policy
	// to the sources.
	hasSelfMetrics := false
	for _, sp := range sourcePolicies {
		if sp.PolicyType() == selfmetrics.PolicyType {
			hasSelfMetrics = true
			break
		}
	}
	if !hasSelfMetrics {
		sourcePolicies = append(sourcePolicies, BuiltInSelfMetricsPolicy)
	}

	// Evaluate each source. Some policy types may be exceptional. The main exception is selfmetrics.
	// If a policy with type selfmetrics is found, call the ContextSetup method on it before evaluating.
	// Make the exceptional policy handling extendable for future policies that may need exceptions
	// (switch case on policy type probably).
	for _, sp := range sourcePolicies {
		srcPolicy, ok := sp.(googlepolicy.SourcePolicy)
		if !ok {
			return nil, fmt.Errorf("source policy %q does not implement SourcePolicy", sp.PolicyName())
		}

		var sourceConf *confmap.Conf
		var err error

		switch p := srcPolicy.(type) {
		case *selfmetrics.SelfMetricsPolicy:
			evalCtx := p.ContextSetup(ctx, collectorID, fleetID)
			sourceConf, err = p.Evaluate(evalCtx)
		default:
			sourceConf, err = srcPolicy.Evaluate(ctx)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to evaluate source policy %q: %w", sp.PolicyName(), err)
		}
		if err := conf.Merge(cleanConf(sourceConf)); err != nil {
			return nil, fmt.Errorf("failed to merge config for source policy %q: %w", sp.PolicyName(), err)
		}

		logsPipelines, err := srcPolicy.LogsPipelines(destPolicy.PreProcessLogIDs(), destPolicy.ExporterIDs(), destPolicy.ExtensionIDs())
		if err != nil {
			return nil, fmt.Errorf("failed to load logs pipelines for source policy %q: %w", sp.PolicyName(), err)
		}
		if logsPipelines != nil {
			if err := conf.Merge(cleanConf(logsPipelines)); err != nil {
				return nil, fmt.Errorf("failed to merge logs pipelines for source policy %q: %w", sp.PolicyName(), err)
			}
		}

		metricsPipelines, err := srcPolicy.MetricsPipelines(destPolicy.PreProcessMetricIDs(), destPolicy.ExporterIDs(), destPolicy.ExtensionIDs())
		if err != nil {
			return nil, fmt.Errorf("failed to load metrics pipelines for source policy %q: %w", sp.PolicyName(), err)
		}
		if metricsPipelines != nil {
			if err := conf.Merge(cleanConf(metricsPipelines)); err != nil {
				return nil, fmt.Errorf("failed to merge metrics pipelines for source policy %q: %w", sp.PolicyName(), err)
			}
		}

		tracesPipelines, err := srcPolicy.TracesPipelines(destPolicy.PreProcessTraceIDs(), destPolicy.ExporterIDs(), destPolicy.ExtensionIDs())
		if err != nil {
			return nil, fmt.Errorf("failed to load traces pipelines for source policy %q: %w", sp.PolicyName(), err)
		}
		if tracesPipelines != nil {
			if err := conf.Merge(cleanConf(tracesPipelines)); err != nil {
				return nil, fmt.Errorf("failed to merge traces pipelines for source policy %q: %w", sp.PolicyName(), err)
			}
		}
	}

	conf, err = p.ensureResourceDetection(conf)
	if err != nil {
		return nil, fmt.Errorf("failed to configure resource detection: %w", err)
	}

	// Validate the fully merged confmap.
	if err := confmap.Validate(conf); err != nil {
		return nil, fmt.Errorf("failed to validate merged configuration: %w", err)
	}

	return confmap.NewRetrieved(conf.ToStringMap())
}

// ensureResourceDetection ensures that every pipeline has a resourcedetection processor
// positioned prior to policies, and that all resourcedetection processors include the "gcp" detector.
func (p *provider) ensureResourceDetection(conf *confmap.Conf) (*confmap.Conf, error) {
	if conf == nil {
		conf = confmap.New()
	}
	rawMap := conf.ToStringMap()

	processorsRaw, _ := rawMap["processors"].(map[string]any)
	if processorsRaw == nil {
		processorsRaw = make(map[string]any)
		rawMap["processors"] = processorsRaw
	}

	// Find all processors of type resourcedetection.
	var rdIDs []string
	for key := range processorsRaw {
		parts := strings.Split(key, "/")
		if parts[0] == "resourcedetection" {
			rdIDs = append(rdIDs, key)
		}
	}

	// For any existing resourcedetection processor, ensure "gcp" is in its detectors (at the end),
	// and default timeout to 10s if not set.
	for _, rdID := range rdIDs {
		rdConfig, _ := processorsRaw[rdID].(map[string]any)
		if rdConfig == nil {
			rdConfig = make(map[string]any)
			processorsRaw[rdID] = rdConfig
		}
		if _, ok := rdConfig["timeout"]; !ok {
			rdConfig["timeout"] = "10s"
		}
		var detectors []string
		hasGCP := false
		if dList, ok := rdConfig["detectors"].([]any); ok {
			for _, d := range dList {
				s := fmt.Sprint(d)
				detectors = append(detectors, s)
				if s == "gcp" {
					hasGCP = true
				}
			}
		} else if dList, ok := rdConfig["detectors"].([]string); ok {
			for _, s := range dList {
				detectors = append(detectors, s)
				if s == "gcp" {
					hasGCP = true
				}
			}
		}
		if !hasGCP {
			detectors = append(detectors, "gcp")
		}
		rdConfig["detectors"] = detectors
	}

	// If no resourcedetection processor is defined, create the default one.
	defaultRDID := "resourcedetection"
	if len(rdIDs) == 0 {
		processorsRaw[defaultRDID] = map[string]any{
			"detectors": []string{"gcp"},
			"timeout":   "10s",
		}
		rdIDs = append(rdIDs, defaultRDID)
	}

	// Ensure every pipeline in service::pipelines has a resourcedetection processor at the beginning.
	serviceMap, _ := rawMap["service"].(map[string]any)
	if serviceMap != nil {
		pipelinesMap, _ := serviceMap["pipelines"].(map[string]any)
		for _, pipeVal := range pipelinesMap {
			pipeMap, ok := pipeVal.(map[string]any)
			if !ok {
				continue
			}
			var procs []string
			if pList, ok := pipeMap["processors"].([]any); ok {
				for _, p := range pList {
					procs = append(procs, fmt.Sprint(p))
				}
			} else if pList, ok := pipeMap["processors"].([]string); ok {
				procs = append(procs, pList...)
			}

			// Check if any processor in procs is of type resourcedetection.
			var nonRDProcs []string
			var foundRDID string

			for _, p := range procs {
				parts := strings.Split(p, "/")
				if parts[0] == "resourcedetection" {
					if foundRDID == "" {
						foundRDID = p
					}
					// Deduplicate: ignore subsequent resourcedetection processors in the same pipeline
				} else {
					nonRDProcs = append(nonRDProcs, p)
				}
			}

			targetRDID := defaultRDID
			if len(rdIDs) > 0 {
				targetRDID = rdIDs[0]
			}
			if foundRDID != "" {
				targetRDID = foundRDID
			}

			// Place targetRDID at the beginning (prior to policies and pre-export processors)
			pipeMap["processors"] = append([]string{targetRDID}, nonRDProcs...)
		}
	}

	return confmap.NewFromStringMap(rawMap), nil
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
