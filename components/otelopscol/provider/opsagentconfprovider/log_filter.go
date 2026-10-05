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
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.uber.org/multierr"
)

// logTarget represents a parsed member from the Cloud Logging filter grammar
// (either a literal value or a dotted field path). Each element is not yet unescaped.
type logTarget []string

var logEntryRootValueMapToOTel = map[string][]string{
	"severity":    {"severity_text"},
	"logName":     {"attributes", "gcp.log_name"},
	"trace":       {"trace_id.string"},
	"spanId":      {"span_id.string"},
	"textPayload": {"body"},
}

var logEntryRootStructMapToOTel = map[string][]string{
	"jsonPayload":    {"body"},
	"labels":         {"attributes"},
	"operation":      {"attributes", "gcp.operation"},
	"sourceLocation": {"attributes", "gcp.source_location"},
	"httpRequest":    {"attributes", "gcp.http_request"},
}

var specialFieldsMap = map[string]string{
	"logging.googleapis.com/severity":       "severity",
	"logging.googleapis.com/logName":        "logName",
	"logging.googleapis.com/trace":          "trace",
	"logging.googleapis.com/spanId":         "spanId",
	"logging.googleapis.com/labels":         "labels",
	"logging.googleapis.com/operation":      "operation",
	"logging.googleapis.com/sourceLocation": "sourceLocation",
	"logging.googleapis.com/httpRequest":    "httpRequest",
}

func (m logTarget) ottlPath() ([]string, error) {
	unquoted, err := m.Unquote()
	if err != nil {
		return nil, err
	}
	var otel []string
	if len(unquoted) == 1 {
		if v, ok := logEntryRootValueMapToOTel[unquoted[0]]; ok {
			otel = v
		}
	}
	if len(unquoted) >= 1 {
		if unquoted[0] == "sourceLocation" && len(unquoted) > 1 && unquoted[1] == "function" {
			unquoted[1] = "func"
		}
		if v, ok := logEntryRootStructMapToOTel[unquoted[0]]; ok {
			otel = append(append([]string(nil), v...), unquoted[1:]...)
		}
	}
	if otel == nil {
		return nil, fmt.Errorf("field %q not found", strings.Join(m, "."))
	}
	return otel, nil
}

// canonicalPathKey returns a canonical string key representing the resolved OTTL path.
func (m logTarget) canonicalPathKey() (string, error) {
	path, err := m.ottlPath()
	if err != nil {
		return "", err
	}
	return strings.Join(path, "\x00"), nil
}

// OTTLAccessor returns the OTTL LValue used to reference this field.
func (m logTarget) OTTLAccessor() (ottlLValue, error) {
	otel, err := m.ottlPath()
	if err != nil {
		return nil, err
	}
	return ottlLValue(otel), nil
}

const (
	filterStartChar  = `#$%&'*/;?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[]^_` + "`" + `abcdefghijklmnopqrstuvwxyz{|}`
	filterMidChar    = filterStartChar + `0123456789+-`
	filterStringChar = filterMidChar + `!(),.:<=>~`
)

func escapeFilterString(in string) string {
	var needQuotes bool
	var b strings.Builder
	for i, c := range in {
		if i == 0 {
			if strings.ContainsRune(filterStartChar, c) {
				b.WriteRune(c)
				continue
			}
			needQuotes = true
		}
		if strings.ContainsRune(filterMidChar, c) {
			b.WriteRune(c)
			continue
		}
		needQuotes = true
		if strings.ContainsRune(filterStringChar, c) {
			b.WriteRune(c)
		} else if c == '\a' {
			b.WriteString(`\a`)
		} else if c == '\b' {
			b.WriteString(`\b`)
		} else if c == '\f' {
			b.WriteString(`\f`)
		} else if c == '\n' {
			b.WriteString(`\n`)
		} else if c == '\r' {
			b.WriteString(`\r`)
		} else if c == '\t' {
			b.WriteString(`\t`)
		} else if c == '\v' {
			b.WriteString(`\v`)
		} else {
			fmt.Fprintf(&b, `\u%04X`, c)
		}
	}
	if needQuotes {
		return fmt.Sprintf(`"%s"`, b.String())
	}
	return b.String()
}

func (m logTarget) Unquote() ([]string, error) {
	unquoted := make([]string, 0, len(m))
	for _, part := range m {
		p, err := unquoteTextOrString(part)
		if err != nil {
			return nil, err
		}
		unquoted = append(unquoted, p)
	}
	return unquoted, nil
}

func (m logTarget) String() string {
	unquoted, err := m.Unquote()
	if err != nil {
		return fmt.Sprintf("UNPARSABLE TARGET %#v", m)
	}
	out := make([]string, 0, len(unquoted))
	for _, s := range unquoted {
		out = append(out, escapeFilterString(s))
	}
	return strings.Join(out, ".")
}

// logMember represents a validated Cloud Logging field path.
type logMember struct {
	logTarget
}

func newLogMember(m string) (*logMember, error) {
	expr, err := parseFilterInput(m)
	if err != nil {
		return nil, err
	}
	r, ok := expr.(logRestriction)
	if !ok || r.Operator != "GLOBAL" {
		return nil, fmt.Errorf("not a field: %#v", expr)
	}
	return &logMember{r.LHS}, nil
}

// newLogMemberLegacy attempts to parse m as a filter member, falling back to prepending "jsonPayload.".
func newLogMemberLegacy(m string) (*logMember, error) {
	out, err := newLogMember(m)
	if err != nil {
		if prefixed, perr := newLogMember(fmt.Sprintf("jsonPayload.%s", m)); perr == nil {
			return prefixed, nil
		}
	}
	return out, err
}

// logFilter represents a parsed Cloud Logging filter expression.
type logFilter struct {
	expr logFilterExpr
}

func newLogFilter(f string) (*logFilter, error) {
	expr, err := parseFilterInput(f)
	if err != nil {
		return nil, err
	}
	return &logFilter{expr: expr.Simplify()}, nil
}

func (f logFilter) OTTLExpression() (ottlValue, error) {
	return f.expr.OTTLExpression()
}

func (f logFilter) String() string {
	return f.expr.String()
}

type logFilterExpr interface {
	Simplify() logFilterExpr
	OTTLExpression() (ottlValue, error)
	fmt.Stringer
}

type logRestriction struct {
	Operator string
	LHS      logTarget
	RHS      string
}

func newLogRestriction(lhs logTarget, operator string, rhs logTarget) (*logRestriction, error) {
	if _, err := lhs.OTTLAccessor(); err != nil {
		return nil, err
	}
	r := logRestriction{
		Operator: operator,
		LHS:      lhs,
	}
	if rhs != nil {
		if len(rhs) != 1 {
			return nil, fmt.Errorf("unexpected rhs: %v", rhs)
		}
		switch r.Operator {
		case "=~", "!~":
			raw := rhs[0]
			if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
				return nil, fmt.Errorf("regular expressions must begin and end with '\"', token %q", raw)
			}
			r.RHS = raw[1 : len(raw)-1]
		default:
			unquoted, err := unquoteTextOrString(rhs[0])
			if err != nil {
				return nil, err
			}
			r.RHS = unquoted
		}
	}
	return &r, nil
}

func (r logRestriction) Simplify() logFilterExpr {
	return r
}

func (r logRestriction) String() string {
	if r.Operator == "GLOBAL" {
		return escapeFilterString(r.RHS)
	}
	switch r.Operator {
	case "=~", "!~":
		return fmt.Sprintf(`%s %s "%s"`, r.LHS, r.Operator, r.RHS)
	}
	return fmt.Sprintf(`%s %s %s`, r.LHS, r.Operator, escapeFilterString(r.RHS))
}

func (r logRestriction) OTTLExpression() (ottlValue, error) {
	lhs, _ := r.LHS.OTTLAccessor()
	var expr ottlValue
	switch r.Operator {
	case "GLOBAL", "<", "<=", ">", ">=":
		return nil, fmt.Errorf("unimplemented operator: %s", r.Operator)
	case ":":
		expr = ottlIsMatch(lhs, fmt.Sprintf(`(?i)%s`, regexp.QuoteMeta(r.RHS)))
	case "=~", "!~":
		expr = ottlIsMatch(lhs, r.RHS)
		if _, err := regexp.Compile(r.RHS); err != nil {
			expr = ottlIsMatchRubyRegex(lhs, r.RHS)
		}
		if r.Operator == "!~" {
			expr = ottlNot(expr)
		}
	case "=", "!=":
		expr = ottlIsMatch(lhs, fmt.Sprintf(`(?i)^%s$`, regexp.QuoteMeta(r.RHS)))
		if r.Operator == "!=" {
			expr = ottlNot(expr)
		}
	}
	if expr != nil {
		return ottlAnd(lhs.IsPresent(), expr), nil
	}
	return nil, fmt.Errorf("unknown operator: %s", r.Operator)
}

type logConjunction []logFilterExpr
type logDisjunction []logFilterExpr

func newLogConjunction(e logFilterExpr) logConjunction {
	if c, ok := e.(logConjunction); ok {
		return c
	}
	return logConjunction{e.Simplify()}
}

func (c logConjunction) Simplify() logFilterExpr {
	if len(c) == 1 {
		return c[0]
	}
	return c
}

func (c logConjunction) Append(e logFilterExpr) logConjunction {
	if other, ok := e.(logConjunction); ok {
		return append(c, other...)
	}
	return append(c, e.Simplify())
}

func (c logConjunction) OTTLExpression() (ottlValue, error) {
	return evalLogExprSlice(c, ottlAnd)
}

func (c logConjunction) String() string {
	return formatLogExprSlice(c, "AND")
}

func newLogDisjunction(e logFilterExpr) logDisjunction {
	if d, ok := e.(logDisjunction); ok {
		return d
	}
	return logDisjunction{e.Simplify()}
}

func (d logDisjunction) Simplify() logFilterExpr {
	if len(d) == 1 {
		return d[0]
	}
	return d
}

func (d logDisjunction) Append(e logFilterExpr) logDisjunction {
	if other, ok := e.(logDisjunction); ok {
		return append(d, other...)
	}
	return append(d, e.Simplify())
}

func (d logDisjunction) OTTLExpression() (ottlValue, error) {
	return evalLogExprSlice(d, ottlOr)
}

func (d logDisjunction) String() string {
	return formatLogExprSlice(d, "OR")
}

func evalLogExprSlice(s []logFilterExpr, operator func(...ottlValue) ottlValue) (ottlValue, error) {
	values := make([]ottlValue, 0, len(s))
	var err error
	for _, e := range s {
		v, eerr := e.OTTLExpression()
		values = append(values, v)
		multierr.AppendInto(&err, eerr)
	}
	return operator(values...), err
}

func formatLogExprSlice(s []logFilterExpr, operator string) string {
	out := make([]string, 0, len(s))
	for _, e := range s {
		out = append(out, e.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(out, ") "+operator+" ("))
}

type logNegation struct {
	logFilterExpr
}

func (n logNegation) Simplify() logFilterExpr {
	return logNegation{n.logFilterExpr.Simplify()}
}

func (n logNegation) OTTLExpression() (ottlValue, error) {
	v, err := n.logFilterExpr.OTTLExpression()
	if err != nil {
		return nil, err
	}
	return ottlNot(v), nil
}

func (n logNegation) String() string {
	return fmt.Sprintf("NOT %s", n.logFilterExpr.String())
}

// unquoteTextOrString returns text literals as-is and unquotes double-quoted string literals.
func unquoteTextOrString(in string) (string, error) {
	if len(in) > 0 && in[0] == '"' {
		return unquoteFilterString(in[1 : len(in)-1])
	}
	return in, nil
}

// unquoteFilterString replaces Cloud Logging filter escape sequences with the characters they represent.
// It assumes the surrounding double quotes have already been stripped.
func unquoteFilterString(in string) (string, error) {
	var buf strings.Builder
	buf.Grow(3 * len(in) / 2)

	r := strings.NewReader(in)
	for {
		c, _, err := r.ReadRune()
		if err != nil {
			break
		}
		if c != '\\' {
			buf.WriteRune(c)
			continue
		}
		c, _, err = r.ReadRune()
		if err != nil {
			buf.WriteRune('\\')
			break
		}
		switch c {
		case ',', ':', '=', '<', '>', '+', '~', '"', '\\', '.', '*':
			buf.WriteRune(c)
		case 'u':
			digits := make([]byte, 4)
			n, _ := r.Read(digits)
			digits = digits[:n]
			codepoint, err := strconv.ParseUint(string(digits), 16, 16)
			if n < 4 || err != nil {
				buf.WriteRune('\\')
				buf.WriteRune('u')
				buf.Write(digits)
				break
			}
			buf.WriteRune(rune(codepoint))
		case '0', '1', '2', '3', '4', '5', '6', '7':
			digits := []byte{byte(c)}
			for len(digits) < 3 {
				b, err := r.ReadByte()
				if err != nil {
					break
				}
				if b < '0' || b > '7' {
					_ = r.UnreadByte()
					break
				}
				digits = append(digits, b)
				if digits[0] > '3' && len(digits) == 2 {
					break
				}
			}
			codepoint, err := strconv.ParseUint(string(digits), 8, 8)
			if err != nil {
				buf.WriteRune('\\')
				buf.Write(digits)
				break
			}
			buf.WriteRune(rune(codepoint))
		case 'x':
			digits := make([]byte, 2)
			n, _ := r.Read(digits)
			digits = digits[:n]
			codepoint, err := strconv.ParseUint(string(digits), 16, 8)
			if n < 2 || err != nil {
				buf.WriteRune('\\')
				buf.WriteRune('x')
				buf.Write(digits)
				break
			}
			buf.WriteRune(rune(codepoint))
		case 'a':
			buf.WriteRune('\a')
		case 'b':
			buf.WriteRune('\b')
		case 'f':
			buf.WriteRune('\f')
		case 'n':
			buf.WriteRune('\n')
		case 'r':
			buf.WriteRune('\r')
		case 't':
			buf.WriteRune('\t')
		case 'v':
			buf.WriteRune('\v')
		default:
			return "", fmt.Errorf(`invalid escape sequence: \%s`, string(c))
		}
	}
	return buf.String(), nil
}

// Lexer and recursive-descent parser for the Cloud Logging filter subset (confgenerator/filter/internal/filter.bnf).

type filterTokenKind int

const (
	tokEOF filterTokenKind = iota
	tokWS
	tokDot
	tokLParen
	tokRParen
	tokMinus
	tokComparator
	tokText
	tokString
)

type filterToken struct {
	kind filterTokenKind
	lit  string
}

func isFilterWSRune(r rune) bool {
	switch r {
	case ' ', '\r', '\t', '\u000C', '\u00A0', '\n':
		return true
	}
	return false
}

func isFilterStartRune(r rune) bool {
	return r == '!' ||
		(r >= '#' && r <= '\'') ||
		r == '*' ||
		r == '/' ||
		r == ';' ||
		r == '?' ||
		r == '@' ||
		(r >= 'A' && r <= 'Z') ||
		r == '[' ||
		r == ']' ||
		(r >= '^' && r <= '}') ||
		(r >= '\u00a1' && r <= '\ufffe')
}

func isFilterMidRune(r rune) bool {
	return isFilterStartRune(r) || (r >= '0' && r <= '9') || r == '+' || r == '-'
}

func isHexDigitByte(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func scanFilterTextEsc(s string, pos int) (int, bool) {
	if pos+1 >= len(s) || s[pos] != '\\' {
		return 0, false
	}
	switch c := s[pos+1]; c {
	case ',', ':', '=', '<', '>', '+', '~', '"', '\\', '.', '*':
		return pos + 2, true
	case 'u':
		if pos+6 <= len(s) &&
			isHexDigitByte(s[pos+2]) && isHexDigitByte(s[pos+3]) &&
			isHexDigitByte(s[pos+4]) && isHexDigitByte(s[pos+5]) {
			return pos + 6, true
		}
		return 0, false
	case 'x':
		if pos+4 <= len(s) && isHexDigitByte(s[pos+2]) && isHexDigitByte(s[pos+3]) {
			return pos + 4, true
		}
		return 0, false
	default:
		if c >= '0' && c <= '7' {
			end := pos + 2
			maxLen := 3
			if c > '3' {
				maxLen = 2
			}
			for end < len(s) && end-(pos+1) < maxLen && s[end] >= '0' && s[end] <= '7' {
				end++
			}
			return end, true
		}
		return 0, false
	}
}

func isValidStringCharRune(r rune) bool {
	return isFilterWSRune(r) ||
		r == '!' ||
		(r >= '#' && r <= '[') ||
		(r >= ']' && r <= '~') ||
		(r >= '\u00a1' && r <= '\ufffe')
}

func tokenizeFilter(s string) ([]filterToken, error) {
	var tokens []filterToken
	pos := 0
	for pos < len(s) {
		r, width := utf8.DecodeRuneInString(s[pos:])
		if r == utf8.RuneError && width == 1 {
			return nil, fmt.Errorf("invalid UTF-8 at offset %d", pos)
		}

		// Whitespace
		if isFilterWSRune(r) {
			start := pos
			pos += width
			for pos < len(s) {
				nr, nw := utf8.DecodeRuneInString(s[pos:])
				if !isFilterWSRune(nr) {
					break
				}
				pos += nw
			}
			tokens = append(tokens, filterToken{kind: tokWS, lit: s[start:pos]})
			continue
		}

		// Single-character punctuation
		switch r {
		case '.':
			tokens = append(tokens, filterToken{kind: tokDot, lit: "."})
			pos++
			continue
		case '(':
			tokens = append(tokens, filterToken{kind: tokLParen, lit: "("})
			pos++
			continue
		case ')':
			tokens = append(tokens, filterToken{kind: tokRParen, lit: ")"})
			pos++
			continue
		}

		// Two-character comparators
		if pos+2 <= len(s) {
			switch two := s[pos : pos+2]; two {
			case "<=", ">=", "!=", "=~", "!~":
				tokens = append(tokens, filterToken{kind: tokComparator, lit: two})
				pos += 2
				continue
			}
		}

		// Single-character comparators
		switch r {
		case '<', '>', '=', ':':
			tokens = append(tokens, filterToken{kind: tokComparator, lit: s[pos : pos+1]})
			pos++
			continue
		}

		// Quoted string
		if r == '"' {
			start := pos
			pos++
			closed := false
			for pos < len(s) {
				cr, cw := utf8.DecodeRuneInString(s[pos:])
				if cr == '"' {
					pos += cw
					closed = true
					break
				}
				if cr == '\\' {
					pos += cw
					if pos < len(s) {
						_, nw := utf8.DecodeRuneInString(s[pos:])
						pos += nw
					}
					continue
				}
				if !isValidStringCharRune(cr) {
					return nil, fmt.Errorf("invalid character %q in string at offset %d", cr, pos)
				}
				pos += cw
			}
			if !closed {
				return nil, fmt.Errorf("unterminated string literal at offset %d", start)
			}
			tokens = append(tokens, filterToken{kind: tokString, lit: s[start:pos]})
			continue
		}

		// Text literal: (_start_char | _number_prefix | _text_esc) {_mid_char | _text_esc}
		start := pos
		if nextPos, ok := scanFilterTextEsc(s, pos); ok {
			pos = nextPos
		} else if isFilterStartRune(r) || (r >= '0' && r <= '9') {
			pos += width
		} else if r == '-' && pos+1 < len(s) && s[pos+1] >= '0' && s[pos+1] <= '9' {
			pos += 2
		} else if r == '-' {
			tokens = append(tokens, filterToken{kind: tokMinus, lit: "-"})
			pos++
			continue
		} else {
			return nil, fmt.Errorf("unexpected character %q at offset %d", r, pos)
		}

		for pos < len(s) {
			if nextPos, ok := scanFilterTextEsc(s, pos); ok {
				pos = nextPos
				continue
			}
			mr, mw := utf8.DecodeRuneInString(s[pos:])
			if isFilterMidRune(mr) {
				pos += mw
				continue
			}
			break
		}
		tokens = append(tokens, filterToken{kind: tokText, lit: s[start:pos]})
	}
	tokens = append(tokens, filterToken{kind: tokEOF})
	return tokens, nil
}

type filterParser struct {
	tokens []filterToken
	pos    int
}

func (p *filterParser) peek() filterToken {
	return p.tokens[p.pos]
}

func (p *filterParser) next() filterToken {
	tok := p.tokens[p.pos]
	if tok.kind != tokEOF {
		p.pos++
	}
	return tok
}

func (p *filterParser) skipWS() bool {
	if p.peek().kind == tokWS {
		p.next()
		return true
	}
	return false
}

func parseFilterInput(s string) (logFilterExpr, error) {
	tokens, err := tokenizeFilter(s)
	if err != nil {
		return nil, err
	}
	p := &filterParser{tokens: tokens}
	p.skipWS()
	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	p.skipWS()
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q", p.peek().lit)
	}
	return expr.Simplify(), nil
}

func (p *filterParser) parseExpression() (logFilterExpr, error) {
	first, err := p.parseAmbiguousSequence()
	if err != nil {
		return nil, err
	}
	conj := newLogConjunction(first)
	for {
		saved := p.pos
		p.skipWS()
		if p.peek().kind == tokText && p.peek().lit == "AND" {
			p.next()
			p.skipWS()
			rhs, err := p.parseAmbiguousSequence()
			if err != nil {
				return nil, err
			}
			conj = conj.Append(rhs)
			continue
		}
		p.pos = saved
		break
	}
	return conj, nil
}

func (p *filterParser) parseAmbiguousSequence() (logFilterExpr, error) {
	first, err := p.parseAmbiguousFactor()
	if err != nil {
		return nil, err
	}
	conj := newLogConjunction(first)
	for {
		saved := p.pos
		if !p.skipWS() {
			break
		}
		tok := p.peek()
		if tok.kind == tokEOF || tok.kind == tokRParen ||
			(tok.kind == tokText && (tok.lit == "AND" || tok.lit == "OR")) {
			p.pos = saved
			break
		}
		rhs, err := p.parseAmbiguousFactor()
		if err != nil {
			return nil, err
		}
		conj = conj.Append(rhs)
	}
	return conj, nil
}

func (p *filterParser) parseAmbiguousFactor() (logFilterExpr, error) {
	first, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	disj := newLogDisjunction(first)
	for {
		saved := p.pos
		p.skipWS()
		if p.peek().kind == tokText && p.peek().lit == "OR" {
			p.next()
			p.skipWS()
			rhs, err := p.parseTerm()
			if err != nil {
				return nil, err
			}
			disj = disj.Append(rhs)
			continue
		}
		p.pos = saved
		break
	}
	return disj, nil
}

func (p *filterParser) parseTerm() (logFilterExpr, error) {
	tok := p.peek()
	if tok.kind == tokText && tok.lit == "NOT" {
		p.next()
		p.skipWS()
		prim, err := p.parsePrimitive()
		if err != nil {
			return nil, err
		}
		return &logNegation{prim}, nil
	}
	if tok.kind == tokMinus {
		p.next()
		prim, err := p.parsePrimitive()
		if err != nil {
			return nil, err
		}
		return &logNegation{prim}, nil
	}
	return p.parsePrimitive()
}

func (p *filterParser) parsePrimitive() (logFilterExpr, error) {
	if p.peek().kind == tokLParen {
		p.next()
		p.skipWS()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.peek().kind != tokRParen {
			return nil, fmt.Errorf("expected ')', got %q", p.peek().lit)
		}
		p.next()
		return expr, nil
	}
	return p.parseRestriction()
}

func (p *filterParser) parseRestriction() (logFilterExpr, error) {
	lhs, err := p.parseMember()
	if err != nil {
		return nil, err
	}
	saved := p.pos
	p.skipWS()
	if p.peek().kind == tokComparator {
		op := p.next().lit
		p.skipWS()
		rhs, err := p.parseMember()
		if err != nil {
			return nil, err
		}
		r, err := newLogRestriction(lhs, op, rhs)
		if err != nil {
			return nil, err
		}
		return *r, nil
	}
	p.pos = saved
	r, err := newLogRestriction(lhs, "GLOBAL", nil)
	if err != nil {
		return nil, err
	}
	return *r, nil
}

func (p *filterParser) parseMember() (logTarget, error) {
	tok := p.peek()
	if tok.kind != tokText && tok.kind != tokString {
		return nil, fmt.Errorf("expected field or value, got %q", tok.lit)
	}
	target := logTarget{p.next().lit}
	for p.peek().kind == tokDot {
		p.next()
		part := p.peek()
		if part.kind != tokText && part.kind != tokString {
			return nil, fmt.Errorf("expected field part after '.', got %q", part.lit)
		}
		target = append(target, p.next().lit)
	}
	return target, nil
}
