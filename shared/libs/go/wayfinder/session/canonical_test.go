package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCanonical_InitWritesMetadata(t *testing.T) {
	dir := t.TempDir()
	c := OpenCanonical(dir)
	if err := c.Init("sess-1", OriginClaudeCode); err != nil {
		t.Fatalf("Init: %v", err)
	}
	meta, err := c.LoadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if meta.SessionID != "sess-1" {
		t.Errorf("SessionID = %q", meta.SessionID)
	}
	if meta.ActiveAgent != OriginClaudeCode {
		t.Errorf("ActiveAgent = %q", meta.ActiveAgent)
	}
	if meta.AgentBindings == nil {
		t.Fatal("AgentBindings should be non-nil")
	}
	if _, err := os.Stat(filepath.Join(dir, "history")); err != nil {
		t.Errorf("history dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "native")); !os.IsNotExist(err) {
		t.Errorf("native dir must not be created, err=%v", err)
	}
}

func TestCanonical_AppendAssignsSeqAndOrigin(t *testing.T) {
	dir := t.TempDir()
	c := OpenCanonical(dir)
	if err := c.Init("sess-1", OriginClaudeCode); err != nil {
		t.Fatal(err)
	}
	if err := c.Append([]Message{{
		Role:    "user",
		Content: "hi",
		Origin:  OriginClaudeCode,
	}}); err != nil {
		t.Fatal(err)
	}
	msgs, err := c.LoadRange(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len = %d", len(msgs))
	}
	if msgs[0].Seq != 1 {
		t.Errorf("Seq = %d, want 1", msgs[0].Seq)
	}
	if msgs[0].Origin != OriginClaudeCode {
		t.Errorf("Origin = %q", msgs[0].Origin)
	}
}

func TestCanonical_NextSeqAfterExistingFiles(t *testing.T) {
	dir := t.TempDir()
	c := OpenCanonical(dir)
	if err := c.Init("sess-1", OriginCodex); err != nil {
		t.Fatal(err)
	}
	if err := c.Append([]Message{
		{Role: "user", Content: "a", Origin: OriginCodex},
		{Role: "assistant", Content: "b", Origin: OriginCodex},
	}); err != nil {
		t.Fatal(err)
	}
	next, err := c.NextSeq()
	if err != nil {
		t.Fatal(err)
	}
	if next != 3 {
		t.Errorf("NextSeq = %d, want 3", next)
	}
}

func TestCanonical_UpdateBindingWatermark(t *testing.T) {
	dir := t.TempDir()
	c := OpenCanonical(dir)
	if err := c.Init("sess-1", OriginClaudeCode); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateBinding(OriginClaudeCode, "native-1", 2); err != nil {
		t.Fatal(err)
	}
	meta, err := c.LoadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	b := meta.AgentBindings[OriginClaudeCode]
	if b.AgentSessionID != "native-1" || b.IngestedThroughSeq != 2 {
		t.Errorf("binding = %+v", b)
	}
	if err := c.UpdateBinding(OriginClaudeCode, "", 5); err != nil {
		t.Fatal(err)
	}
	meta, _ = c.LoadMetadata()
	b = meta.AgentBindings[OriginClaudeCode]
	if b.AgentSessionID != "native-1" {
		t.Errorf("native id should be kept, got %q", b.AgentSessionID)
	}
	if b.IngestedThroughSeq != 5 {
		t.Errorf("watermark = %d, want 5", b.IngestedThroughSeq)
	}
}

func TestCanonical_LoadHistoryFromWatermark(t *testing.T) {
	dir := t.TempDir()
	c := OpenCanonical(dir)
	if err := c.Init("sess-1", OriginClaudeCode); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := c.Append([]Message{
		{Role: "user", Content: "1", Seq: 1, Origin: OriginClaudeCode, Timestamp: now},
		{Role: "user", Content: "2", Seq: 2, Origin: OriginClaudeCode, Timestamp: now},
		{Role: "user", Content: "3", Seq: 3, Origin: OriginCodex, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateBinding(OriginClaudeCode, "n", 2); err != nil {
		t.Fatal(err)
	}
	msgs, err := c.LoadHistoryFrom(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Seq != 3 {
		t.Fatalf("got %+v, want seq 3 only", msgs)
	}
}

func TestCanonical_InitKeepsBindings(t *testing.T) {
	dir := t.TempDir()
	c := OpenCanonical(dir)
	if err := c.Init("sess-1", OriginClaudeCode); err != nil {
		t.Fatal(err)
	}
	if err := c.UpdateBinding(OriginClaudeCode, "keep-me", 1); err != nil {
		t.Fatal(err)
	}
	if err := c.Init("sess-1", OriginCodex); err != nil {
		t.Fatal(err)
	}
	meta, err := c.LoadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if meta.ActiveAgent != OriginCodex {
		t.Errorf("ActiveAgent = %q", meta.ActiveAgent)
	}
	if meta.AgentBindings[OriginClaudeCode].AgentSessionID != "keep-me" {
		t.Errorf("binding lost: %+v", meta.AgentBindings)
	}
	raw, _ := json.Marshal(meta)
	if string(raw) == "" {
		t.Fatal("empty metadata")
	}
}
