package agentservice

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const minClaudeCLIVersion = "2.1.0"

// VersionParser defines interface for agent-specific version parsing and verification.
type VersionParser interface {
	Parse(raw string) (major, minor, patch int, err error)
	Check(raw string) error
}

var versionRegex = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// GetVersionParser returns the VersionParser implementation for the given agent.
func GetVersionParser(agentName string) VersionParser {
	switch agentName {
	case "claudecode":
		return &ClaudeVersionParser{}
	case "codex":
		return &CodexVersionParser{}
	default:
		return nil
	}
}

type ClaudeVersionParser struct{}

func (p *ClaudeVersionParser) Parse(raw string) (major, minor, patch int, err error) {
	versionStr := versionRegex.FindString(raw)
	if versionStr == "" {
		return 0, 0, 0, fmt.Errorf("invalid version format: %q", raw)
	}
	segments := strings.Split(versionStr, ".")
	major, _ = strconv.Atoi(segments[0])
	minor, _ = strconv.Atoi(segments[1])
	if len(segments) >= 3 {
		patch, _ = strconv.Atoi(segments[2])
	}
	return major, minor, patch, nil
}

func (p *ClaudeVersionParser) Check(raw string) error {
	if raw == "" || raw == "unavailable" {
		return nil
	}
	major, minor, _, err := p.Parse(raw)
	if err != nil {
		return fmt.Errorf("failed to parse CLI version %q: %w", raw, err)
	}
	minMajor, minMinor, _, _ := p.Parse(minClaudeCLIVersion)
	if major < minMajor || (major == minMajor && minor < minMinor) {
		return fmt.Errorf(
			"Claude Code CLI version %s is not supported. Minimum required: %s. Run \"claude update\" to upgrade",
			raw, minClaudeCLIVersion,
		)
	}
	return nil
}

type CodexVersionParser struct{}

func (p *CodexVersionParser) Parse(raw string) (major, minor, patch int, err error) {
	versionStr := versionRegex.FindString(raw)
	if versionStr == "" {
		return 0, 0, 0, fmt.Errorf("invalid version format: %q", raw)
	}
	segments := strings.Split(versionStr, ".")
	major, _ = strconv.Atoi(segments[0])
	minor, _ = strconv.Atoi(segments[1])
	if len(segments) >= 3 {
		patch, _ = strconv.Atoi(segments[2])
	}
	return major, minor, patch, nil
}

func (p *CodexVersionParser) Check(raw string) error {
	return nil
}
