// Copyright 2026 Google LLC
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

package gce

import (
	"bytes"
	"log"
	"testing"
)

func TestAreTagsValid(t *testing.T) {
	tests := []struct {
		name    string
		tags    []string
		wantOk  bool
		wantErr bool
	}{
		{
			name:    "valid tags",
			tags:    []string{"tag-1", "tag-2", "env-prod"},
			wantOk:  true,
			wantErr: false,
		},
		{
			name:    "empty tags slice",
			tags:    []string{},
			wantOk:  true,
			wantErr: false,
		},
		{
			name:    "tag containing comma",
			tags:    []string{"tag-1", "bad,tag", "tag-2"},
			wantOk:  false,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := areTagsValid(tc.tags)
			if ok != tc.wantOk {
				t.Errorf("areTagsValid(%v) ok = %v, want %v", tc.tags, ok, tc.wantOk)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("areTagsValid(%v) err = %v, wantErr = %v", tc.tags, err, tc.wantErr)
			}
		})
	}
}

func TestAddRemoveTagInvalidTags(t *testing.T) {
	ctx := t.Context()
	logger := log.New(&bytes.Buffer{}, "", 0)
	vm := &VM{Name: "test-vm", Project: "test-project", Zone: "us-central1-a"}

	invalidTags := []string{"bad,tag"}
	if _, err := AddTagToVm(ctx, logger, vm, invalidTags); err == nil {
		t.Errorf("AddTagToVm() expected error for invalid tags, got nil")
	}
	if _, err := RemoveTagFromVm(ctx, logger, vm, invalidTags); err == nil {
		t.Errorf("RemoveTagFromVm() expected error for invalid tags, got nil")
	}
}
