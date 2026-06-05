package codingagent_test

import (
	"context"
	"testing"

	"github.com/axsh/hag/codingagent"
)

// mockAgent implements CodingAgent for compile-time check.
type mockAgent struct{}

func (m *mockAgent) CreateSession(_ context.Context, _ ...codingagent.SessionOption) (codingagent.Session, error) {
	return nil, nil
}
func (m *mockAgent) Name() string  { return "mock" }
func (m *mockAgent) Close() error  { return nil }

// mockSession implements Session for compile-time check.
type mockSession struct{}

func (m *mockSession) Send(_ context.Context, _ string) (<-chan codingagent.StreamEvent, error) {
	return nil, nil
}
func (m *mockSession) ID() string  { return "mock-id" }
func (m *mockSession) Close() error { return nil }

// Compile-time interface compliance checks.
var _ codingagent.CodingAgent = (*mockAgent)(nil)
var _ codingagent.Session = (*mockSession)(nil)

func TestCodingAgentInterfaceDefinition(t *testing.T) {
	var agent codingagent.CodingAgent = &mockAgent{}

	if agent.Name() != "mock" {
		t.Errorf("Name() = %v, want mock", agent.Name())
	}

	if err := agent.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

func TestSessionInterfaceDefinition(t *testing.T) {
	var session codingagent.Session = &mockSession{}

	if session.ID() != "mock-id" {
		t.Errorf("ID() = %v, want mock-id", session.ID())
	}

	if err := session.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}
