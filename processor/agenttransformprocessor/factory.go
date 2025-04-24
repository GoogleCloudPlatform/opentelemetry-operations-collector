// Copyright 2022 Google LLC
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

package agenttransformprocessor

import (
	"fmt"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/processor/agenttransformprocessor/internal/logs"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/ottl/contexts/ottllog"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/transformprocessor"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/processor"
)

type CustomFactory struct {
	processor.Factory
}

func (f CustomFactory) CreateDefaultConfig() component.Config {
	fmt.Println("Start agenttransformprocessor CreateDefaultConfig")
	config := f.Factory.CreateDefaultConfig()
	tConfig, ok := config.(transformprocessor.Config)
	if ok {
		tConfig.AdditionalOTTLFunc = []ottl.Factory[ottllog.TransformContext]{logs.NewExtractPatternsRubyRegexFactory[ottllog.TransformContext]()}
		fmt.Println("End agenttransformprocessor CreateDefaultConfig with func", tConfig)
		return tConfig
	}
	fmt.Println("End agenttransformprocessor CreateDefaultConfig no func", config)
	return config
}

// NewFactory create a factory for the transform processor.
func NewFactory() processor.Factory {
	fmt.Println("Start agenttransformprocessor NewFactory")
	oldFactory := transformprocessor.NewFactory()
	customFactory := CustomFactory{Factory: oldFactory}

	fmt.Println("End agenttransformprocessor NewFactory")
	return customFactory
}
