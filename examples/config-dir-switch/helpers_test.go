package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareConfigDirs_WritesMarkers(t *testing.T) {
	base := t.TempDir()
	alpha, beta, err := prepareConfigPair(base, "claudecode", "", "")
	if err != nil {
		t.Fatalf("prepareConfigPair: %v", err)
	}
	alphaBody, err := os.ReadFile(filepath.Join(alpha, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read alpha: %v", err)
	}
	if !strings.Contains(string(alphaBody), markerAlpha) {
		t.Errorf("alpha marker missing: %q", alphaBody)
	}
	betaBody, err := os.ReadFile(filepath.Join(beta, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read beta: %v", err)
	}
	if !strings.Contains(string(betaBody), markerBeta) {
		t.Errorf("beta marker missing: %q", betaBody)
	}
}

func TestPrepareConfigDirs_CodexUsesAgentsMD(t *testing.T) {
	base := t.TempDir()
	alpha, _, err := prepareConfigPair(base, "codex", "", "")
	if err != nil {
		t.Fatalf("prepareConfigPair: %v", err)
	}
	if _, err := os.Stat(filepath.Join(alpha, "AGENTS.md")); err != nil {
		t.Fatalf("expected AGENTS.md: %v", err)
	}
}

func TestDefaultFlags(t *testing.T) {
	f, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if f.Agent != "claudecode" {
		t.Errorf("Agent = %q, want claudecode", f.Agent)
	}
	if f.Server != "http://localhost:3100" {
		t.Errorf("Server = %q", f.Server)
	}
}

func TestParseFlags_AgentCodex(t *testing.T) {
	f, err := parseFlags([]string{"--agent", "codex"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if f.Agent != "codex" {
		t.Errorf("Agent = %q, want codex", f.Agent)
	}
}

func TestMainFlags_Help(t *testing.T) {
	_, err := parseFlags([]string{"-h"})
	if err == nil {
		t.Fatal("expected help error from flag.ContinueOnError")
	}
}
