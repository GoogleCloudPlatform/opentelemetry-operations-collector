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
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

var (
	// ErrEmptyIncludePattern is returned when an include pattern is empty.
	ErrEmptyIncludePattern = errors.New("include pattern cannot be empty")
	// ErrPathTraversal is returned when an include pattern contains prohibited path traversal ('..').
	ErrPathTraversal = errors.New("include pattern contains prohibited path traversal ('..')")
	// ErrSensitiveDirectory is returned when an include pattern targets a forbidden sensitive directory.
	ErrSensitiveDirectory = errors.New("include pattern targets a forbidden sensitive directory")
	// ErrDisallowedLogRoot is returned when an include pattern is outside permitted OS log roots.
	ErrDisallowedLogRoot = errors.New("include pattern is outside permitted OS log roots")
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

// forbiddenPrefixes contains forbidden sensitive system paths on Linux/Unix.
var forbiddenPrefixes = []string{
	"/etc",
	"/root",
	"/proc",
	"/sys",
	"/var/run",
	"/run",
	"/dev",
	"/boot",
	"/bin",
	"/sbin",
	"/usr/bin",
	"/usr/sbin",
}

// winEnvVarRegexp matches Windows %VAR% syntax.
var winEnvVarRegexp = regexp.MustCompile(`%([A-Za-z0-9_]+)%`)

// PathAnatomy breaks down an absolute canonical pattern into its structural components.
type PathAnatomy struct {
	Canonical   string
	IsWindows   bool
	DriveLetter string   // e.g. "C"
	Root        string   // e.g. "var", "opt", "srv", "programdata", "inetpub"
	Segments    []string // directories between Root and Leaf
	Leaf        string   // trailing filename, glob, or empty if bare directory
}

// ValidateIncludePattern validates that a pattern adheres to security guardrails
// across the 4-stage pipeline, verifying all branches if brace expansion is present.
func ValidateIncludePattern(rawPattern string) error {
	// Stage 1: Lexical Sanitization & Syntax Check
	if err := SanitizePattern(rawPattern); err != nil {
		return err
	}

	// Expand any brace expansions to validate every branch individually
	branches, err := expandBraces(rawPattern)
	if err != nil {
		return fmt.Errorf("%w: %q: %v", ErrInvalidGlobPattern, rawPattern, err)
	}
	if len(branches) > maxBraceExpansions {
		return fmt.Errorf("%w: %d > %d", ErrTooManyBraceExpansions, len(branches), maxBraceExpansions)
	}

	for _, branch := range branches {
		if err := validateSingleBranch(branch); err != nil {
			return err
		}
	}
	return nil
}

// validateSingleBranch executes Stages 2, 3, and 4 on a single concrete branch.
func validateSingleBranch(pattern string) error {
	// Stage 2: Canonical Normalization
	canonical := NormalizePattern(pattern)

	// Stage 3: Structural Anatomy & Segment Parsing
	anatomy, err := ParseAnatomy(pattern, canonical)
	if err != nil {
		return err
	}

	// Stage 4: Allowed Root Verification
	return VerifyLogRoot(anatomy, pattern)
}

// SanitizePattern enforces basic lexical integrity and glob syntax on rawPattern (Stage 1).
func SanitizePattern(pattern string) error {
	if pattern == "" {
		return ErrEmptyIncludePattern
	}

	// Reject any path traversal segments.
	if strings.Contains(pattern, "..") {
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

// NormalizePattern converts an include pattern into its canonical form (Stage 2).
// For Windows paths, environment variables are expanded, separators become '/',
// and the path is normalized (drive letter uppercase, path lowercased).
// For Linux/Unix paths, separators become '/' and casing is preserved.
func NormalizePattern(pattern string) string {
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

// ParseAnatomy parses a normalized pattern into structural segments (Stage 3).
func ParseAnatomy(raw string, canonical string) (*PathAnatomy, error) {
	isLinuxAbs := strings.HasPrefix(canonical, "/")
	isWindowsAbs := isWindowsAbsolute(canonical) || isWindowsAbsolute(raw)
	if !isLinuxAbs && !isWindowsAbs {
		return nil, fmt.Errorf("%w: %q", ErrRelativePath, raw)
	}

	if isBareDirectory(raw, canonical) {
		return nil, fmt.Errorf("%w: %q (must specify filename or glob wildcard, e.g. /** or /*.log)", ErrBareDirectoryPath, raw)
	}

	anatomy := &PathAnatomy{
		Canonical: canonical,
		IsWindows: isWindowsAbs,
	}

	if isLinuxAbs {
		trimmed := strings.TrimPrefix(canonical, "/")
		parts := strings.Split(trimmed, "/")
		if len(parts) > 0 {
			anatomy.Root = parts[0]
			if len(parts) > 1 {
				anatomy.Segments = parts[1 : len(parts)-1]
				anatomy.Leaf = parts[len(parts)-1]
			} else {
				anatomy.Leaf = parts[0]
			}
		}
		return anatomy, nil
	}

	// Windows absolute path
	drive, rest, ok := splitWindowsDrive(canonical)
	if !ok {
		if isWindowsAbsolute(raw) {
			return nil, fmt.Errorf("%w: %q", ErrDisallowedLogRoot, raw)
		}
		return nil, fmt.Errorf("%w: %q", ErrRelativePath, raw)
	}
	anatomy.DriveLetter = drive
	parts := strings.Split(rest, "/")
	if len(parts) > 0 {
		anatomy.Root = parts[0]
		if len(parts) > 1 {
			anatomy.Segments = parts[1 : len(parts)-1]
			anatomy.Leaf = parts[len(parts)-1]
		} else {
			anatomy.Leaf = parts[0]
		}
	}
	return anatomy, nil
}

// VerifyLogRoot verifies that the anatomy falls within permitted log roots (Stage 4).
func VerifyLogRoot(anatomy *PathAnatomy, rawPattern string) error {
	// Check forbidden sensitive system directories on Linux/Unix.
	if !anatomy.IsWindows {
		for _, f := range forbiddenPrefixes {
			if anatomy.Canonical == f || strings.HasPrefix(anatomy.Canonical, f+"/") || strings.HasPrefix(anatomy.Canonical, f+"\\") {
				return fmt.Errorf("%w: %q", ErrSensitiveDirectory, rawPattern)
			}
		}

		// In test environments, allow patterns within FILELOG_TEST_ALLOWED_ROOT when running under go test.
		if matchesTestAllowedRoot(anatomy.Canonical, true, "") {
			return nil
		}

		// Allowed Linux roots:
		// 1. /var/log/**
		// 2. /opt/**/logs/**
		// 3. /srv/**/logs/**
		if anatomy.Root == "var" && len(anatomy.Segments) > 0 && anatomy.Segments[0] == "log" {
			return nil
		}
		if (anatomy.Root == "opt" || anatomy.Root == "srv") && slices.Contains(anatomy.Segments, "logs") {
			return nil
		}

		return fmt.Errorf("%w: %q", ErrDisallowedLogRoot, rawPattern)
	}

	// Windows paths:
	// In test environments, allow patterns within FILELOG_TEST_ALLOWED_ROOT when running under go test.
	if matchesTestAllowedRoot(anatomy.Canonical, false, strings.ToLower(anatomy.Canonical)) {
		return nil
	}

	// Allowed Windows roots:
	// 1. <Drive>:\ProgramData\**\logs\** (including %ProgramData%\**\logs\**)
	// 2. <Drive>:\inetpub\logs\** (including %SystemDrive%\inetpub\logs\**)
	if anatomy.Root == "programdata" && slices.Contains(anatomy.Segments, "logs") {
		return nil
	}

	if anatomy.Root == "inetpub" && len(anatomy.Segments) > 0 && anatomy.Segments[0] == "logs" {
		return nil
	}

	return fmt.Errorf("%w: %q", ErrDisallowedLogRoot, rawPattern)
}

func isBareDirectory(pattern string, canonical string) bool {
	if strings.HasSuffix(pattern, "/") || strings.HasSuffix(pattern, "\\") {
		return true
	}
	if pattern == "/var/log" || canonical == "/var/log" {
		return true
	}
	normLower := strings.ToLower(canonical)
	if strings.HasSuffix(pattern, "/logs") || strings.HasSuffix(pattern, "\\logs") || strings.HasSuffix(normLower, "/logs") {
		return true
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

// expandWindowsEnv expands Windows %VAR% syntax (e.g. %ProgramData%, %SystemDrive%).
// It checks os.Getenv(varName) (case-insensitively). If unset, it falls back to standard Windows defaults:
//   - %ProgramData% -> C:\ProgramData
//   - %SystemDrive% -> C:
func expandWindowsEnv(pattern string) string {
	if !strings.Contains(pattern, "%") {
		return pattern
	}
	return winEnvVarRegexp.ReplaceAllStringFunc(pattern, func(m string) string {
		varName := m[1 : len(m)-1]
		var defaultVal string
		switch {
		case strings.EqualFold(varName, "ProgramData"):
			defaultVal = `C:\ProgramData`
		case strings.EqualFold(varName, "SystemDrive"):
			defaultVal = `C:`
		default:
			return m
		}
		val := os.Getenv(varName)
		if val == "" {
			if v := os.Getenv(strings.ToUpper(varName)); v != "" {
				val = v
			} else if v := os.Getenv(strings.ToLower(varName)); v != "" {
				val = v
			}
		}
		if val == "" {
			val = defaultVal
		}
		return strings.TrimRight(val, "/\\")
	})
}

// matchesTestAllowedRoot checks whether pattern falls within the directory specified
// by FILELOG_TEST_ALLOWED_ROOT.
//
// Gated strictly behind testing.Testing(): in non-test (production) binaries,
// testing.Testing() returns false, making this function a total no-op and ensuring
// FILELOG_TEST_ALLOWED_ROOT cannot be used to bypass path validation in production.
func matchesTestAllowedRoot(pattern string, isLinuxAbs bool, winNorm string) bool {
	if !testing.Testing() {
		return false
	}
	testRoot := os.Getenv("FILELOG_TEST_ALLOWED_ROOT")
	if testRoot == "" {
		return false
	}
	cleanTestRoot := filepath.Clean(testRoot)
	if isLinuxAbs {
		return pattern == cleanTestRoot || strings.HasPrefix(pattern, cleanTestRoot+"/")
	}
	normTestRoot := strings.ToLower(strings.ReplaceAll(cleanTestRoot, "\\", "/"))
	return winNorm == normTestRoot || strings.HasPrefix(winNorm, normTestRoot+"/")
}

// expandBraces expands bash-style brace expressions like "{a,b}".
// If no braces exist, it returns []string{pattern}.
func expandBraces(pattern string) ([]string, error) {
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
					expanded, err := expandBraces(prefix + part + suffix)
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
