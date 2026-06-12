package llmgateway

// MaskSecret masks a secret string, showing only the last 4 characters.
// If the string is 4 characters or shorter, it returns "****".
func MaskSecret(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}
