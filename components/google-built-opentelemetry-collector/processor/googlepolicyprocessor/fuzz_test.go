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

package googlepolicyprocessor

import (
	"testing"

	policyv1alpha1 "github.com/GoogleCloudPlatform/opentelemetry-operations-collector/gen/go/policy/v1alpha1"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-collector/pkg/googlepolicy"
	"go.opentelemetry.io/collector/pdata/plog"
	"google.golang.org/protobuf/proto"
)

// FuzzLogTransformPolicy verifies that multiple interacting log transformation
// policies do not cause the evaluator to panic.
func FuzzLogTransformPolicy(f *testing.F) {
	f.Add([]byte{}, []byte{})

	f.Fuzz(func(t *testing.T, data1 []byte, data2 []byte) {
		pb1 := &policyv1alpha1.LogTransformPolicy{}
		pb2 := &policyv1alpha1.LogTransformPolicy{}

		valid1 := proto.Unmarshal(data1, pb1) == nil
		valid2 := proto.Unmarshal(data2, pb2) == nil

		var protos []proto.Message
		if valid1 {
			pb1.Id = "policy1"
			protos = append(protos, pb1)
		}
		if valid2 {
			pb2.Id = "policy2"
			protos = append(protos, pb2)
		}

		if len(protos) == 0 {
			return
		}

		ps, err := googlepolicy.MakePolicySetFromProtos("fuzz-set", "fuzz-rev", protos)
		if err != nil {
			return
		}

		policies := ps.TransformationPolicies()
		if len(policies) == 0 {
			return
		}

		eval, err := NewEvaluator(policies)
		if err != nil || eval == nil {
			return
		}

		// Create a basic static log payload
		ld := plog.NewLogs()
		rl := ld.ResourceLogs().AppendEmpty()
		sl := rl.ScopeLogs().AppendEmpty()
		lr := sl.LogRecords().AppendEmpty()
		lr.Body().SetStr("fuzz test")

		eval.TransformLogs(ld)
	})
}
