// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestHostmetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	os.Args = append(os.Args, "-config=config-for-testing.yaml")

	// Run the main function of otelopscol.
	mainContext(ctx)

	data, err := os.ReadFile("metrics.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal(string(data))
}
