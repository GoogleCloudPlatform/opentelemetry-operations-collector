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

package opsagentconfprovider

import (
	"fmt"
	"slices"
	"strings"
)

type ottlStatements []string

// ottlValue represents an OTTL expression (either an ottlLValue field path or an ottlRValue expression).
type ottlValue interface {
	fmt.Stringer
}

// ottlLValue represents a field path in OTTL that can be read from or written to.
type ottlLValue []string

func (path ottlLValue) String() string {
	parts := []string{path[0]}
	for _, p := range path[1:] {
		parts = append(parts, fmt.Sprintf(`[%q]`, p))
	}
	return strings.Join(parts, "")
}

// ottlRValue represents an arbitrary OTTL expression that evaluates to a value.
type ottlRValue string

func (v ottlRValue) String() string {
	return string(v)
}

func ottlValuef(format string, args ...any) ottlValue {
	return ottlRValue(fmt.Sprintf(format, args...))
}

func ottlStatementf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func ottlStatementsf(format string, args ...any) ottlStatements {
	return ottlStatements{ottlStatementf(format, args...)}
}

func ottlStringLiteral(v string) ottlValue {
	return ottlValuef(`%q`, v)
}

func ottlIntLiteral(v int) ottlValue {
	return ottlValuef(`%d`, v)
}

func ottlFalse() ottlValue {
	return ottlValuef("false")
}

func ottlTrue() ottlValue {
	return ottlValuef("true")
}

func (a ottlLValue) Set(b ottlValue) ottlStatements {
	return a.SetIf(b, nil)
}

func (a ottlLValue) SetIfNil(b ottlValue) ottlStatements {
	return a.SetIf(b, ottlValuef(`%s == nil`, a))
}

func (a ottlLValue) SetIf(b, condition ottlValue) ottlStatements {
	var condStr string
	if condition != nil {
		condStr = fmt.Sprintf(" where %s", condition)
	}
	var statements ottlStatements
	if slices.Equal(a, ottlLValue{"trace_id.string"}) {
		// LogEntry's `trace` field may contain a `projects/$foo/traces/` prefix.
		// Strip it if present by mutating a cached copy before assigning trace_id.string.
		cache := ottlLValue{"cache", "__setif_value"}
		statements = statements.Append(
			cache.Delete(),
			cache.SetIf(b, condition),
			ottlStatementsf(
				`replace_pattern(%s, %q, %q) where %s`,
				cache,
				`^projects/([^/]*)/traces/`,
				"",
				cache.IsPresent(),
			),
		)
		b = cache
	}
	statements = statements.Append(
		ottlStatementsf(`set(%s, %s)%s`, a, b, condStr),
	)
	if slices.Equal(a, ottlLValue{"attributes", "gcp.source_location"}) {
		// LogEntry's `sourceLocation.line` is strictly expected to be an int64 by Cloud Logging.
		statements = statements.Append(
			ottlStatementsf(`set(%s["line"], Int(%s["line"])) where %s["line"] != nil`, a, a, a),
		)
	}
	if slices.Equal(a, ottlLValue{"attributes", "gcp.http_request"}) {
		// HTTPRequest integer fields must be converted to int64 if represented as strings.
		statements = statements.Append(
			ottlStatementsf(`set(%s["status"], Int(%s["status"])) where %s["status"] != nil`, a, a, a),
			ottlStatementsf(`set(%s["responseSize"], Int(%s["responseSize"])) where %s["responseSize"] != nil`, a, a, a),
			ottlStatementsf(`set(%s["requestSize"], Int(%s["requestSize"])) where %s["requestSize"] != nil`, a, a, a),
			ottlStatementsf(`set(%s["cacheFillBytes"], Int(%s["cacheFillBytes"])) where %s["cacheFillBytes"] != nil`, a, a, a),
		)
	}
	if slices.Equal(a, ottlLValue{"severity_text"}) {
		// Zero out severity_number so severity_text takes effect.
		statements = statements.Append(
			ottlLValue{"severity_number"}.SetIf(ottlIntLiteral(0), condition),
		)
	}
	return statements
}

func (a ottlLValue) AppendValuesIf(b, condition ottlValue) ottlStatements {
	return ottlStatements{
		ottlStatementf(`append(%s, %s) where %s`, a, b, condition),
	}
}

func (a ottlLValue) MergeMaps(source ottlValue, strategy string) ottlStatements {
	return a.MergeMapsIf(source, strategy, ottlIsNotNil(source))
}

func (a ottlLValue) MergeMapsIf(source ottlValue, strategy string, condition ottlValue) ottlStatements {
	var condStr string
	if condition != nil {
		condStr = fmt.Sprintf(" where %s", condition)
	}
	return ottlStatements{
		ottlStatementf(`merge_maps(%s, %s, %q)%s`, a, source, strategy, condStr),
	}
}

// IsPresent returns an OTTL condition checking that the field and all parent maps are non-nil.
func (a ottlLValue) IsPresent() ottlValue {
	conditions := make([]ottlValue, 0, len(a))
	for i := 1; i <= len(a); i++ {
		conditions = append(conditions, ottlIsNotNil(a[:i]))
	}
	return ottlAnd(conditions...)
}

func ottlToString(a ottlValue) ottlValue {
	return ottlValuef(`Concat([%s], "")`, a)
}

func ottlToInt(a ottlValue) ottlValue {
	return ottlValuef(`Int(%s)`, a)
}

func ottlToFloat(a ottlValue) ottlValue {
	return ottlValuef(`Double(%s)`, a)
}

func ottlToTime(a ottlValue, strpformat string) ottlValue {
	return ottlValuef(`Time(%s, %q)`, a, strpformat)
}

func ottlParseJSON(a ottlValue) ottlValue {
	return ottlValuef(`ParseJSON(%s)`, a)
}

func ottlParseSimplifiedXML(a ottlValue) ottlValue {
	return ottlValuef(`ParseSimplifiedXML(%s)`, a)
}

func ottlExtractPatternsRubyRegex(a ottlValue, pattern string, omitEmptyValues bool) ottlValue {
	return ottlValuef(`ExtractPatternsRubyRegex(%s, %q, %v)`, a, pattern, omitEmptyValues)
}

func ottlConcat(values []ottlValue, delimiter string) ottlValue {
	stringValues := make([]string, 0, len(values))
	for _, v := range values {
		stringValues = append(stringValues, v.String())
	}
	return ottlValuef(`Concat([%s], "%s")`, strings.Join(stringValues, ","), delimiter)
}

func ottlConvertCase(a ottlValue, toCase string) ottlValue {
	return ottlValuef(`ConvertCase(%s, %q)`, a, toCase)
}

func ottlContainsValue(a ottlValue, value string) ottlValue {
	return ottlValuef(`ContainsValue(%s, %q)`, a, value)
}

func ottlFormatTime(a ottlValue, format string) ottlValue {
	return ottlValuef(`FormatTime(%s, %q)`, a, format)
}

func ottlToValues(a ottlValue) ottlValue {
	return ottlValuef(`ToValues(%s)`, a)
}

func ottlIsMatch(target ottlValue, pattern string) ottlValue {
	return ottlValuef(`IsMatch(%s, %q)`, target, pattern)
}

func ottlIsMatchRubyRegex(target ottlValue, pattern string) ottlValue {
	return ottlValuef(`IsMatchRubyRegex(%s, %q)`, target, pattern)
}

func ottlEquals(a, b ottlValue) ottlValue {
	return ottlValuef(`%s == %s`, a, b)
}

func ottlNot(a ottlValue) ottlValue {
	return ottlValuef(`(not %s)`, a)
}

func ottlAnd(conditions ...ottlValue) ottlValue {
	out := make([]string, 0, len(conditions))
	for _, c := range conditions {
		out = append(out, c.String())
	}
	return ottlValuef(`(%s)`, strings.Join(out, " and "))
}

func ottlOr(conditions ...ottlValue) ottlValue {
	out := make([]string, 0, len(conditions))
	for _, c := range conditions {
		out = append(out, c.String())
	}
	return ottlValuef(`(%s)`, strings.Join(out, " or "))
}

func ottlIsNotNil(a ottlValue) ottlValue {
	return ottlValuef(`%s != nil`, a)
}

// Delete removes a (potentially nested) key from its parent map if the key exists.
func (a ottlLValue) Delete() ottlStatements {
	parent := a[:len(a)-1]
	child := a[len(a)-1]
	return ottlStatements{
		ottlStatementf(`delete_key(%s, %q) where %s`, parent, child, a.IsPresent()),
	}
}

// DeleteIf removes a (potentially nested) key from its parent map if the key exists and cond holds.
func (a ottlLValue) DeleteIf(cond ottlValue) ottlStatements {
	parent := a[:len(a)-1]
	child := a[len(a)-1]
	return ottlStatements{
		ottlStatementf(`delete_key(%s, %q) where %s`, parent, child, ottlAnd(a.IsPresent(), cond)),
	}
}

func (a ottlLValue) KeepKeys(keys ...string) ottlStatements {
	quotedKeys := make([]string, 0, len(keys))
	for _, k := range keys {
		quotedKeys = append(quotedKeys, fmt.Sprintf("%q", k))
	}
	return ottlStatementsf(`keep_keys(%s, [%s])`, a, strings.Join(quotedKeys, ", "))
}

func newOTTLStatements(a ...ottlStatements) ottlStatements {
	return ottlStatements{}.Append(a...)
}

func (a ottlStatements) Append(b ...ottlStatements) ottlStatements {
	for _, c := range b {
		a = append(a, c...)
	}
	return a
}
