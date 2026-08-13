// Package api provides HTTP handlers for the Tern artifact API.
package api

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
)

// SystemArtifactHandler handles /api/v1/artifacts/system routes.
type SystemArtifactHandler struct {
	store store.ArtifactStore
}

// NewSystemArtifactHandler creates a handler backed by the given store.
func NewSystemArtifactHandler(s store.ArtifactStore) *SystemArtifactHandler {
	return &SystemArtifactHandler{store: s}
}

// RegisterRoutes registers the system artifact routes on mux under prefix.
// prefix should be "/api/v1/artifacts/system" (no trailing slash).
func (h *SystemArtifactHandler) RegisterRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(prefix, h.routeRoot)
	mux.HandleFunc(prefix+"/", h.routeByKey)
}

// routeRoot dispatches GET (list) and POST .../archive.
func (h *SystemArtifactHandler) routeRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleList(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// routeByKey dispatches:
//   - POST .../archive  → handleArchive
//   - GET  .../{key}/content → handleContent
//   - GET  .../{key}         → handleGetByKey
func (h *SystemArtifactHandler) routeByKey(w http.ResponseWriter, r *http.Request) {
	// Extract the sub-path after the prefix+"/".
	// e.g. "/api/v1/artifacts/system/foo/bar.go/content" → "foo/bar.go/content"
	subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/system/")
	subPath = strings.Trim(subPath, "/")

	// POST .../archive
	if r.Method == http.MethodPost && subPath == "archive" {
		h.handleArchive(w, r)
		return
	}

	// GET is the only other allowed method.
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if strings.HasSuffix(subPath, "/content") {
		key := strings.TrimSuffix(subPath, "/content")
		h.handleContent(w, r, key)
	} else {
		h.handleGetByKey(w, r, subPath)
	}
}

// handleList serves GET /api/v1/artifacts/system.
func (h *SystemArtifactHandler) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.SystemArtifactFilter{
		Q:              q.Get("q"),
		SessionIDs:     q["session_id"],
		AgentIDs:       q["agent_id"],
		Operation:      q.Get("operation"),
		IncludeDeleted: q.Get("include_deleted") == "true",
		Sort:           q.Get("sort"),
		Order:          q.Get("order"),
	}
	if p := q.Get("page"); p != "" {
		f.Page, _ = strconv.Atoi(p)
	}
	if pp := q.Get("per_page"); pp != "" {
		f.PerPage, _ = strconv.Atoi(pp)
	}
	if s := q.Get("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			f.Since = &t
		}
	}
	if u := q.Get("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			f.Until = &t
		}
	}

	page, err := h.store.ListSystemArtifacts(r.Context(), f)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"source":      "system",
		"total_count": page.TotalCount,
		"page":        page.Page,
		"per_page":    page.PerPage,
		"items":       systemItemsJSON(page.Items),
	})
}

// handleGetByKey serves GET /api/v1/artifacts/system/{key}.
func (h *SystemArtifactHandler) handleGetByKey(w http.ResponseWriter, r *http.Request, key string) {
	events, err := h.store.GetSystemArtifactByKey(r.Context(), key)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(events) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{
		"source":     "system",
		"key":        key,
		"operations": systemItemsJSON(events),
	})
}

// handleContent serves GET /api/v1/artifacts/system/{key}/content.
func (h *SystemArtifactHandler) handleContent(w http.ResponseWriter, r *http.Request, key string) {
	events, err := h.store.GetSystemArtifactByKey(r.Context(), key)
	if err != nil || len(events) == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	// Use the ActualPath from the most recent event.
	latest := events[len(events)-1]
	if latest.ActualPath == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	resolved := resolveArtifactPath(latest.ActualPath)
	f, err := os.Open(resolved)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Disposition",
		`attachment; filename="`+filepath.Base(latest.ActualPath)+`"`)
	io.Copy(w, f) //nolint:errcheck
}

// archiveRequest is the JSON body for POST .../archive.
type archiveRequest struct {
	Keys      []string `json:"keys"`
	Q         string   `json:"q"`
	SessionID []string `json:"session_id"`
}

// handleArchive serves POST /api/v1/artifacts/system/archive.
func (h *SystemArtifactHandler) handleArchive(w http.ResponseWriter, r *http.Request) {
	var req archiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Collect unique keys to include.
	keys := make(map[string]string) // key → actualPath

	// Keys specified directly.
	for _, k := range req.Keys {
		events, _ := h.store.GetSystemArtifactByKey(r.Context(), k)
		if len(events) > 0 {
			keys[k] = events[len(events)-1].ActualPath
		}
	}

	// Keys matched by glob.
	if req.Q != "" {
		events, _ := h.store.ListAllSystemArtifacts(r.Context(), store.SystemArtifactFilter{
			Q:          req.Q,
			SessionIDs: req.SessionID,
			Sort:       "occurred_at",
			Order:      "asc",
		})
		for _, e := range events {
			// Ascending occurred_at: later events overwrite ActualPath for the same key.
			keys[e.Key] = e.ActualPath
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="artifacts.zip"`)

	zw := zip.NewWriter(w)
	defer zw.Close()

	for key, actualPath := range keys {
		if actualPath == "" {
			continue
		}
		resolved := resolveArtifactPath(actualPath)
		f, err := os.Open(resolved)
		if err != nil {
			continue // skip missing files silently
		}
		zf, err := zw.Create(key)
		if err != nil {
			f.Close()
			continue
		}
		io.Copy(zf, f) //nolint:errcheck
		f.Close()
	}
}

// ---- JSON helpers ----

func systemItemsJSON(events []store.SystemArtifactEvent) []map[string]any {
	out := make([]map[string]any, len(events))
	for i, e := range events {
		out[i] = map[string]any{
			"key":         e.Key,
			"operation":   e.Operation,
			"agent_id":    e.AgentID,
			"session_id":  e.SessionID,
			"occurred_at": e.OccurredAt.UTC().Format(time.RFC3339),
			"tool_name":   e.ToolName,
			"sha":         e.ContentSHA,
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// resolveArtifactPath maps stored ActualPath values to a path the OS can open.
// On Windows with Git Bash/MSYS, agents may report paths like /tmp/... while
// the physical file lives under %TEMP%/...
func resolveArtifactPath(actualPath string) string {
	candidates := []string{actualPath, filepath.FromSlash(actualPath)}
	if runtime.GOOS == "windows" {
		slash := filepath.ToSlash(actualPath)
		if strings.HasPrefix(slash, "/tmp/") {
			if temp := os.Getenv("TEMP"); temp != "" {
				suffix := strings.TrimPrefix(slash, "/tmp/")
				candidates = append(candidates, filepath.Join(temp, suffix))
			}
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return actualPath
}
