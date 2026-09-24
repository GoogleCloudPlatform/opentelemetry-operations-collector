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

package event

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/noop"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type recordingLoggerProvider struct {
	noop.LoggerProvider
	logger *recordingLogger
}

func (p *recordingLoggerProvider) Logger(_ string, _ ...log.LoggerOption) log.Logger {
	return p.logger
}

type recordingLogger struct {
	noop.Logger
	records []log.Record
}

func (l *recordingLogger) Enabled(context.Context, log.EnabledParameters) bool {
	return true
}

func (l *recordingLogger) Emit(_ context.Context, record log.Record) {
	l.records = append(l.records, record.Clone())
}

func TestRecordPolicyEvaluateErrorEvent_ZapObserver(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	RecordPolicyEvaluateErrorEvent(
		logger,
		errors.New("failed to compile regex expression"),
		"policy-123",
		"886313e1-3b8a-5372-9b90-0c9aee199e5d",
	)

	entries := recorded.All()
	require.Len(t, entries, 1)
	entry := entries[0]
	assert.Equal(t, "failed to compile regex expression", entry.Message)
	assert.Equal(t, zapcore.ErrorLevel, entry.Level)
	assert.Equal(t, map[string]any{
		"event.name":                 PolicyEvaluateErrorEventName,
		"gcp.policy.id":              "policy-123",
		"gcp.policy.set.revision.id": "886313e1-3b8a-5372-9b90-0c9aee199e5d",
	}, entry.ContextMap())
}

func TestRecordPolicyEvaluateErrorEvent_OtelZapBridge(t *testing.T) {
	recLogger := &recordingLogger{}
	provider := &recordingLoggerProvider{logger: recLogger}
	core := otelzap.NewCore("test", otelzap.WithLoggerProvider(provider), otelzap.WithSchemaURL(SchemaURL))
	logger := zap.New(core)

	RecordPolicyEvaluateErrorEvent(
		logger,
		errors.New("evaluation timed out"),
		"policy-789",
		"cfbfa0d5-2960-5b88-888c-f4937062d26b",
		WithPolicyEvaluateErrorEventFields(zap.String("custom.key", "custom-val")),
	)

	require.Len(t, recLogger.records, 1)
	rec := recLogger.records[0]
	assert.Equal(t, "evaluation timed out", rec.Body().AsString())
	assert.Equal(t, log.SeverityError, rec.Severity())

	attrs := make(map[string]any)
	rec.WalkAttributes(func(kv attribute.KeyValue) bool {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
		return true
	})

	assert.Equal(t, map[string]any{
		"event.name":                 PolicyEvaluateErrorEventName,
		"gcp.policy.id":              "policy-789",
		"gcp.policy.set.revision.id": "cfbfa0d5-2960-5b88-888c-f4937062d26b",
		"custom.key":                 "custom-val",
	}, attrs)
}

func TestRecordPolicySetInvalidEvent(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	RecordPolicySetInvalidEvent(
		logger,
		errors.New("more than one destination policy found"),
		"886313e1-3b8a-5372-9b90-0c9aee199e5d",
	)

	entries := recorded.All()
	require.Len(t, entries, 1)
	entry := entries[0]
	assert.Equal(t, "more than one destination policy found", entry.Message)
	assert.Equal(t, zapcore.ErrorLevel, entry.Level)
	assert.Equal(t, map[string]any{
		"event.name":                 PolicySetInvalidEventName,
		"gcp.policy.set.revision.id": "886313e1-3b8a-5372-9b90-0c9aee199e5d",
	}, entry.ContextMap())
}
