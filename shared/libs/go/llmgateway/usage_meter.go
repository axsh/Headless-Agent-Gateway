package llmgateway

import (
	"strings"
	"sync"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

// UsageMeter records best-effort per-call token usage keyed by Tern session + turn.
type UsageMeter struct {
	mu   sync.Mutex
	byKey map[string][]codingagent.TokenUsage
}

func NewUsageMeter() *UsageMeter {
	return &UsageMeter{byKey: make(map[string][]codingagent.TokenUsage)}
}

func meterKey(ternSession, turn string) string {
	return ternSession + "|" + turn
}

// Record appends a call usage for the given Tern session and turn.
func (m *UsageMeter) Record(ternSession, turn string, u codingagent.TokenUsage) {
	if m == nil || ternSession == "" || turn == "" {
		return
	}
	if u.Source == "" {
		u.Source = codingagent.UsageSourceLLMGateway
	}
	if u.Confidence == "" {
		u.Confidence = codingagent.UsageConfidenceLow
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k := meterKey(ternSession, turn)
	m.byKey[k] = append(m.byKey[k], u)
}

// Take returns and clears recorded call usages for the turn.
func (m *UsageMeter) Take(ternSession, turn string) []codingagent.TokenUsage {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k := meterKey(ternSession, turn)
	out := m.byKey[k]
	delete(m.byKey, k)
	return out
}

// ExtractMetaValue extracts ";key=value" from an auth header metadata string.
func ExtractMetaValue(authHeader, key string) string {
	prefix := key + "="
	for _, part := range strings.Split(authHeader, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "Bearer ") {
			part = strings.TrimSpace(strings.TrimPrefix(part, "Bearer "))
		}
		if strings.HasPrefix(part, prefix) {
			return strings.TrimPrefix(part, prefix)
		}
	}
	return ""
}

// ExtractTernSessionID extracts tern_sid= from gateway auth metadata.
func ExtractTernSessionID(authHeader string) string {
	return ExtractMetaValue(authHeader, "tern_sid")
}

// ExtractTurnID extracts tid= from gateway auth metadata.
func ExtractTurnID(authHeader string) string {
	return ExtractMetaValue(authHeader, "tid")
}
