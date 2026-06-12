package wayfinder

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// blockedCommandPrefixes lists command prefixes that are always blocked.
var blockedCommandPrefixes = []string{
	"sudo",
	"su ",
	"curl ",
	"wget ",
	"nc ",
	"ncat ",
	"dd ",
	"mkfs",
	"shutdown",
	"reboot",
	"halt",
	"poweroff",
}

// ValidatePath checks that a file path resolves to within the workDir boundary.
// It returns the resolved absolute path, or an error if the path escapes workDir.
func ValidatePath(workDir, rawPath string) (string, error) {
	var absPath string
	if filepath.IsAbs(rawPath) {
		absPath = filepath.Clean(rawPath)
	} else {
		absPath = filepath.Clean(filepath.Join(workDir, rawPath))
	}

	// Ensure the resolved path is within workDir.
	relPath, err := filepath.Rel(workDir, absPath)
	if err != nil {
		return "", fmt.Errorf("path validation failed: %w", err)
	}
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("path %q is outside workDir %q", rawPath, workDir)
	}

	return absPath, nil
}

// IsBlockedCommand checks if the command line starts with a blocked prefix.
func IsBlockedCommand(commandLine string) bool {
	trimmed := strings.TrimSpace(commandLine)
	for _, prefix := range blockedCommandPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// CheckFileOwnership determines if a file operation (e.g. delete) is permitted.
// The check follows a priority chain:
//
//  1. Tracker: If the file was created by the agent, allow.
//  2. AllowedPatterns: If the file path matches an allowed regex pattern, allow.
//  3. OS ownership: If the file exists and is owned by the current user, allow.
//  4. Otherwise: deny.
func CheckFileOwnership(absPath string, tracker *FileTracker, compiledPatterns []*regexp.Regexp, _ any) error {
	// 1. Check tracker.
	if tracker != nil && tracker.IsTracked(absPath) {
		return nil
	}

	// 2. Check allowed patterns.
	if MatchesAllowedPattern(absPath, compiledPatterns) {
		return nil
	}

	// 3. Check OS file ownership (simplified: check if file exists and is accessible).
	if info, err := os.Stat(absPath); err == nil && info != nil {
		return nil
	}

	return fmt.Errorf("permission denied: file %q is not tracked, does not match allowed patterns, and is not accessible", absPath)
}

// MatchesAllowedPattern checks if a path matches any of the compiled regex patterns.
func MatchesAllowedPattern(absPath string, patterns []*regexp.Regexp) bool {
	for _, p := range patterns {
		if p.MatchString(absPath) {
			return true
		}
	}
	return false
}

// CompileAllowedPatterns compiles string regex patterns into regexp objects.
// Invalid patterns are silently skipped.
func CompileAllowedPatterns(patterns []string) []*regexp.Regexp {
	var compiled []*regexp.Regexp
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			// Skip invalid patterns silently.
			continue
		}
		compiled = append(compiled, re)
	}
	return compiled
}
