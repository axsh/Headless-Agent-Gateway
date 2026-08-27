package codingagent

import (
	"encoding/json"
	"testing"
)

func TestResolveFileChangeCollectors_Defaults(t *testing.T) {
	got, err := ResolveFileChangeCollectors(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultFileChangeCollectors()
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}

	got, err = ResolveFileChangeCollectors(json.RawMessage("null"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("null: got %+v want %+v", got, want)
	}
}

func TestResolveFileChangeCollectors_PartialOverride(t *testing.T) {
	got, err := ResolveFileChangeCollectors(json.RawMessage(`{"workdir_reconcile":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !got.StructuredTool || !got.ShellParser || !got.WorkdirReconcile {
		t.Fatalf("got %+v", got)
	}

	got, err = ResolveFileChangeCollectors(json.RawMessage(`{"structured_tool":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.StructuredTool || !got.ShellParser || got.WorkdirReconcile {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveFileChangeCollectors_AllOff(t *testing.T) {
	raw := json.RawMessage(`{
		"structured_tool":false,
		"shell_parser":false,
		"workdir_reconcile":false
	}`)
	got, err := ResolveFileChangeCollectors(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.StructuredTool || got.ShellParser || got.WorkdirReconcile {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveFileChangeCollectors_UnknownKey(t *testing.T) {
	_, err := ResolveFileChangeCollectors(json.RawMessage(`{"foo":true}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveFileChangeCollectors_NonBool(t *testing.T) {
	_, err := ResolveFileChangeCollectors(json.RawMessage(`{"structured_tool":"yes"}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEffectiveFileChangeCollectors_Nil(t *testing.T) {
	got := EffectiveFileChangeCollectors(nil)
	if got != DefaultFileChangeCollectors() {
		t.Fatalf("got %+v", got)
	}
}

func TestEffectiveFileChangeCollectors_AllOffPreserved(t *testing.T) {
	off := FileChangeCollectors{}
	got := EffectiveFileChangeCollectors(&off)
	if got.StructuredTool || got.ShellParser || got.WorkdirReconcile {
		t.Fatalf("got %+v", got)
	}
}
