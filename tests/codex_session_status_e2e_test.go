// Package llm_test contains E2E tests for early session status updates.
package llm_test

import (
	"context"
	"testing"
	"time"

	v1 "github.com/axsh/arctic-tern/client/v1"
)

func TestCodexE2E_SessionStatusOnTerminalEvent(t *testing.T) {
	baseURL, cleanup := startFakeCodexE2EServer(t)
	defer cleanup()

	ctx := context.Background()
	workDir := t.TempDir()
	client := v1.New(baseURL, v1.WithNoTimeout())

	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: workDir,
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stream, err := sess.SendText(ctx, "trigger")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	resultReceived := make(chan struct{})
	go func() {
		for ev := range stream.Events() {
			if ev.Type == v1.EventResult {
				close(resultReceived)
				return
			}
			if ev.Type == v1.EventError {
				t.Errorf("unexpected stream error: %s", ev.Error)
				close(resultReceived)
				return
			}
		}
	}()

	select {
	case <-resultReceived:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for EventResult")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		session, err := client.GetSession(ctx, sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		status, _ := session["status"].(string)
		if status == "completed" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	session, err := client.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status, _ := session["status"].(string)
	t.Fatalf("status after EventResult = %q, want completed within 500ms", status)
}

func TestCodexE2E_ClientV1_DisconnectAfterTerminalEventUpdatesStatus(t *testing.T) {
	baseURL, cleanup := startFakeCodexE2EServer(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := v1.New(baseURL, v1.WithNoTimeout())
	sess, err := client.CreateSession(ctx, v1.SessionRequest{
		Agent:   "codex",
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	stream, err := sess.SendText(ctx, "trigger")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	resultSeen := make(chan struct{})
	go func() {
		for ev := range stream.Events() {
			if ev.Type == v1.EventResult {
				close(resultSeen)
				cancel()
				return
			}
			if ev.Type == v1.EventError {
				t.Errorf("stream error: %s", ev.Error)
				close(resultSeen)
				return
			}
		}
	}()

	select {
	case <-resultSeen:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for EventResult")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		session, err := client.GetSession(context.Background(), sess.ID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		status, _ := session["status"].(string)
		if status == "completed" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	session, err := client.GetSession(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	status, _ := session["status"].(string)
	t.Fatalf("status after disconnect = %q, want completed within 500ms", status)
}
