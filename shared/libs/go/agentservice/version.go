package agentservice

import (
	"fmt"
	"strconv"
	"strings"
)

const minClaudeCLIVersion = "2.1.0"

// parseCLIVersion extracts major.minor.patch from a version string like "2.1.169 (Claude Code)".
// Returns (0,0,0, err) if parsing fails.
func parseCLIVersion(raw string) (major, minor, patch int, err error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return 0, 0, 0, fmt.Errorf("empty version string")
	}
	versionStr := parts[0]

	segments := strings.SplitN(versionStr, ".", 3)
	if len(segments) < 2 {
		return 0, 0, 0, fmt.Errorf("invalid version format: %q", versionStr)
	}

	major, err = strconv.Atoi(segments[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid major version: %w", err)
	}
	minor, err = strconv.Atoi(segments[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid minor version: %w", err)
	}
	if len(segments) >= 3 {
		patch, err = strconv.Atoi(segments[2])
		if err != nil {
			// Patch part may contain non-numeric suffix; treat as 0.
			return major, minor, 0, nil
		}
	}
	return major, minor, patch, nil
}

// checkCLIVersion validates that the given version meets the minimum requirement.
// Returns nil if valid, or an error with a user-friendly message.
func checkCLIVersion(raw string, minVersion string) error {
	if raw == "" || raw == "unavailable" {
		return nil // CLI not found; handled separately.
	}

	major, minor, _, err := parseCLIVersion(raw)
	if err != nil {
		return fmt.Errorf("failed to parse CLI version %q: %w", raw, err)
	}

	minMajor, minMinor, _, _ := parseCLIVersion(minVersion)

	if major < minMajor || (major == minMajor && minor < minMinor) {
		return fmt.Errorf(
			"Claude Code CLI version %s is not supported. Minimum required: %s. Run \"claude update\" to upgrade",
			raw, minVersion,
		)
	}
	return nil
}
