package api

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/storage"
	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/google/uuid"
)

// UserArtifactHandler handles /api/v1/artifacts/user routes.
type UserArtifactHandler struct {
	store   store.ArtifactStore
	storage *storage.UserArtifactStorage
}

// NewUserArtifactHandler creates a handler for user-uploaded artifacts.
func NewUserArtifactHandler(s store.ArtifactStore, st *storage.UserArtifactStorage) *UserArtifactHandler {
	return &UserArtifactHandler{store: s, storage: st}
}

// RegisterRoutes registers user artifact routes on mux under prefix.
// prefix should be "/api/v1/artifacts/user" (no trailing slash).
func (h *UserArtifactHandler) RegisterRoutes(mux *http.ServeMux, prefix string) {
	mux.HandleFunc(prefix, h.routeRoot)
	mux.HandleFunc(prefix+"/", h.routeByKey)
}

// routeRoot handles GET (list).
func (h *UserArtifactHandler) routeRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleList(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// routeByKey dispatches:
//   - POST .../archive
//   - PUT  .../{key}         → handlePut
//   - GET  .../{key}/content → handleContent
//   - GET  .../{key}         → handleGetMeta
//   - DELETE .../{key}       → handleDelete
func (h *UserArtifactHandler) routeByKey(w http.ResponseWriter, r *http.Request) {
	subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/artifacts/user/")
	subPath = strings.Trim(subPath, "/")

	if r.Method == http.MethodPost && subPath == "archive" {
		h.handleArchive(w, r)
		return
	}

	if strings.HasSuffix(subPath, "/content") {
		key := strings.TrimSuffix(subPath, "/content")
		switch r.Method {
		case http.MethodGet:
			h.handleContent(w, r, key)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodPut:
		h.handlePut(w, r, subPath)
	case http.MethodGet:
		h.handleGetMeta(w, r, subPath)
	case http.MethodDelete:
		h.handleDelete(w, r, subPath)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleList serves GET /api/v1/artifacts/user.
func (h *UserArtifactHandler) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.UserArtifactFilter{
		Q:     q.Get("q"),
		Sort:  q.Get("sort"),
		Order: q.Get("order"),
	}
	if p := q.Get("page"); p != "" {
		f.Page, _ = strconv.Atoi(p)
	}
	if pp := q.Get("per_page"); pp != "" {
		f.PerPage, _ = strconv.Atoi(pp)
	}

	page, err := h.store.ListUserArtifacts(r.Context(), f)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"source":      "user",
		"total_count": page.TotalCount,
		"page":        page.Page,
		"per_page":    page.PerPage,
		"items":       userItemsJSON(page.Items),
	})
}

// handlePut serves PUT /api/v1/artifacts/user/{key}.
// Creates or replaces the artifact at the given logical key.
func (h *UserArtifactHandler) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}

	// Check if the artifact already exists to determine created vs updated.
	existing, err := h.store.GetUserArtifactByKey(r.Context(), key)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	id := uuid.NewString()
	if existing != nil {
		id = existing.ID
		// Delete old physical file (best-effort; ID is reused so it'll be overwritten).
		_ = h.storage.Delete(existing.ID)
	}

	info, err := h.storage.Write(id, r.Body)
	if err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	createdAt := now
	if existing != nil {
		createdAt = existing.CreatedAt
	}

	art := store.UserArtifact{
		ID:         id,
		Key:        key,
		ActualPath: info.ActualPath,
		Filename:   lastPathSegment(key),
		Size:       info.Size,
		MIMEType:   info.MIMEType,
		ContentSHA: info.SHA256,
		CreatedAt:  createdAt,
		UpdatedAt:  now,
	}
	if err := h.store.SaveUserArtifact(r.Context(), art); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	status := "created"
	code := http.StatusCreated
	if existing != nil {
		status = "updated"
		code = http.StatusOK
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"source": "user",
		"key":    key,
		"status": status,
		"sha":    info.SHA256,
		"size":   info.Size,
	})
}

// handleGetMeta serves GET /api/v1/artifacts/user/{key} (metadata only).
func (h *UserArtifactHandler) handleGetMeta(w http.ResponseWriter, r *http.Request, key string) {
	art, err := h.store.GetUserArtifactByKey(r.Context(), key)
	if err != nil || art == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, userItemJSON(*art))
}

// handleContent serves GET /api/v1/artifacts/user/{key}/content.
func (h *UserArtifactHandler) handleContent(w http.ResponseWriter, r *http.Request, key string) {
	art, err := h.store.GetUserArtifactByKey(r.Context(), key)
	if err != nil || art == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	rc, err := h.storage.Read(art.ID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer rc.Close()

	if art.MIMEType != "" {
		w.Header().Set("Content-Type", art.MIMEType)
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+art.Filename+`"`)
	io.Copy(w, rc) //nolint:errcheck
}

// handleDelete serves DELETE /api/v1/artifacts/user/{key}.
func (h *UserArtifactHandler) handleDelete(w http.ResponseWriter, r *http.Request, key string) {
	art, err := h.store.GetUserArtifactByKey(r.Context(), key)
	if err != nil || art == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	_ = h.storage.Delete(art.ID)
	if err := h.store.DeleteUserArtifact(r.Context(), key); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleArchive serves POST /api/v1/artifacts/user/archive.
func (h *UserArtifactHandler) handleArchive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keys []string `json:"keys"`
		Q    string   `json:"q"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Collect artifacts.
	arts := make(map[string]store.UserArtifact)
	for _, k := range req.Keys {
		a, _ := h.store.GetUserArtifactByKey(r.Context(), k)
		if a != nil {
			arts[k] = *a
		}
	}
	if req.Q != "" {
		page, _ := h.store.ListUserArtifacts(r.Context(), store.UserArtifactFilter{
			Q: req.Q, PerPage: 100,
		})
		for _, a := range page.Items {
			if _, exists := arts[a.Key]; !exists {
				arts[a.Key] = a
			}
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="user-artifacts.zip"`)

	zw := zip.NewWriter(w)
	defer zw.Close()

	for key, art := range arts {
		rc, err := h.storage.Read(art.ID)
		if err != nil {
			continue
		}
		zf, err := zw.Create(key)
		if err != nil {
			rc.Close()
			continue
		}
		io.Copy(zf, rc) //nolint:errcheck
		rc.Close()
	}
}

// ---- JSON helpers ----

func userItemJSON(a store.UserArtifact) map[string]any {
	return map[string]any{
		"source":     "user",
		"id":         a.ID,
		"key":        a.Key,
		"filename":   a.Filename,
		"size":       a.Size,
		"mime_type":  a.MIMEType,
		"sha":        a.ContentSHA,
		"created_at": a.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": a.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func userItemsJSON(arts []store.UserArtifact) []map[string]any {
	out := make([]map[string]any, len(arts))
	for i, a := range arts {
		out[i] = userItemJSON(a)
	}
	return out
}

func lastPathSegment(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) == 0 {
		return p
	}
	return parts[len(parts)-1]
}
