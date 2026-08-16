package agentservice

import (
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/session"
)

// EventsToMessages converts a turn's StreamEvents into canonical history messages.
// User prompts are not ingested here; they are appended at SendMessage start.
func EventsToMessages(origin string, events []codingagent.StreamEvent) []session.Message {
	origin = session.NormalizeOrigin(origin)
	now := time.Now()
	var msgs []session.Message
	var text strings.Builder

	flushText := func() {
		if text.Len() == 0 {
			return
		}
		msgs = append(msgs, session.Message{
			Role:      "assistant",
			Content:   text.String(),
			Timestamp: now,
			Origin:    origin,
		})
		text.Reset()
	}

	for _, ev := range events {
		switch ev.Type {
		case codingagent.EventText:
			text.WriteString(ev.Content)
		case codingagent.EventToolUse:
			flushText()
			msgs = append(msgs, session.Message{
				Role:      "assistant",
				Timestamp: now,
				Origin:    origin,
				ToolCalls: []session.ToolCallRecord{{
					Name:  ev.ToolName,
					Input: ev.ToolInput,
				}},
			})
		case codingagent.EventToolResult:
			flushText()
			msgs = append(msgs, session.Message{
				Role:      "tool",
				Content:   ev.Content,
				Timestamp: now,
				Origin:    origin,
			})
		case codingagent.EventResult:
			flushText()
		case codingagent.EventSystem, codingagent.EventError, codingagent.EventUserInputRequired,
			codingagent.EventToolResultPart, codingagent.EventNodeStart, codingagent.EventNodeComplete,
			codingagent.EventNodeFailed, codingagent.EventProgress:
			// Not conversation facts.
		default:
			flushText()
		}
	}
	flushText()
	return msgs
}

// IngestTurn appends assistant/tool facts from a completed turn and updates the binding watermark.
func IngestTurn(sessionDir, origin, nativeSessionID string, events []codingagent.StreamEvent) error {
	if sessionDir == "" {
		return nil
	}
	c := session.OpenCanonical(sessionDir)
	if err := c.Init("", origin); err != nil {
		return err
	}
	for _, ev := range events {
		if ev.Type == codingagent.EventSystem && ev.SessionID != "" {
			nativeSessionID = ev.SessionID
		}
	}
	msgs := EventsToMessages(origin, events)
	if err := c.Append(msgs); err != nil {
		return err
	}
	meta, err := c.LoadMetadata()
	if err != nil {
		return err
	}
	return c.UpdateBinding(origin, nativeSessionID, meta.TotalSeq)
}
