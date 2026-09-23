// Copyright 2026 gRPC authors.
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

// Package grpctest provides a minimal test runner shim for xDS client unit tests.
package grpctest

import (
	"reflect"
	"strings"
	"testing"
)

// Tester is an embedded base type for test suites.
type Tester struct{}

// Setup is a no-op setup hook.
func (Tester) Setup(*testing.T) {}

// Teardown is a no-op teardown hook.
func (Tester) Teardown(*testing.T) {}

// RunSubTests runs all "Test*" methods on x as subtests of t.
func RunSubTests(t *testing.T, x any) {
	v := reflect.ValueOf(x)
	vt := v.Type()
	for i := 0; i < vt.NumMethod(); i++ {
		m := vt.Method(i)
		if !strings.HasPrefix(m.Name, "Test") {
			continue
		}
		name := strings.TrimPrefix(m.Name, "Test")
		t.Run(name, func(t *testing.T) {
			if setup, ok := vt.MethodByName("Setup"); ok {
				setup.Func.Call([]reflect.Value{v, reflect.ValueOf(t)})
			}
			defer func() {
				if teardown, ok := vt.MethodByName("Teardown"); ok {
					teardown.Func.Call([]reflect.Value{v, reflect.ValueOf(t)})
				}
			}()
			m.Func.Call([]reflect.Value{v, reflect.ValueOf(t)})
		})
	}
}
