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

	"go.opentelemetry.io/collector/confmap"
)

const schemeName = "googlecontrolplane"

type googleControlPlaneProvider struct{}

func (p *googleControlPlaneProvider) Retrieve(ctx context.Context, uri string, watcher confmap.WatcherFunc) (*confmap.Retrieved, error) {
	return nil, nil
}

func (p *googleControlPlaneProvider) Scheme() string {
	return schemeName
}

func (p *googleControlPlaneProvider) Shutdown(ctx context.Context) error {
	return nil
}

// NewFactory returns a new confmap.ProviderFactory for googlecontrolplane provider.
func NewFactory() confmap.ProviderFactory {
	return confmap.NewProviderFactory(func(set confmap.ProviderSettings) confmap.Provider {
		return &googleControlPlaneProvider{}
	})
}
