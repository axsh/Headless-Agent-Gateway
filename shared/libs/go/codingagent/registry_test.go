package codingagent

import (
	"context"
	"fmt"
	"testing"
)

// mockAgent implements CodingAgent for testing.
type mockAgent struct {
	name string
}

func (m *mockAgent) CreateSession(ctx context.Context, opts ...SessionOption) (Session, error) {
	return nil, nil
}
func (m *mockAgent) Name() string { return m.name }
func (m *mockAgent) Close() error { return nil }

func TestRegister_And_CreateAll(t *testing.T) {
	tests := []struct {
		name      string
		factories map[string]FactoryFunc
		wantCount int
		wantNames []string
	}{
		{
			name:      "no factories registered",
			factories: map[string]FactoryFunc{},
			wantCount: 0,
		},
		{
			name: "one factory returns agent",
			factories: map[string]FactoryFunc{
				"test-agent": func(cfg *AdapterConfig) (CodingAgent, error) {
					return &mockAgent{name: "test-agent"}, nil
				},
			},
			wantCount: 1,
			wantNames: []string{"test-agent"},
		},
		{
			name: "factory returns nil - CLI not found",
			factories: map[string]FactoryFunc{
				"missing": func(cfg *AdapterConfig) (CodingAgent, error) {
					return nil, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "factory returns error",
			factories: map[string]FactoryFunc{
				"broken": func(cfg *AdapterConfig) (CodingAgent, error) {
					return nil, fmt.Errorf("init failed")
				},
			},
			wantCount: 0,
		},
		{
			name: "multiple factories with mixed results",
			factories: map[string]FactoryFunc{
				"good": func(cfg *AdapterConfig) (CodingAgent, error) {
					return &mockAgent{name: "good"}, nil
				},
				"missing": func(cfg *AdapterConfig) (CodingAgent, error) {
					return nil, nil
				},
				"broken": func(cfg *AdapterConfig) (CodingAgent, error) {
					return nil, fmt.Errorf("fail")
				},
			},
			wantCount: 1,
			wantNames: []string{"good"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetRegistry()
			for name, factory := range tt.factories {
				Register(name, factory)
			}
			cfg := &AdapterConfig{}
			agents := CreateAll(cfg)
			if len(agents) != tt.wantCount {
				t.Fatalf("CreateAll() returned %d agents, want %d", len(agents), tt.wantCount)
			}
			for _, wantName := range tt.wantNames {
				found := false
				for _, a := range agents {
					if a.Name() == wantName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected agent %q not found in results", wantName)
				}
			}
		})
	}
}

func TestRegister_DuplicateName_Panics(t *testing.T) {
	resetRegistry()
	Register("dup", func(cfg *AdapterConfig) (CodingAgent, error) {
		return nil, nil
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for duplicate registration, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if msg != "codingagent: Register called twice for dup" {
			t.Errorf("unexpected panic message: %s", msg)
		}
	}()

	Register("dup", func(cfg *AdapterConfig) (CodingAgent, error) {
		return nil, nil
	})
}
