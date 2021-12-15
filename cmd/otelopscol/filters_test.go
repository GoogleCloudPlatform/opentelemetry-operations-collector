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
