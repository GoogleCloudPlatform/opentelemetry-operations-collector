package main

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"moul.io/zapfilter"
)

// Returns a zapfilter core that will filter logs with some error message.
func errorFilterCore() zap.Option {
	errorSubstringsToFilter := []string{
		// Filter out a problematic upstream otel spam log from hostmetrics.
		// Upstream issue: https://github.com/open-telemetry/opentelemetry-collector/issues/3004
		"error reading process name for pid",
	}

	logFilterFunc := func(entry zapcore.Entry, fields []zapcore.Field) bool {
		if !strings.Contains(entry.Caller.File, "scrapercontroller.go") {
			return true
		}
		for _, field := range fields {
			if field.Key == "error" {
				logError, ok := field.Interface.(error)
				if !ok {
					return true
				}
				return !matchAny(logError.Error(), errorSubstringsToFilter)
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
