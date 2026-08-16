package agentservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/portable"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

const summarizationSystemPrompt = `You are a conversation summarizer. Summarize the following conversation concisely.
Rules:
- Preserve the meaning and intent of user requests and assistant responses.
- MUST preserve all tool call names and their outcomes (success/failure/key results).
- MUST preserve specific file paths, command outputs, and operation results.
- Keep causal relationships between user requests and assistant actions.
- Output in the same language as the conversation.
- Be concise but do not lose important facts.`

const mergeSystemPrompt = `You are a conversation summarizer.
Merge the following two conversation summaries into a single, cohesive summary.
Rules:
- Preserve all tool call names and their outcomes from both summaries.
- Maintain chronological order (Summary A happened before Summary B).
- Preserve specific file paths, command outputs, and operation results.
- Keep causal relationships between user requests and assistant actions.
- Be concise but do not lose important facts.
- Output in the same language as the summaries.`

// GatewaySummarizer implements portable.Summarizer via LLM Gateway chat completions.
type GatewaySummarizer struct {
	GatewayURL   string
	Token        string
	DefaultModel string
	HTTPClient   *http.Client
}

var _ portable.Summarizer = (*GatewaySummarizer)(nil)

// Summarize maps a message chunk to a concise summary.
func (g *GatewaySummarizer) Summarize(ctx context.Context, model string, msgs []session.Message) (string, error) {
	user := "Summarize this conversation:\n\n" + conversationLog(msgs)
	return g.chat(ctx, model, summarizationSystemPrompt, user)
}

// Merge combines two summaries.
func (g *GatewaySummarizer) Merge(ctx context.Context, model string, a, b string) (string, error) {
	user := "Summary A:\n" + a + "\n\nSummary B:\n" + b
	return g.chat(ctx, model, mergeSystemPrompt, user)
}

func (g *GatewaySummarizer) chat(ctx context.Context, model, system, user string) (string, error) {
	if g == nil || g.GatewayURL == "" {
		return "", fmt.Errorf("llm gateway is not configured")
	}
	if model == "" {
		model = g.DefaultModel
	}
	if model == "" {
		return "", fmt.Errorf("summarizer model is empty")
	}
	client := g.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(g.GatewayURL, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if g.Token != "" {
		req.Header.Set("X-Gateway-Token", g.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("gateway summarizer HTTP %d: %s", resp.StatusCode, truncateForLog(string(respBody), 500))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode summarizer response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("summarizer returned no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

func conversationLog(msgs []session.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "user":
			fmt.Fprintf(&b, "USER: %s\n", m.Content)
		case "assistant":
			fmt.Fprintf(&b, "ASSISTANT: %s\n", m.Content)
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "  [TOOL CALL: %s]\n", tc.Name)
			}
		case "tool":
			fmt.Fprintf(&b, "  [TOOL RESULT: %s]\n", m.Content)
		default:
			fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
		}
	}
	return b.String()
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
