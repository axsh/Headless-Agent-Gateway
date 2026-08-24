package codingagent

import "fmt"

// Sandbox mode values for CreateSession / SessionRecord (Codex CLI -s aligned names).
const (
	SandboxModeReadOnly         = "read-only"
	SandboxModeWorkspaceWrite   = "workspace-write"
	SandboxModeDangerFullAccess = "danger-full-access"
)

// ResolveSandboxMode applies session vs server precedence:
//  1. explicit non-empty request mode (must be allowed) -> that mode
//  2. else if serverDisableSandbox -> danger-full-access
//  3. else -> read-only
//
// Empty explicit is treated as unset (fall through to 2/3).
// Unknown explicit returns an error for HTTP 400 mapping.
func ResolveSandboxMode(explicit string, serverDisableSandbox bool) (string, error) {
	if explicit != "" {
		switch explicit {
		case SandboxModeReadOnly, SandboxModeWorkspaceWrite, SandboxModeDangerFullAccess:
			return explicit, nil
		default:
			return "", fmt.Errorf(
				"unsupported sandbox_mode: %q (allowed: %s, %s, %s)",
				explicit, SandboxModeReadOnly, SandboxModeWorkspaceWrite, SandboxModeDangerFullAccess,
			)
		}
	}
	if serverDisableSandbox {
		return SandboxModeDangerFullAccess, nil
	}
	return SandboxModeReadOnly, nil
}

// SandboxModeDisablesSandbox reports whether the mode maps to full CLI sandbox bypass
// (Codex --dangerously-bypass-approvals-and-sandbox / Claude CLAUDE_CODE_SKIP_SANDBOX).
func SandboxModeDisablesSandbox(mode string) bool {
	return mode == SandboxModeDangerFullAccess
}

// EffectiveSandboxMode returns mode, or SandboxModeReadOnly when empty (legacy records).
func EffectiveSandboxMode(mode string) string {
	if mode == "" {
		return SandboxModeReadOnly
	}
	return mode
}
