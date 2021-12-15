// Copyright 2021 Google LLC
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
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestErrorFilterOption(t *testing.T) {
	testCases := []struct {
		filterConfigs    []errorFilterConfig
		errors           []error
		expectedLogCount int
	}{
		{
			filterConfigs: []errorFilterConfig{
				{
					errorSubstrings: []string{"error reading process name"},
				},
			},
			errors: []error{
				errors.New("error reading process name for pid 1:"),
			},
			expectedLogCount: 0,
		},
		{
			filterConfigs: []errorFilterConfig{
				{
					errorSubstrings: []string{"error reading process name"},
				},
			},
			errors: []error{
				errors.New("error reading process name for pid 1:"),
				errors.New("error reading process name for pid 2:"),
				errors.New("error reading process name for pid 0:"),
			},
			expectedLogCount: 0,
		},
		{
			filterConfigs: []errorFilterConfig{
				{
					errorSubstrings: []string{"error reading process name"},
				},
			},
			errors: []error{
				errors.New("error reading process name for pid 1:"),
				errors.New("unrelated error"),
			},
			expectedLogCount: 1,
		},
	}

	for _, testCase := range testCases {
		logger, observedLogs := makeErrorFilterLogger(testCase.filterConfigs)
		for _, err := range testCase.errors {
			logger.Error("error", zap.Error(err))
		}
		if len(observedLogs.All()) != testCase.expectedLogCount {
			t.Fatalf("expected %d logs, got %d", testCase.expectedLogCount, len(observedLogs.All()))
		}
	}
}

func makeErrorFilterLogger(filterConfigs []errorFilterConfig) (*zap.Logger, *observer.ObservedLogs) {
	observedCore, observedLogs := observer.New(zap.InfoLevel)
	options := []zap.Option{}
	for _, filterConfig := range filterConfigs {
		options = append(options, makeErrorFilterOption(filterConfig))
	}
	logger := zap.New(observedCore, options...)
	return logger, observedLogs
}
