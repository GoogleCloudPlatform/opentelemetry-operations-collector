// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package filterprocessor

import (
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/internal/ottlfuncs"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottllog"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/filterprocessor"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/processor"
)

var componentType component.Type = component.MustNewType("filter")

// NewFactory create a factory for the transform processor.
func NewFactory() processor.Factory {
	additionaLogFunctions := []ottl.Factory[ottllog.TransformContext]{
		ottlfuncs.NewExtractPatternsRubyRegexFactory[ottllog.TransformContext](),
		ottlfuncs.NewIsMatchRubyRegexFactory[ottllog.TransformContext](),
		ottlfuncs.NewToValuesFactory[ottllog.TransformContext](),
	}
	return filterprocessor.NewFactoryWithOptions(
		filterprocessor.WithLogFunctions(filterprocessor.DefaultLogFunctions()),
		filterprocessor.WithLogFunctions(additionaLogFunctions),
	)
}
