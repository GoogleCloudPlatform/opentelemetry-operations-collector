// Copyright 2025 Google LLC
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

package main

import (
	"errors"
	"flag"
	"strings"
)

var (
	flagSpec            = flag.String("spec", "", "The distribution specification to use")
	flagForce           = flag.Bool("force", false, "Force generate even if there are no differences detected")
	flagVerbose         = flag.Bool("v", false, "Verbose output")
	flagRegistry        = newArrayFlag("registry", "Provide additional component registries")
	flagCustomTemplates = flag.String("custom_templates", "", "Provide a set of custom templates for this distribution")
	flagOtelConfig      = flag.String("otel_config", "", "An OTel Config to generate a spec from configured components")
	flagCompare         = flag.Bool("compare", false, "Compare the generated distribution against the existing one")

	errNoSpecFlag = errors.New("missing --spec flag")
)

type arrayFlag []string

func newArrayFlag(name, usage string) *arrayFlag {
	var newFlagValue arrayFlag
	flag.Var(&newFlagValue, name, usage)
	return &newFlagValue
}

// Implements flag.Value
func (a *arrayFlag) String() string {
	return strings.Join(*a, " ")
}

func (a *arrayFlag) Set(value string) error {
	values := []string{value}
	if strings.Contains(value, ",") {
		values = strings.Split(value, ",")
	}
	*a = append(*a, values...)
	return nil
}
