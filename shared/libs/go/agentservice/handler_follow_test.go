package agentservice_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleFollow_StatusCodes(t *testing.T) {
	srv, handler := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/missing/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing session: status = %d", w.Code)
	}

	body, _ := json.Marshal(map[string]string{"agent": "claudecode", "session_dir": t.TempDir()})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(string(body)))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	var created map[string]string
	json.NewDecoder(w.Body).Decode(&created)
	id := created["session_id"]

	req = httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+id+"/events", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotAcceptable {
		t.Fatalf("no accept: status = %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+id+"/events", nil)
	req.Header.Set("Accept", "text/event-stream")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("no exec: status = %d body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no active turn") {
		t.Fatalf("body = %s", w.Body.String())
	}

	if err := srv.MarkSessionBusy(id, "active"); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+id+"/events?from=abc", nil)
	req.Header.Set("Accept", "text/event-stream")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad from: status = %d body = %s", w.Code, w.Body.String())
	}
}
