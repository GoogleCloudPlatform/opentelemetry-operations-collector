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

package command

import (
	"flag"
	"strings"
)

type ArrayFlag []string

func NewArrayFlag(flagSet *flag.FlagSet, name, usage string) *ArrayFlag {
	var newFlagValue ArrayFlag
	flagSet.Var(&newFlagValue, name, usage)
	return &newFlagValue
}

// Implements flag.Value
func (a *ArrayFlag) String() string {
	return strings.Join(*a, " ")
}

func (a *ArrayFlag) Set(value string) error {
	values := []string{value}
	if strings.Contains(value, ",") {
		values = strings.Split(value, ",")
	}
	*a = append(*a, values...)
	return nil
}
