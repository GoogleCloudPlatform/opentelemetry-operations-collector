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
	"testing"

	"github.com/google/go-cmp/cmp"
)

var validLogFilters = []string{
	`severity = "hello"`,
	`jsonPayload."bar.baz" = "hello"`,
	`jsonPayload.b.c=~"b.*c"`,
	`"jsonPayload"."foo" = "bar"`,
	`-severity = 1`,
	`NOT severity = 3`,
	`(jsonPayload.bar = "one" OR jsonPayload.bar = "two") jsonPayload.baz = "three"`,
	`jsonPayload.one = 1 jsonPayload.two = 2 AND jsonPayload.three = 3`,
	`jsonPayload.int_field:0 OR jsonPayload.int_field:0 AND jsonPayload.int_field:0`,
	`jsonPayload.compound.string_field : wal\"rus`,
	`severity =~ "ERROR" AND jsonPayload.message =~ "foo" AND httpRequest.requestMethod =~ "GET"`,
	`severity = "AND"`,
	`severity = AND`,
	`severity = OR`,
	`severity = NOT`,
	`"json\u0050ayload".foo = bar`,
	`jsonPayload.\= = bar`,
	`jsonPayload."\=" = bar`,
}

func TestLogFilterShouldParse(t *testing.T) {
	for _, tc := range validLogFilters {
		t.Run(tc, func(t *testing.T) {
			filter, err := newLogFilter(tc)
			if err != nil {
				t.Fatalf("newLogFilter(%q) unexpected error: %v", tc, err)
			}
			if filter == nil {
				t.Fatal("got nil filter")
			}
			if _, err := filter.OTTLExpression(); err != nil {
				t.Errorf("OTTLExpression() unexpected error: %v", err)
			}
		})
	}
}

func TestLogFilterRoundTrip(t *testing.T) {
	for _, tc := range validLogFilters {
		t.Run(tc, func(t *testing.T) {
			filter, err := newLogFilter(tc)
			if err != nil {
				t.Fatal(err)
			}
			first := filter.String()
			filter2, err := newLogFilter(first)
			if err != nil {
				t.Fatalf("failed to re-parse %q: %v", first, err)
			}
			second := filter2.String()
			if diff := cmp.Diff(second, first); diff != "" {
				t.Errorf("filter did not round-trip (second -, first +):\n%s", diff)
			}
		})
	}
}

func TestInvalidLogFilters(t *testing.T) {
	for _, tc := range []string{
		`"missing operator"`,
		`invalid/characters*here`,
		`jsonPayload.foo =~ bareword`,
		`json\u0050ayload.foo = bar`,
	} {
		t.Run(tc, func(t *testing.T) {
			filter, err := newLogFilter(tc)
			if err == nil {
				t.Errorf("invalid filter %q unexpectedly parsed: %v", tc, filter)
			}
		})
	}
}

func TestValidLogMembers(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`jsonPayload.foo`, []string{"jsonPayload", "foo"}},
		{`labels."logging.googleapis.com/foo"`, []string{"labels", "logging.googleapis.com/foo"}},
		{`severity`, []string{"severity"}},
		{`jsonPayload.\=`, []string{"jsonPayload", `\=`}},
		{`jsonPayload."\="`, []string{"jsonPayload", `=`}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			member, err := newLogMember(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := member.Unquote()
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("incorrect parse (got -/want +):\n%s", diff)
			}
		})
	}
}

func TestUnquoteFilterString(t *testing.T) {
	for in, out := range map[string]string{
		`\150\145\154\154\157`:     "\150\145\154\154\157",
		`\x48\x65\x6C\x6C\x6F`:     "Hello",
		`\150\145\u013E\u013E\157`: "\150\145\u013E\u013E\157",
		`sl\\as\\\\h`:              `sl\as\\h`,
		`\777`:                     `?7`,
		`\377`:                     "\u00FF",
		`\`:                        `\`,
		`☃`:                        `☃`,
	} {
		t.Run(in, func(t *testing.T) {
			got, err := unquoteFilterString(in)
			if got != out {
				t.Errorf("got %q, want %q", got, out)
			}
			if err != nil {
				t.Error(err)
			}
		})
	}
}

func TestValidLogTargetPath(t *testing.T) {
	for _, tc := range []struct {
		in           logTarget
		want         string
		ottlPath     []string
		ottlAccessor string
	}{
		{
			logTarget{"jsonPayload", "hello"},
			"jsonPayload.hello",
			[]string{"body", "hello"},
			`body["hello"]`,
		},
		{
			logTarget{`"json\u0050ayload"`, "hello"},
			"jsonPayload.hello",
			[]string{"body", "hello"},
			`body["hello"]`,
		},
		{
			logTarget{"severity"},
			"severity",
			[]string{"severity_text"},
			`severity_text`,
		},
		{
			logTarget{"httpRequest"},
			"httpRequest",
			[]string{"attributes", "gcp.http_request"},
			`attributes["gcp.http_request"]`,
		},
		{
			logTarget{"httpRequest", "status"},
			"httpRequest.status",
			[]string{"attributes", "gcp.http_request", "status"},
			`attributes["gcp.http_request"]["status"]`,
		},
		{
			logTarget{"sourceLocation", "line"},
			"sourceLocation.line",
			[]string{"attributes", "gcp.source_location", "line"},
			`attributes["gcp.source_location"]["line"]`,
		},
		{
			logTarget{"sourceLocation", "function"},
			"sourceLocation.function",
			[]string{"attributes", "gcp.source_location", "func"},
			`attributes["gcp.source_location"]["func"]`,
		},
		{
			logTarget{"labels", "custom"},
			"labels.custom",
			[]string{"attributes", "custom"},
			`attributes["custom"]`,
		},
		{
			logTarget{`jsonPayload`, `"escaped fields \a\b\f\n\r\t\v"`},
			`jsonPayload."escaped\u0020fields\u0020\a\b\f\n\r\t\v"`,
			[]string{"body", "escaped fields \a\b\f\n\r\t\v"},
			`body["escaped fields \a\b\f\n\r\t\v"]`,
		},
	} {
		t.Run(tc.in.String(), func(t *testing.T) {
			got := tc.in.String()
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("unexpected target string (got -/want +):\n%s", diff)
			}
			gotPath, err := tc.in.ottlPath()
			if err != nil {
				t.Errorf("got unexpected error: %v", err)
			}
			if diff := cmp.Diff(gotPath, tc.ottlPath); diff != "" {
				t.Errorf("unexpected OTTL path (got -/want +):\n%s", diff)
			}
			gotAccessor, err := tc.in.OTTLAccessor()
			if err != nil {
				t.Errorf("got unexpected error: %v", err)
			}
			if diff := cmp.Diff(gotAccessor.String(), tc.ottlAccessor); diff != "" {
				t.Errorf("unexpected OTTL accessor (got -/want +):\n%s", diff)
			}
		})
	}
}
