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
	"strings"
)

var (
	// ErrEmptyIncludePattern is returned when an include pattern is empty or whitespace-only.
	ErrEmptyIncludePattern = errors.New("include pattern cannot be empty")
	// ErrInvalidGlobPattern is returned when an include pattern contains invalid glob syntax.
	ErrInvalidGlobPattern = errors.New("invalid glob pattern")
)

// validateIncludePattern validates that a pattern is non-empty, non-whitespace,
// and uses only simple `*`, `**`, and `?` wildcards (without `[...]` character
// classes, `{a,b}` brace alternation, or POSIX `\` glob escapes).
func validateIncludePattern(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return ErrEmptyIncludePattern
	}
	if !hasValidGlobSyntax(pattern) {
		return fmt.Errorf("%w: %q", ErrInvalidGlobPattern, pattern)
	}
	return nil
}

func hasValidGlobSyntax(pattern string) bool {
	if strings.ContainsAny(pattern, "[]{}") {
		return false
	}
	if !isWindowsAbsolute(pattern) && strings.Contains(pattern, "\\") {
		return false
	}
	return true
}

// normalizePattern converts an include pattern into its canonical form.
// For Windows paths, separators become '/' and the path is normalized
// (drive letter uppercase, path lowercased).
// For Linux/Unix paths, the pattern is returned unchanged.
func normalizePattern(pattern string) string {
	if isWindowsAbsolute(pattern) {
		normalized := strings.ReplaceAll(pattern, "\\", "/")
		drive := strings.ToUpper(normalized[:1])
		rest := strings.ToLower(normalized[2:])
		return drive + ":" + rest
	}
	return pattern
}

func isWindowsAbsolute(pattern string) bool {
	return len(pattern) >= 3 && isAlpha(pattern[0]) && pattern[1] == ':' && (pattern[2] == '/' || pattern[2] == '\\')
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
