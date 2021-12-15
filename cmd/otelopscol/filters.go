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
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"moul.io/zapfilter"
)

type errorFilterConfig struct {
	fileName        string
	errorSubstrings []string
}

// Returns zap options that will filter logs with some error message from a file.
func errorFilterOptions() []zap.Option {
	errorFilters := []errorFilterConfig{
		// Filter out a problematic upstream otel spam log from hostmetrics.
		// Upstream issue: https://github.com/open-telemetry/opentelemetry-collector/issues/3004
		{
			fileName:        "scrapercontroller.go",
			errorSubstrings: []string{"error reading process name for pid"},
		},
	}

	options := []zap.Option{}
	for _, filter := range errorFilters {
		options = append(options, makeErrorFilterOption(filter))
	}

	return options
}

func makeErrorFilterOption(filterConfig errorFilterConfig) zap.Option {
	logFilterFunc := func(entry zapcore.Entry, fields []zapcore.Field) bool {
		if filterConfig.fileName != "" &&
			!strings.Contains(entry.Caller.File, filterConfig.fileName) {
			return true
		}
		for _, field := range fields {
			if field.Key == "error" {
				logError, ok := field.Interface.(error)
				if !ok {
					return true
				}
				return !matchAny(logError.Error(), filterConfig.errorSubstrings)
			}
		}
		return true
	}

	return zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return zapfilter.NewFilteringCore(core, logFilterFunc)
	})
}

func matchAny(s string, subs []string) bool {
	matches := make([]bool, len(subs))
	for i, sub := range subs {
		matches[i] = strings.Contains(s, sub)
	}
	for _, match := range matches {
		if match {
			return true
		}
	}
	return false
}
