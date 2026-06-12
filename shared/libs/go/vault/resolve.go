package vault

import "strings"

const vaultPrefix = "vault://"

// IsVaultRef returns true if the value is a vault:// reference.
func IsVaultRef(value string) bool {
	return strings.HasPrefix(value, vaultPrefix)
}

// ParseVaultRef extracts the path from a vault:// reference.
// Returns the path portion (e.g. "providers/anthropic/primary") and true,
// or empty string and false if not a vault reference.
func ParseVaultRef(value string) (string, bool) {
	if !IsVaultRef(value) {
		return "", false
	}
	return strings.TrimPrefix(value, vaultPrefix), true
}
