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
	"strings"

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
)

const (
	innerSchemeFile      = "file"
	innerSchemeComponent = "component"
)

var _ confmap.Provider = (*provider)(nil)

type provider struct {
	logger *zap.Logger

	manager policyManager
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
			return p.evaluateActivePolicySet()
		}
	}

	switch target.Scheme {
	case innerSchemeFile:
		p.manager, err = NewFilePolicyManager(p.logger, target)
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

	return p.evaluateActivePolicySet()
}

func (p *provider) evaluateActivePolicySet() (*confmap.Retrieved, error) {
	// 0. This is a recursive function. If there is no longer an active policy set in `pkg/googlepolicy`,
	// we respond with the base case of only evaluating the built-in policies.

	// 1. Get a copy of the current active policy set from `pkg/googlepolicy`.

	// 2. Load any destination policies. There can be 0 or 1. If there are more than
	// that, return an error.

	// 3. Apply the active destination policies. If the previous step did not yield any,
	// use the built-in one.

	// 4.
	return nil, nil
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

type policyManager interface {
	Start() error
	Stop() error
	URI() *url.URL
	PolicyEvaluationResult(revisionID string, err error)
}
