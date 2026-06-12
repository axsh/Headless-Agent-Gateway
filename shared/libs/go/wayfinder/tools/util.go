package tools

import "strings"

// countOccurrences counts non-overlapping occurrences of substr in s.
func countOccurrences(s, substr string) int {
	return strings.Count(s, substr)
}

// replaceFirst replaces the first occurrence of old with new in s.
func replaceFirst(s, old, new string) string {
	idx := strings.Index(s, old)
	if idx < 0 {
		return s
	}
	return s[:idx] + new + s[idx+len(old):]
}
