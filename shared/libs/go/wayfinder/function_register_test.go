package wayfinder

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
	"github.com/axsh/arctic-tern/shared/libs/go/toolconfig"
	"github.com/axsh/arctic-tern/shared/libs/go/wayfinder/tools"
)

func TestRegisterClientFunctionsAndSubmit(t *testing.T) {
	core := NewAgentCore(&stubLLM{}, &AgentConfig{
		WorkDir:      t.TempDir(),
		SessionDir:   t.TempDir(),
		LogicalModel: "test",
	}, nil)
	if err := RegisterClientFunctions(core.Registry(), map[string]toolconfig.FunctionConfig{
		"lookup_ticket": {
			Description: "Look up",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}); err != nil {
		t.Fatal(err)
	}

	tool, ok := core.Registry().Get("lookup_ticket")
	if !ok {
		t.Fatal("tool not registered")
	}
	_, err := tool.Handler(context.Background(), map[string]any{"ticket_id": "T-1"})
	var fcErr *tools.FunctionCallError
	if !errors.As(err, &fcErr) {
		t.Fatalf("expected FunctionCallError, got %v", err)
	}

	ch := make(chan codingagent.StreamEvent, 4)
	core.SetEmitter(NewEventEmitter(ch))

	done := make(chan string, 1)
	go func() {
		out, waitErr := core.waitForClientFunction(context.Background(), fcErr.Req)
		if waitErr != nil {
			done <- "err:" + waitErr.Error()
			return
		}
		done <- out
	}()

	select {
	case ev := <-ch:
		if ev.Type != codingagent.EventFunctionCall {
			t.Fatalf("event type = %s", ev.Type)
		}
		if err := core.SubmitToolResult(ev.CallID, `{"ok":true}`, false); err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for function_call event")
	}

	select {
	case got := <-done:
		if got != `{"ok":true}` {
			t.Fatalf("got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

type stubLLM struct{}

func (s *stubLLM) GenerateMessage(context.Context, string, []ChatMessage, []ToolDefinition, ...GenerateOptions) (*LLMResponse, error) {
	return &LLMResponse{Content: "done"}, nil
}
