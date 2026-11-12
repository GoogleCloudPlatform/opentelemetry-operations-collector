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

package inprocesstelemetry

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// loggerScopeName is the instrumentation scope name upstream otelconftelemetry
// uses for the Collector's internal logs.
const loggerScopeName = "go.opentelemetry.io/collector/service"

// inProcessCore wraps the zap core built by upstream otelconftelemetry and
// additionally copies every log entry that core accepts to an OpenTelemetry
// LoggerProvider backed by the in-process LogBuffer.
//
// It mirrors upstream's own zap-to-LoggerProvider tee
// (service/telemetry/otelconftelemetry/logger_tee.go): entries are only
// copied once the wrapped core has accepted them, so the configured level and
// sampling apply to the in-process copy too, and the Collector's per-component
// attributes become instrumentation scope attributes rather than log record
// attributes.
type inProcessCore struct {
	inner      zapcore.Core // upstream core (stderr and any configured processors)
	otelCore   zapcore.Core // otelzap core forwarding to the in-process LoggerProvider
	lp         log.LoggerProvider
	withFields []zap.Field // fields added with With, re-applied when the scope changes
}

var _ zapcore.Core = inProcessCore{}

func newInProcessCore(inner zapcore.Core, lp log.LoggerProvider) inProcessCore {
	return inProcessCore{
		inner:    inner,
		otelCore: otelzap.NewCore(loggerScopeName, otelzap.WithLoggerProvider(lp)),
		lp:       lp,
	}
}

func (c inProcessCore) With(fields []zapcore.Field) zapcore.Core {
	// Upstream's core edits the slice it is given in place (it deletes the
	// scope field from it), so it must not see ours.
	c.inner = c.inner.With(slices.Clone(fields))
	fields = slices.DeleteFunc(slices.Clone(fields), func(field zapcore.Field) bool {
		scope, ok := extractScopeAttributes(field)
		if !ok {
			return false
		}
		c.otelCore = otelzap.NewCore(
			loggerScopeName,
			otelzap.WithLoggerProvider(c.lp),
			otelzap.WithAttributes(scope...),
		).With(c.withFields)
		return true
	})
	c.otelCore = c.otelCore.With(fields)
	c.withFields = append(slices.Clone(c.withFields), fields...)
	return c
}

func (c inProcessCore) Enabled(level zapcore.Level) bool {
	return c.inner.Enabled(level)
}

func (c inProcessCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	ce = c.inner.Check(entry, ce)
	if ce != nil {
		// Only copy entries that the wrapped core accepted.
		ce = ce.AddCore(entry, c.otelCore)
	}
	return ce
}

// Write is never called by zap for a conforming caller, because only the inner
// cores add themselves to the CheckedEntry. Like zapcore.multiCore, it is still
// implemented for non-conforming callers.
func (c inProcessCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	return multierr.Append(
		c.inner.Write(entry, fields),
		c.otelCore.Write(entry, fields),
	)
}

func (c inProcessCore) Sync() error {
	return multierr.Append(c.inner.Sync(), c.otelCore.Sync())
}

// extractScopeAttributes recognizes the zap field that the Collector attaches
// to every component logger to carry its component attributes (otelcol.signal,
// otelcol.component.id, ...) and returns those attributes.
//
// Upstream represents that field with an unexported type,
// componentattribute.scopeAttributesField, in an internal package, so it is
// recognized here by its type name and package path, and its attributes are
// read through its public zapcore.ObjectMarshaler implementation. If upstream
// ever renames the type, the field is no longer recognized and its attributes
// end up as log record attributes instead of scope attributes: still present,
// just in a different place. The selftelemetry receiver's end-to-end test
// guards against that.
func extractScopeAttributes(field zapcore.Field) ([]attribute.KeyValue, bool) {
	if field.Type != zapcore.InlineMarshalerType || field.Interface == nil {
		return nil, false
	}
	t := reflect.TypeOf(field.Interface)
	if t.Name() != "scopeAttributesField" ||
		!strings.HasSuffix(t.PkgPath(), "/service/internal/componentattribute") {
		return nil, false
	}
	marshaler, ok := field.Interface.(zapcore.ObjectMarshaler)
	if !ok {
		return nil, false
	}
	enc := zapcore.NewMapObjectEncoder()
	if err := marshaler.MarshalLogObject(enc); err != nil {
		return nil, false
	}
	keys := make([]string, 0, len(enc.Fields))
	for k := range enc.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	attrs := make([]attribute.KeyValue, 0, len(keys))
	for _, k := range keys {
		attrs = append(attrs, toAttribute(k, enc.Fields[k]))
	}
	return attrs, true
}

func toAttribute(key string, v any) attribute.KeyValue {
	switch v := v.(type) {
	case string:
		return attribute.String(key, v)
	case bool:
		return attribute.Bool(key, v)
	case int:
		return attribute.Int(key, v)
	case int64:
		return attribute.Int64(key, v)
	case float64:
		return attribute.Float64(key, v)
	default:
		return attribute.String(key, fmt.Sprint(v))
	}
}
