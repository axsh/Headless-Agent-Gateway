package llm_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/agentservice"
	"github.com/axsh/arctic-tern/shared/libs/go/llmgateway"
)

func TestTernSessionDir_NoNativeVendorHome(t *testing.T) {
	ts, _, codex, _ := newPortabilityHTTP(t)
	workDir := t.TempDir()
	id := portCreate(t, ts, "codex", workDir, "", "gpt-4o")
	portSend(t, ts, id, "hi")

	sessionDir := filepath.Join(workDir, ".tern", id)
	if _, err := os.Stat(filepath.Join(sessionDir, "record.json")); err != nil {
		t.Fatalf("record.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "history")); err != nil {
		t.Fatalf("history: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "native")); !os.IsNotExist(err) {
		t.Fatalf("native/ must not exist, err=%v", err)
	}
	cfg := codex.last()
	if cfg == nil {
		t.Fatal("missing codex config")
	}
	want := filepath.Join(workDir, ".codex")
	if cfg.SessionDir != want {
		t.Fatalf("SessionDir = %q, want %q", cfg.SessionDir, want)
	}
}

func TestCodexUsesWorkDirCodexHome(t *testing.T) {
	ts, _, codex, _ := newPortabilityHTTP(t)
	workDir := t.TempDir()
	id := portCreate(t, ts, "codex", workDir, "", "gpt-4o")
	portSend(t, ts, id, "ping")
	cfg := codex.last()
	want := filepath.Join(workDir, ".codex")
	if cfg.SessionDir != want {
		t.Fatalf("SessionDir = %q, want %q", cfg.SessionDir, want)
	}
}

func TestClaudeUsesWorkDirClaudeHome(t *testing.T) {
	ts, claude, _, _ := newPortabilityHTTP(t)
	workDir := t.TempDir()
	id := portCreate(t, ts, "claudecode", workDir, "", "claude-sonnet-4-6")
	portSend(t, ts, id, "ping")
	cfg := claude.last()
	want := filepath.Join(workDir, ".claude")
	if cfg.SessionDir != want {
		t.Fatalf("SessionDir = %q, want %q", cfg.SessionDir, want)
	}
	if strings.Contains(cfg.SessionDir, ".claudecode") {
		t.Fatalf("must not use .claudecode, got %q", cfg.SessionDir)
	}
}

func TestWayfinderUsesTernSessionDir(t *testing.T) {
	wf := &portabilityAgent{name: "wayfinder", nativeID: "wf-native"}
	sum := &portabilitySummarizer{}
	srv := agentservice.New(agentservice.WithSummarizer(sum))
	srv.RegisterAgent(wf)
	srv.SetGatewayModels(
		[]llmgateway.ModelInfo{{Model: "gpt-4o"}},
		&llmgateway.ModelInfo{Model: "gpt-4o"},
	)
	ts := httptest.NewServer(srv.HTTPHandler())
	t.Cleanup(ts.Close)

	workDir := t.TempDir()
	id := portCreate(t, ts, "wayfinder", workDir, "", "gpt-4o")
	portSend(t, ts, id, "ping")
	cfg := wf.last()
	want := filepath.Join(workDir, ".tern", id)
	if cfg.SessionDir != want {
		t.Fatalf("SessionDir = %q, want %q", cfg.SessionDir, want)
	}
	if strings.HasSuffix(cfg.SessionDir, string(filepath.Separator)+"native") ||
		strings.Contains(cfg.SessionDir, ".wayfinder") {
		t.Fatalf("wayfinder must use tern session root, got %q", cfg.SessionDir)
	}
}

func TestRepeatedCodexSessions_TernTreeHasNoNativePluginsTree(t *testing.T) {
	ts, _, codex, _ := newPortabilityHTTP(t)
	workDir := t.TempDir()
	wantHome := filepath.Join(workDir, ".codex")
	for i := 0; i < 5; i++ {
		id := portCreate(t, ts, "codex", workDir, "", "gpt-4o")
		portSend(t, ts, id, "turn")
		resp, err := http.Post(ts.URL+"/api/v1/sessions/"+id+"/terminate", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		sessionDir := filepath.Join(workDir, ".tern", id)
		plugins := filepath.Join(sessionDir, "native", ".tmp", "plugins")
		if _, err := os.Stat(plugins); !os.IsNotExist(err) {
			t.Fatalf("session %d: plugins tree must not exist under tern, err=%v", i, err)
		}
		if _, err := os.Stat(filepath.Join(sessionDir, "native")); !os.IsNotExist(err) {
			t.Fatalf("session %d: native/ must not exist, err=%v", i, err)
		}
		cfg := codex.last()
		if cfg.SessionDir != wantHome {
			t.Fatalf("session %d: SessionDir = %q, want %q", i, cfg.SessionDir, wantHome)
		}
	}
}

func TestCodexResumeUsesSameVendorHome(t *testing.T) {
	ts, _, codex, _ := newPortabilityHTTP(t)
	workDir := t.TempDir()
	id := portCreate(t, ts, "codex", workDir, "", "gpt-4o")
	portSend(t, ts, id, "first")
	first := codex.last().SessionDir
	portSend(t, ts, id, "second")
	second := codex.last()
	want := filepath.Join(workDir, ".codex")
	if first != want || second.SessionDir != want {
		t.Fatalf("vendor homes = %q / %q, want %q", first, second.SessionDir, want)
	}
	if second.AgentSessionID != "codex-native" {
		t.Fatalf("resume id = %q, want codex-native", second.AgentSessionID)
	}
}

func TestStorageRoot_SharedParentForHomes(t *testing.T) {
	ts, _, codex, _ := newPortabilityHTTP(t)
	workDir := t.TempDir()
	storageRoot := t.TempDir()
	id := portCreate(t, ts, "codex", workDir, "", "gpt-4o", storageRoot)
	portSend(t, ts, id, "hi")

	sessionDir := filepath.Join(storageRoot, ".tern", id)
	if _, err := os.Stat(filepath.Join(sessionDir, "record.json")); err != nil {
		t.Fatalf("record.json under storage_root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".tern", id, "record.json")); !os.IsNotExist(err) {
		t.Fatalf("record must not be under work_dir/.tern when storage_root differs")
	}
	cfg := codex.last()
	want := filepath.Join(storageRoot, ".codex")
	if cfg.SessionDir != want {
		t.Fatalf("SessionDir = %q, want %q", cfg.SessionDir, want)
	}
}

func TestStorageRoot_ExplicitSessionDir_VendorFromStorageRoot(t *testing.T) {
	ts, _, codex, _ := newPortabilityHTTP(t)
	workDir := t.TempDir()
	storageRoot := t.TempDir()
	leaf := t.TempDir()
	id := portCreate(t, ts, "codex", workDir, leaf, "gpt-4o", storageRoot)
	portSend(t, ts, id, "ping")
	if _, err := os.Stat(filepath.Join(leaf, "record.json")); err != nil {
		t.Fatalf("record.json under explicit leaf: %v", err)
	}
	cfg := codex.last()
	want := filepath.Join(storageRoot, ".codex")
	if cfg.SessionDir != want {
		t.Fatalf("SessionDir = %q, want %q", cfg.SessionDir, want)
	}
	_ = id
}
