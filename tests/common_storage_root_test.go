package llm_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestStorageRoot_CreateListGet(t *testing.T) {
	ts, _, _, _ := newPortabilityHTTP(t)
	workDir := t.TempDir()
	storageRoot := t.TempDir()

	createBody, _ := json.Marshal(map[string]string{
		"agent":        "codex",
		"model":        "gpt-4o",
		"work_dir":     workDir,
		"storage_root": storageRoot,
	})
	resp, err := http.Post(ts.URL+"/api/v1/sessions", "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var created map[string]string
	json.NewDecoder(resp.Body).Decode(&created)
	id := created["session_id"]
	if id == "" {
		t.Fatal("missing session_id")
	}

	wantSessionDir := filepath.Join(storageRoot, ".tern", id)
	if _, err := os.Stat(filepath.Join(wantSessionDir, "record.json")); err != nil {
		t.Fatalf("record.json: %v", err)
	}

	getResp, err := http.Get(ts.URL + "/api/v1/sessions/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	var got map[string]any
	json.NewDecoder(getResp.Body).Decode(&got)
	if got["storage_root"] != storageRoot {
		t.Fatalf("storage_root = %v, want %s", got["storage_root"], storageRoot)
	}
	if got["session_dir"] != wantSessionDir {
		t.Fatalf("session_dir = %v, want %s", got["session_dir"], wantSessionDir)
	}

	listURL := ts.URL + "/api/v1/sessions?work_dir=" + url.QueryEscape(workDir) + "&storage_root=" + url.QueryEscape(storageRoot)
	listResp, err := http.Get(listURL)
	if err != nil {
		t.Fatal(err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", listResp.StatusCode)
	}
	var recs []map[string]any
	json.NewDecoder(listResp.Body).Decode(&recs)
	if len(recs) != 1 || recs[0]["id"] != id {
		t.Fatalf("list recs = %+v", recs)
	}
}
