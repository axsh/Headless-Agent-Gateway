package agentservice_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/axsh/arctic-tern/shared/libs/go/codingagent"
)

func TestCreateSession_FileChangeCollectors_Default(t *testing.T) {
	_, handler := newTestServer()
	body := `{"agent":"claudecode","work_dir":"/tmp/w"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var created map[string]string
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+created["session_id"], nil)
	gw := httptest.NewRecorder()
	handler.ServeHTTP(gw, getReq)
	if gw.Code != http.StatusOK {
		t.Fatalf("get status=%d", gw.Code)
	}
	var info codingagent.SessionRecord
	if err := json.NewDecoder(gw.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.FileChangeCollectors == nil {
		t.Fatal("expected file_change_collectors")
	}
	want := codingagent.DefaultFileChangeCollectors()
	if *info.FileChangeCollectors != want {
		t.Fatalf("got %+v want %+v", *info.FileChangeCollectors, want)
	}
}

func TestCreateSession_FileChangeCollectors_PartialAndUnknown(t *testing.T) {
	_, handler := newTestServer()

	body := `{"agent":"claudecode","work_dir":"/tmp/w","file_change_collectors":{"workdir_reconcile":true}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var created map[string]string
	_ = json.NewDecoder(w.Body).Decode(&created)

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+created["session_id"], nil)
	gw := httptest.NewRecorder()
	handler.ServeHTTP(gw, getReq)
	var info codingagent.SessionRecord
	_ = json.NewDecoder(gw.Body).Decode(&info)
	if info.FileChangeCollectors == nil || !info.FileChangeCollectors.WorkdirReconcile {
		t.Fatalf("got %+v", info.FileChangeCollectors)
	}
	if !info.FileChangeCollectors.StructuredTool || !info.FileChangeCollectors.ShellParser {
		t.Fatalf("partial defaults missing: %+v", info.FileChangeCollectors)
	}

	bad := `{"agent":"claudecode","work_dir":"/tmp/w","file_change_collectors":{"nope":true}}`
	breq := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader([]byte(bad)))
	breq.Header.Set("Content-Type", "application/json")
	bw := httptest.NewRecorder()
	handler.ServeHTTP(bw, breq)
	if bw.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", bw.Code)
	}
}
