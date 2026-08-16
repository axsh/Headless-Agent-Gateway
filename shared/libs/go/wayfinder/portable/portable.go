package portable

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

const (
	TransferHeader = "Tern session context transfer"
	TransferNotice = "The following is reconstructed from a shared session log. It is supplementary history, not your own previous turn. Origins are labeled."
	TransferFooter = "End of transferred context"

	AlgorithmMapReduce  = "map_reduce"
	AlgorithmFull       = "full"
	AlgorithmStructured = "structured"

	defaultMaxChunkMessages = 20
	defaultThresholdBytes   = 32768
	defaultRecentKeep       = 8
	toolContentMaxRunes     = 5000
)

// ErrMapReduceRequiresLLM is returned when map_reduce must compact and no Summarizer is set.
var ErrMapReduceRequiresLLM = errors.New("map_reduce supplement requires an LLM summarizer")

// Strategy is the per-session / per-turn context-transfer policy.
type Strategy = session.SupplementStrategy

// Summarizer is the LLM MapReduce backend used by BuildSupplement.
type Summarizer interface {
	Summarize(ctx context.Context, model string, msgs []session.Message) (string, error)
	Merge(ctx context.Context, model string, a, b string) (string, error)
}

// WithDefaults fills zero-valued strategy fields.
func WithDefaults(s Strategy) Strategy {
	if s.Algorithm == "" {
		s.Algorithm = AlgorithmMapReduce
	}
	if s.MaxChunkMessages <= 0 {
		s.MaxChunkMessages = defaultMaxChunkMessages
	}
	if s.ThresholdBytes <= 0 {
		s.ThresholdBytes = defaultThresholdBytes
	}
	if s.RecentKeep <= 0 {
		s.RecentKeep = defaultRecentKeep
	}
	return s
}

// KnownAlgorithm reports whether algorithm is empty (defaults later) or a supported value.
func KnownAlgorithm(algorithm string) bool {
	switch algorithm {
	case "", AlgorithmMapReduce, AlgorithmFull, AlgorithmStructured:
		return true
	default:
		return false
	}
}

// MergeStrategy overlays non-empty fields: turn > session > server.
func MergeStrategy(server, sess, turn Strategy) (Strategy, error) {
	out := overlay(overlay(server, sess), turn)
	if !KnownAlgorithm(out.Algorithm) {
		return Strategy{}, fmt.Errorf("unknown supplement algorithm: %s", out.Algorithm)
	}
	return out, nil
}

func overlay(base, over Strategy) Strategy {
	if over.Algorithm != "" {
		base.Algorithm = over.Algorithm
	}
	if over.Model != "" {
		base.Model = over.Model
	}
	if over.MaxChunkMessages != 0 {
		base.MaxChunkMessages = over.MaxChunkMessages
	}
	if over.ThresholdBytes != 0 {
		base.ThresholdBytes = over.ThresholdBytes
	}
	if over.RecentKeep != 0 {
		base.RecentKeep = over.RecentKeep
	}
	return base
}

// Delta returns messages with seq > ingestedThroughSeq and origin != targetOrigin.
func Delta(msgs []session.Message, targetOrigin string, ingestedThroughSeq int) []session.Message {
	target := session.NormalizeOrigin(targetOrigin)
	var out []session.Message
	for _, m := range msgs {
		if m.Seq <= ingestedThroughSeq {
			continue
		}
		if session.NormalizeOrigin(m.Origin) == target {
			continue
		}
		out = append(out, m)
	}
	return out
}

// RenderSupplement formats messages with origin labels for prompt injection.
func RenderSupplement(targetAgent string, msgs []session.Message) string {
	target := session.NormalizeOrigin(targetAgent)
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteByte('\n')
		}
		origin := session.NormalizeOrigin(m.Origin)
		fmt.Fprintf(&b, "[origin=%s] %s:", origin, m.Role)
		if m.Content != "" {
			b.WriteByte(' ')
			b.WriteString(trimRunes(m.Content, toolContentMaxRunes))
		}
		for _, tc := range m.ToolCalls {
			b.WriteByte('\n')
			if origin == target {
				fmt.Fprintf(&b, "  tool(%s)", tc.Name)
			} else {
				fmt.Fprintf(&b, "  tool(%s:%s)", origin, tc.Name)
			}
		}
	}
	return b.String()
}

// StructuredSummary is a non-LLM fallback that keeps origin and first-line facts.
func StructuredSummary(msgs []session.Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteByte('\n')
		}
		origin := session.NormalizeOrigin(m.Origin)
		fmt.Fprintf(&b, "%s %s: %s", origin, m.Role, firstLine(m.Content))
		for _, tc := range m.ToolCalls {
			fmt.Fprintf(&b, " tool(%s)", tc.Name)
		}
	}
	return b.String()
}

// BuildSupplement renders or summarizes delta messages for the target agent.
func BuildSupplement(ctx context.Context, targetAgent string, msgs []session.Message, strat Strategy, llm Summarizer) (string, error) {
	strat = WithDefaults(strat)
	rendered := RenderSupplement(targetAgent, msgs)
	switch strat.Algorithm {
	case AlgorithmFull:
		return rendered, nil
	case AlgorithmStructured:
		return StructuredSummary(msgs), nil
	case AlgorithmMapReduce:
		if len(rendered) <= strat.ThresholdBytes {
			return rendered, nil
		}
		if llm == nil {
			return "", ErrMapReduceRequiresLLM
		}
		keepCount := strat.RecentKeep
		if keepCount > len(msgs) {
			keepCount = len(msgs)
		}
		old := msgs[:len(msgs)-keepCount]
		keep := msgs[len(msgs)-keepCount:]
		if len(old) == 0 {
			return rendered, nil
		}
		mr := session.NewMapReduceSummarizer(
			func(chunk []session.Message) (string, error) {
				return llm.Summarize(ctx, strat.Model, chunk)
			},
			func(a, b string) (string, error) {
				return llm.Merge(ctx, strat.Model, a, b)
			},
			StructuredSummary,
			strat.MaxChunkMessages,
		)
		summary, err := mr.Summarize(old)
		if err != nil {
			return "", err
		}
		recent := RenderSupplement(targetAgent, keep)
		return "[COMPACTED CONTEXT SUMMARY]\n" + summary + "\n" + recent, nil
	default:
		return "", fmt.Errorf("unknown supplement algorithm: %s", strat.Algorithm)
	}
}

// WrapPrompt places transferred context before the user prompt.
func WrapPrompt(supplement, userPrompt string) string {
	if strings.TrimSpace(supplement) == "" {
		return userPrompt
	}
	return TransferHeader + "\n\n" + TransferNotice + "\n\n" + supplement + "\n\n" + TransferFooter + "\n\n" + userPrompt
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func trimRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "\n... [TRUNCATED]"
}
