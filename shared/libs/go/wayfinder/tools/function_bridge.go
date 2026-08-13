package tools

import "errors"

// ErrFunctionCallRequired signals that a client-executed function was invoked.
var ErrFunctionCallRequired = errors.New("function call required")

// FunctionCallRequest is the payload for a pending client function call.
type FunctionCallRequest struct {
	CallID    string
	Name      string
	Arguments map[string]any
}

// FunctionCallError wraps ErrFunctionCallRequired with request details.
type FunctionCallError struct {
	Req FunctionCallRequest
}

func (e *FunctionCallError) Error() string { return ErrFunctionCallRequired.Error() }
func (e *FunctionCallError) Unwrap() error { return ErrFunctionCallRequired }
