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

package filelogsource

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
)

var (
	// ErrEmptyIncludePattern is returned when an include pattern is empty.
	ErrEmptyIncludePattern = errors.New("include pattern cannot be empty")
	// ErrPathTraversal is returned when an include pattern contains prohibited path traversal ('..').
	ErrPathTraversal = errors.New("include pattern contains prohibited path traversal ('..')")
	// ErrDollarNotAllowed is returned when an include pattern contains '$', preventing confmap variable expansion injection.
	ErrDollarNotAllowed = errors.New("include pattern cannot contain '$' (confmap variable expansion not allowed)")
	// ErrRelativePath is returned when an include pattern is not an absolute path.
	ErrRelativePath = errors.New("include pattern must be an absolute path")
	// ErrBareDirectoryPath is returned when an include pattern specifies a bare directory rather than a file pattern.
	ErrBareDirectoryPath = errors.New("include pattern must target files, not bare directory")
	// ErrInvalidGlobPattern is returned when an include pattern contains invalid glob syntax.
	ErrInvalidGlobPattern = errors.New("invalid glob pattern")
	// ErrTooManyBraceExpansions is returned when brace expansion exceeds maximum allowed branches.
	ErrTooManyBraceExpansions = errors.New("brace expansion exceeds maximum branch limit")
)

const maxBraceExpansions = 100

// winEnvVarRegexp matches Windows %VAR% syntax anchored at the start of a pattern.
var winEnvVarRegexp = regexp.MustCompile(`^%([A-Za-z0-9_]+)%`)

var (
	osMu      sync.RWMutex
	currentOS = runtime.GOOS
)

func getOS() string {
	osMu.RLock()
	defer osMu.RUnlock()
	return currentOS
}

func setOSForTest(osName string) func() {
	osMu.Lock()
	prev := currentOS
	currentOS = osName
	osMu.Unlock()
	return func() {
		osMu.Lock()
		currentOS = prev
		osMu.Unlock()
	}
}

// validateIncludePattern validates that a pattern adheres to lexical path and glob guardrails,
// verifying all branches if brace expansion is present.
func validateIncludePattern(rawPattern string) error {
	if err := sanitizePattern(rawPattern); err != nil {
		return err
	}

	branches, err := expandBraces(rawPattern)
	if err != nil {
		if errors.Is(err, ErrTooManyBraceExpansions) {
			return err
		}
		return fmt.Errorf("%w: %q: %v", ErrInvalidGlobPattern, rawPattern, err)
	}

	for _, branch := range branches {
		if err := validateSingleBranch(branch); err != nil {
			return err
		}
	}
	return nil
}

// sanitizePattern enforces basic lexical integrity and glob syntax on rawPattern.
func sanitizePattern(pattern string) error {
	if pattern == "" {
		return ErrEmptyIncludePattern
	}

	// Reject '$' to prevent confmap variable expansion injection (e.g. ${file:...}, ${env:...}).
	if strings.Contains(pattern, "$") {
		return fmt.Errorf("%w: %q", ErrDollarNotAllowed, pattern)
	}

	// Reject bare directory patterns.
	if isBareDirectory(pattern, pattern) {
		return fmt.Errorf("%w: %q (must specify filename or glob wildcard, e.g. /** or /*.log)", ErrBareDirectoryPath, pattern)
	}

	// Reject any path traversal segments ('..').
	if hasPathTraversal(pattern) {
		return fmt.Errorf("%w: %q", ErrPathTraversal, pattern)
	}

	// Validate glob pattern syntax.
	// For Windows paths, normalize separators to '/' before validating glob syntax,
	// so that '\' is not treated as an escaping character by doublestar.
	patternForGlob := pattern
	if isWindowsAbsolute(pattern) {
		patternForGlob = strings.ReplaceAll(pattern, "\\", "/")
	}
	if !doublestar.ValidatePattern(patternForGlob) {
		return fmt.Errorf("%w: %q", ErrInvalidGlobPattern, pattern)
	}

	return nil
}

// validateSingleBranch validates a single concrete branch pattern.
func validateSingleBranch(pattern string) error {
	if hasPathTraversal(pattern) {
		return fmt.Errorf("%w: %q", ErrPathTraversal, pattern)
	}

	canonical := normalizePattern(pattern)
	if hasPathTraversal(canonical) {
		return fmt.Errorf("%w: %q", ErrPathTraversal, pattern)
	}

	// Cross-OS absolute path check:
	// - On Windows, include patterns must target Windows absolute paths (with drive letter or %VAR%).
	// - On non-Windows OSes (Linux, Darwin), include patterns must target absolute paths starting with '/'.
	if err := validateAbsolutePath(pattern, canonical); err != nil {
		return err
	}

	// Reject bare directories
	if isBareDirectory(pattern, canonical) {
		return fmt.Errorf("%w: %q (must specify filename or glob wildcard, e.g. /** or /*.log)", ErrBareDirectoryPath, pattern)
	}

	// Canonical path clean invariant:
	// Path cleaning must not alter the canonical path (which indicates traversal or redundant dot segments were collapsed).
	cleanPath := path.Clean(canonical)
	if cleanPath != canonical {
		return fmt.Errorf("%w: %q (path cleaning collapsed segments: %q)", ErrPathTraversal, pattern, cleanPath)
	}

	// Verify segments for directory traversal
	segments, leaf := splitSegmentsAndLeaf(canonical)
	for _, seg := range segments {
		if isTraversalDirectorySegment(seg) {
			return fmt.Errorf("%w: %q", ErrPathTraversal, pattern)
		}
	}
	if isTraversalSegment(leaf) {
		return fmt.Errorf("%w: %q", ErrPathTraversal, pattern)
	}

	return nil
}

func validateAbsolutePath(raw string, canonical string) error {
	isLinuxAbs := strings.HasPrefix(canonical, "/")
	isWinAbs := isWindowsAbsolute(canonical) || isWindowsAbsolute(raw)

	if getOS() == "windows" {
		if !isWinAbs {
			return fmt.Errorf("%w: %q", ErrRelativePath, raw)
		}
	} else {
		if !isLinuxAbs {
			return fmt.Errorf("%w: %q", ErrRelativePath, raw)
		}
	}
	return nil
}

func isBareDirectory(pattern string, canonical string) bool {
	cleanPat := strings.TrimRight(pattern, " ")
	if cleanPat == "." || cleanPat == ".." {
		return true
	}
	if strings.HasSuffix(cleanPat, "/") || strings.HasSuffix(cleanPat, "\\") {
		return true
	}
	if strings.HasSuffix(cleanPat, "/.") || strings.HasSuffix(cleanPat, "\\.") ||
		strings.HasSuffix(cleanPat, "/..") || strings.HasSuffix(cleanPat, "\\..") ||
		strings.HasSuffix(cleanPat, "/./") || strings.HasSuffix(cleanPat, "\\.\\") {
		return true
	}
	if pattern == "/var/log" || canonical == "/var/log" {
		return true
	}
	normLower := strings.ToLower(canonical)
	if strings.HasSuffix(pattern, "/logs") || strings.HasSuffix(pattern, "\\logs") || strings.HasSuffix(normLower, "/logs") {
		return true
	}
	// Bare root directory: "/" or "C:\" or "C:/" or "C:"
	if canonical == "/" || (len(canonical) >= 2 && isAlpha(canonical[0]) && canonical[1] == ':' && (len(canonical) == 2 || canonical[2:] == "/")) {
		return true
	}
	return false
}

// splitSegmentsAndLeaf splits a canonical path into intermediate directory segments and leaf.
func splitSegmentsAndLeaf(canonical string) ([]string, string) {
	norm := canonical
	if _, rest, ok := splitWindowsDrive(canonical); ok {
		norm = rest
	} else {
		norm = strings.TrimPrefix(norm, "/")
	}
	parts := strings.Split(norm, "/")
	if len(parts) == 0 {
		return nil, ""
	}
	if len(parts) == 1 {
		return nil, parts[0]
	}
	return parts[:len(parts)-1], parts[len(parts)-1]
}

// normalizePattern converts an include pattern into its canonical form.
// For Windows paths, environment variables are expanded, separators become '/',
// and the path is normalized (drive letter uppercase, path lowercased).
// For Linux/Unix paths, separators become '/' and casing is preserved.
func normalizePattern(pattern string) string {
	if isWindowsAbsolute(pattern) {
		expanded := expandWindowsEnv(pattern)
		normalized := strings.ReplaceAll(expanded, "\\", "/")
		if len(normalized) >= 2 && isAlpha(normalized[0]) && normalized[1] == ':' {
			drive := strings.ToUpper(normalized[:1])
			rest := strings.ToLower(normalized[2:])
			return drive + ":" + rest
		}
		return strings.ToLower(normalized)
	}
	return strings.ReplaceAll(pattern, "\\", "/")
}

// isTraversalSegment checks whether a leaf segment represents a traversal component ('..')
// either literally or via glob character classes (e.g. '.[.]', '[.][.]').
func isTraversalSegment(seg string) bool {
	if seg == ".." {
		return true
	}
	if strings.Contains(seg, ".") {
		clean := strings.ReplaceAll(strings.ReplaceAll(seg, "[", ""), "]", "")
		if clean == ".." {
			return true
		}
	}
	return false
}

// isTraversalDirectorySegment checks whether an intermediate directory segment represents
// a traversal component ('..' or '.') either literally, via glob character classes (e.g. '.[.]', '[.][.]', '[.]'),
// or via wildcards that match parent directory entries (e.g. '*..*').
func isTraversalDirectorySegment(seg string) bool {
	if seg == ".." || seg == "." {
		return true
	}
	if strings.Contains(seg, ".") {
		clean := strings.ReplaceAll(strings.ReplaceAll(seg, "[", ""), "]", "")
		if clean == ".." || clean == "." {
			return true
		}
		if matched, _ := doublestar.Match(seg, ".."); matched {
			return true
		}
		if matched, _ := doublestar.Match(seg, "."); matched {
			return true
		}
	}
	return false
}

// hasPathTraversal checks whether pattern contains any path traversal segment ('..' or '.')
// across all expanded branches, including glob character classes (e.g. '.[.]', '[.][.]', '[.]')
// and brace expansions (e.g. '.{.,}.'). Legitimate filenames like 'app..log' or '*..*' as leaf are permitted.
func hasPathTraversal(pattern string) bool {
	branches, err := expandBraces(pattern)
	if err != nil {
		for _, part := range strings.FieldsFunc(pattern, func(r rune) bool {
			return r == '/' || r == '\\' || r == '{' || r == '}' || r == ','
		}) {
			if isTraversalSegment(part) {
				return true
			}
		}
		return false
	}
	for _, b := range branches {
		norm := strings.ReplaceAll(b, "\\", "/")
		parts := strings.Split(norm, "/")
		for i, part := range parts {
			if i < len(parts)-1 {
				if isTraversalDirectorySegment(part) {
					return true
				}
			} else {
				if isTraversalSegment(part) {
					return true
				}
			}
		}
	}
	return false
}

// splitWindowsDrive splits a canonical Windows path (e.g. "C:/programdata/...") into drive and rest.
func splitWindowsDrive(canonical string) (drive, rest string, ok bool) {
	if len(canonical) >= 3 && isAlpha(canonical[0]) && canonical[1] == ':' && canonical[2] == '/' {
		return canonical[:1], canonical[3:], true
	}
	return "", "", false
}

func isWindowsAbsolute(pattern string) bool {
	if len(pattern) >= 3 && isAlpha(pattern[0]) && pattern[1] == ':' && (pattern[2] == '/' || pattern[2] == '\\') {
		return true
	}
	if strings.HasPrefix(pattern, "%") {
		return true
	}
	return false
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// expandWindowsEnv expands Windows %VAR% syntax (e.g. %ProgramData%, %SystemDrive%)
// anchored at the beginning of the pattern.
func expandWindowsEnv(pattern string) string {
	if !strings.HasPrefix(pattern, "%") {
		return pattern
	}
	loc := winEnvVarRegexp.FindStringSubmatchIndex(pattern)
	if loc == nil {
		return pattern
	}
	varName := pattern[loc[2]:loc[3]]
	var defaultVal string
	switch {
	case strings.EqualFold(varName, "ProgramData"):
		defaultVal = `C:\ProgramData`
	case strings.EqualFold(varName, "SystemDrive"):
		defaultVal = `C:`
	default:
		return pattern
	}
	val := defaultVal
	rest := pattern[loc[1]:]
	return strings.TrimRight(val, "/\\") + rest
}

// expandBraces expands bash-style brace expressions like "{a,b}".
// It eagerly bounds the expansion count during recursion to prevent exponential DoS (CWE-400).
func expandBraces(pattern string) ([]string, error) {
	count := 0
	return expandBracesBounded(pattern, &count, maxBraceExpansions)
}

func expandBracesBounded(pattern string, count *int, max int) ([]string, error) {
	start := -1
	depth := 0
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			i++ // skip escaped character
			continue
		}
		if pattern[i] == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if pattern[i] == '}' {
			depth--
			if depth < 0 {
				return nil, errors.New("unmatched '}' in pattern")
			}
			if depth == 0 {
				prefix := pattern[:start]
				suffix := pattern[i+1:]
				body := pattern[start+1 : i]
				parts := splitBraceBody(body)
				var result []string
				for _, part := range parts {
					expanded, err := expandBracesBounded(prefix+part+suffix, count, max)
					if err != nil {
						return nil, err
					}
					result = append(result, expanded...)
				}
				return result, nil
			}
		}
	}
	if depth > 0 {
		return nil, errors.New("unmatched '{' in pattern")
	}
	*count++
	if *count > max {
		return nil, fmt.Errorf("%w: exceeds limit of %d branches", ErrTooManyBraceExpansions, max)
	}
	return []string{pattern}, nil
}

func splitBraceBody(body string) []string {
	var parts []string
	depth := 0
	start := 0
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' {
			i++
			continue
		}
		if body[i] == '{' {
			depth++
		} else if body[i] == '}' {
			depth--
		} else if body[i] == ',' && depth == 0 {
			parts = append(parts, body[start:i])
			start = i + 1
		}
	}
	parts = append(parts, body[start:])
	return parts
}
