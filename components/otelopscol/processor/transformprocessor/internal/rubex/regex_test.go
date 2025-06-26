// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rubex

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
)

var good_re = []string{
	``,
	`.`,
	`^.$`,
	`a`,
	`a*`,
	`a+`,
	`a?`,
	`a|b`,
	`a*|b*`,
	`(a*|b)(c*|d)`,
	`[a-z]`,
	`[a-abc-c\-\]\[]`,
	`[a-z]+`,
	//`[]`, //this is not considered as good by ruby/javascript regex
	`[abc]`,
	`[^1234]`,
	`[^\n]`,
	`\!\\`,
}

type stringError struct {
	re  string
	err error
}

var bad_re = []stringError{
	{`*`, errors.New("target of repeat operator is not specified")},
	{`+`, errors.New("target of repeat operator is not specified")},
	{`?`, errors.New("target of repeat operator is not specified")},
	{`(abc`, errors.New("end pattern with unmatched parenthesis")},
	{`abc)`, errors.New("unmatched close parenthesis")},
	{`x[a-z`, errors.New("premature end of char-class")},
	//{`abc]`, Err}, //this is not considered as bad by ruby/javascript regex; nor are the following commented out regex patterns
	{`abc[`, errors.New("premature end of char-class")},
	{`[z-a]`, errors.New("empty range in char class")},
	{`abc\`, errors.New("end pattern at escape")},
	//{`a**`, Err},
	//{`a*+`, Err},
	//{`a??`, Err},
	//{`\x`, Err},
}

func runParallel(testFunc func(chan bool), concurrency int) {
	runtime.GOMAXPROCS(4)
	done := make(chan bool, concurrency)
	for i := 0; i < concurrency; i++ {
		go testFunc(done)
	}
	for i := 0; i < concurrency; i++ {
		<-done
		<-done
	}
	runtime.GOMAXPROCS(1)
}

const numConcurrentRuns = 200

func compileTest(t *testing.T, expr string, error error) *Regexp {
	re, err := Compile(expr)
	if (error == nil && err != error) || (error != nil && err.Error() != error.Error()) {
		t.Error("compiling `", expr, "`; unexpected error: ", err.Error())
	}
	return re
}

func TestGoodCompile(t *testing.T) {
	testFunc := func(done chan bool) {
		done <- false
		for i := 0; i < len(good_re); i++ {
			compileTest(t, good_re[i], nil)
		}
		done <- true
	}
	runParallel(testFunc, numConcurrentRuns)
}

func TestBadCompile(t *testing.T) {
	for i := 0; i < len(bad_re); i++ {
		compileTest(t, bad_re[i].re, bad_re[i].err)
	}
}

func matchTest(t *testing.T, test *FindTest) {
	re := compileTest(t, test.pat, nil)
	if re == nil {
		return
	}
	m := re.MatchString(test.text)
	if m != (len(test.matches) > 0) {
		t.Errorf("MatchString failure on %s: %t should be %t", test.pat, m, len(test.matches) > 0)
	}
	// now try bytes
	m = re.Match([]byte(test.text))
	if m != (len(test.matches) > 0) {
		t.Errorf("Match failure on %s: %t should be %t", test.pat, m, len(test.matches) > 0)
	}
}

func TestMatch(t *testing.T) {
	for _, test := range findTests {
		matchTest(t, &test)
	}
}

/* how to match $ as itself */
func TestPattern1(t *testing.T) {
	re := MustCompile(`b\$a`)
	if !re.MatchString("b$a") {
		t.Errorf("expect to match\n")
	}
	re = MustCompile("b\\$a")
	if !re.MatchString("b$a") {
		t.Errorf("expect to match 2\n")
	}
}

/* how to use $ as the end of line */
func TestPattern2(t *testing.T) {
	re := MustCompile("a$")
	if !re.MatchString("a") {
		t.Errorf("expect to match\n")
	}
	if re.MatchString("ab") {
		t.Errorf("expect to mismatch\n")
	}
}

type numSubexpCase struct {
	input    string
	expected int
}

var numSubexpCases = []numSubexpCase{
	{``, 0},
	{`.*`, 0},
	{`abba`, 0},
	{`ab(b)a`, 1},
	{`ab(.*)a`, 1},
	{`(.*)ab(.*)a`, 2},
	{`(.*)(ab)(.*)a`, 3},
	{`(.*)((a)b)(.*)a`, 4},
	{`(.*)(\(ab)(.*)a`, 3},
	{`(.*)(\(a\)b)(.*)a`, 3},
}

func TestNumSubexp(t *testing.T) {
	for _, c := range numSubexpCases {
		re := MustCompile(c.input)
		n := re.NumSubexp()
		if n != c.expected {
			t.Errorf("NumSubexp for %q returned %d, expected %d", c.input, n, c.expected)
		}
	}
}

// For each pattern/text pair, what is the expected output of each function?
// We can derive the textual results from the indexed results, the non-submatch
// results from the submatched results, the single results from the 'all' results,
// and the byte results from the string results. Therefore the table includes
// only the FindAllStringSubmatchIndex result.
type FindTest struct {
	pat     string
	text    string
	matches [][]int
}

func (t FindTest) String() string {
	return fmt.Sprintf("pattern: %#q text: %#q", t.pat, t.text)
}

var findTests = []FindTest{
	{``, ``, build(1, 0, 0)},
	{`^abcdefg`, "abcdefg", build(1, 0, 7)},
	{`a+`, "baaab", build(1, 1, 4)},
	{"abcd..", "abcdef", build(1, 0, 6)},
	{`a`, "a", build(1, 0, 1)},
	{`x`, "y", nil},
	{`b`, "abc", build(1, 1, 2)},
	{`.`, "a", build(1, 0, 1)},
	{`.*`, "abcdef", build(2, 0, 6, 6, 6)},
	{`^`, "abcde", build(1, 0, 0)},
	{`$`, "abcde", build(1, 5, 5)},
	{`^abcd$`, "abcd", build(1, 0, 4)},
	{`^bcd'`, "abcdef", nil},
	{`^abcd$`, "abcde", nil},
	{`a+`, "baaab", build(1, 1, 4)},
	{`a*`, "baaab", build(4, 0, 0, 1, 4, 4, 4, 5, 5)},
	{`[a-z]+`, "abcd", build(1, 0, 4)},
	{`[^a-z]+`, "ab1234cd", build(1, 2, 6)},
	{`[a\-\]z]+`, "az]-bcz", build(2, 0, 4, 6, 7)},
	{`[^\n]+`, "abcd\n", build(1, 0, 4)},
	{`[日本語]+`, "日本語日本語", build(1, 0, 18)},
	{`日本語+`, "日本語", build(1, 0, 9)},
	{`a*`, "日本語", build(4, 0, 0, 3, 3, 6, 6, 9, 9)},
	{`日本語+`, "日本語語語語", build(1, 0, 18)},
	{`()`, "", build(1, 0, 0, 0, 0)},
	{`(a)`, "a", build(1, 0, 1, 0, 1)},
	{`(.)(.)`, "日a", build(1, 0, 4, 0, 3, 3, 4)},
	{`(.*)`, "", build(1, 0, 0, 0, 0)},
	{`(.*)`, "abcd", build(2, 0, 4, 0, 4, 4, 4, 4, 4)},
	{`(..)(..)`, "abcd", build(1, 0, 4, 0, 2, 2, 4)},
	{`(([^xyz]*)(d))`, "abcd", build(1, 0, 4, 0, 4, 0, 3, 3, 4)},
	{`((a|b|c)*(d))`, "abcd", build(1, 0, 4, 0, 4, 2, 3, 3, 4)},
	{`(((a|b|c)*)(d))`, "abcd", build(1, 0, 4, 0, 4, 0, 3, 2, 3, 3, 4)},
	{"\a\b\f\n\r\t\v", "\a\b\f\n\r\t\v", build(1, 0, 7)},
	{`[\a\b\f\n\r\t\v]+`, "\a\b\f\n\r\t\v", build(1, 0, 7)},

	//{`a*(|(b))c*`, "aacc", build(2, 0, 4, 4, 4)},
	{`(.*).*`, "ab", build(2, 0, 2, 0, 2, 2, 2, 2, 2)},
	{`[.]`, ".", build(1, 0, 1)},
	{`/$`, "/abc/", build(1, 4, 5)},
	{`/$`, "/abc", nil},

	// multiple matches
	{`.`, "abc", build(3, 0, 1, 1, 2, 2, 3)},
	{`(.)`, "abc", build(3, 0, 1, 0, 1, 1, 2, 1, 2, 2, 3, 2, 3)},
	{`.(.)`, "abcd", build(2, 0, 2, 1, 2, 2, 4, 3, 4)},
	{`ab*`, "abbaab", build(3, 0, 3, 3, 4, 4, 6)},
	{`a(b*)`, "abbaab", build(3, 0, 3, 1, 3, 3, 4, 4, 4, 4, 6, 5, 6)},

	// fixed bugs
	{`ab$`, "cab", build(1, 1, 3)},
	{`axxb$`, "axxcb", nil},
	{`data`, "daXY data", build(1, 5, 9)},
	{`da(.)a$`, "daXY data", build(1, 5, 9, 7, 8)},
	{`zx+`, "zzx", build(1, 1, 3)},

	// can backslash-escape any punctuation
	{`\!\"\#\$\%\&\'\(\)\*\+\,\-\.\/\:\;\<\=\>\?\@\[\\\]\^\_\{\|\}\~`,
		`!"#$%&'()*+,-./:;<=>?@[\]^_{|}~`, build(1, 0, 31)},
	{`[\!\"\#\$\%\&\'\(\)\*\+\,\-\.\/\:\;\<\=\>\?\@\[\\\]\^\_\{\|\}\~]+`,
		`!"#$%&'()*+,-./:;<=>?@[\]^_{|}~`, build(1, 0, 31)},
	{"\\`", "`", build(1, 0, 1)},
	{"[\\`]+", "`", build(1, 0, 1)},

	// long set of matches (longer than startSize)
	{
		".",
		"qwertyuiopasdfghjklzxcvbnm1234567890",
		build(36, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10,
			10, 11, 11, 12, 12, 13, 13, 14, 14, 15, 15, 16, 16, 17, 17, 18, 18, 19, 19, 20,
			20, 21, 21, 22, 22, 23, 23, 24, 24, 25, 25, 26, 26, 27, 27, 28, 28, 29, 29, 30,
			30, 31, 31, 32, 32, 33, 33, 34, 34, 35, 35, 36),
	},
}

// build is a helper to construct a [][]int by extracting n sequences from x.
// This represents n matches with len(x)/n submatches each.
func build(n int, x ...int) [][]int {
	ret := make([][]int, n)
	runLength := len(x) / n
	j := 0
	for i := range ret {
		ret[i] = make([]int, runLength)
		copy(ret[i], x[j:])
		j += runLength
		if j > len(x) {
			panic("invalid build entry")
		}
	}
	return ret
}

func testSubmatchString(test *FindTest, n int, submatches []int, result []string, t *testing.T) {
	if len(submatches) != len(result)*2 {
		t.Errorf("match %d: expected %d submatches; got %d: %s", n, len(submatches)/2, len(result), test)
		return
	}
	for k := 0; k < len(submatches); k += 2 {
		if submatches[k] == -1 {
			if result[k/2] != "" {
				t.Errorf("match %d: expected nil got %q: %s", n, result, test)
			}
			continue
		}
		expect := test.text[submatches[k]:submatches[k+1]]
		if expect != result[k/2] {
			t.Errorf("match %d: expected %q got %q: %s", n, expect, result, test)
			return
		}
	}
}

func TestFindStringSubmatch(t *testing.T) {
	for _, test := range findTests {
		result := MustCompile(test.pat).FindStringSubmatch(test.text)
		switch {
		case test.matches == nil && result == nil:
			// ok
		case test.matches == nil && result != nil:
			t.Errorf("expected no match; got one: %s", test)
		case test.matches != nil && result == nil:
			t.Errorf("expected match; got none: %s", test)
		case test.matches != nil && result != nil:
			testSubmatchString(&test, 0, test.matches[0], result, t)
		}
	}
}

func testSubmatchIndices(test *FindTest, n int, expect, result []int, t *testing.T) {
	if len(expect) != len(result) {
		t.Errorf("match %d: expected %d matches; got %d: %s", n, len(expect)/2, len(result)/2, test)
		return
	}
	for k, e := range expect {
		if e != result[k] {
			t.Errorf("match %d: submatch error: expected %v got %v: %s", n, expect, result, test)
		}
	}
}

func testFindSubmatchIndex(test *FindTest, result []int, t *testing.T) {
	switch {
	case test.matches == nil && result == nil:
		// ok
	case test.matches == nil && result != nil:
		t.Errorf("expected no match; got one: %s", test)
	case test.matches != nil && result == nil:
		t.Errorf("expected match; got none: %s", test)
	case test.matches != nil && result != nil:
		testSubmatchIndices(test, 0, test.matches[0], result, t)
	}
}

func TestFindSubmatchIndex(t *testing.T) {
	for _, test := range findTests {
		testFindSubmatchIndex(&test, MustCompile(test.pat).FindSubmatchIndex([]byte(test.text)), t)
	}
}
