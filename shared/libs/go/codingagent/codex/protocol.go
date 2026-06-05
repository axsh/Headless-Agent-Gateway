package codex

import (
	"encoding/json"

	"github.com/axsh/hag/codingagent"
)

// JSONRPCMessage is a generic JSON-RPC 2.0 message structure.
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// BuildInitializeRequest constructs a JSON-RPC 2.0 initialize request.
func BuildInitializeRequest() ([]byte, error) {
	id := 1
	msg := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "initialize",
	}
	return json.Marshal(msg)
}

// BuildStartThreadRequest constructs a JSON-RPC 2.0 startThread request.
func BuildStartThreadRequest(prompt string) ([]byte, error) {
	id := 2
	params, _ := json.Marshal(map[string]string{"prompt": prompt})
	msg := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "startThread",
		Params:  params,
	}
	return json.Marshal(msg)
}

// ParseNotification converts a JSON-RPC 2.0 notification to a StreamEvent.
func ParseNotification(line string) *codingagent.StreamEvent {
	var msg JSONRPCMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return nil
	}

	switch msg.Method {
	case "text":
		var p struct {
			Content string `json:"content"`
		}
		json.Unmarshal(msg.Params, &p)
		return &codingagent.StreamEvent{Type: codingagent.EventText, Content: p.Content}

	case "tool_use":
		var p struct {
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}
		json.Unmarshal(msg.Params, &p)
		return &codingagent.StreamEvent{
			Type:      codingagent.EventToolUse,
			ToolName:  p.Name,
			ToolInput: p.Input,
		}

	case "result":
		return &codingagent.StreamEvent{Type: codingagent.EventResult}

	default:
		return nil
	}
}

// IsApprovalRequest checks if the message is an approval request.
func IsApprovalRequest(msg *JSONRPCMessage) bool {
	return msg.Method == "approval_request" && msg.ID != nil
}

// BuildApprovalResponse constructs an auto-approval response.
func BuildApprovalResponse(id int) ([]byte, error) {
	result, _ := json.Marshal(map[string]bool{"approved": true})
	msg := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Result:  result,
	}
	return json.Marshal(msg)
}
