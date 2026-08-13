package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	_ "modernc.org/sqlite" // SQLite driver (CGO-free)
)

// ArtifactStore is the persistence interface for artifact data.
type ArtifactStore interface {
	// Session operations.
	UpsertSession(ctx context.Context, s Session) error
	CloseSession(ctx context.Context, sessionID string) error

	// System artifact operations.
	SaveSystemArtifactEvent(ctx context.Context, e SystemArtifactEvent) error
	ListSystemArtifacts(ctx context.Context, f SystemArtifactFilter) (*SystemArtifactPage, error)
	// ListAllSystemArtifacts returns all matching events; Page/PerPage are ignored.
	ListAllSystemArtifacts(ctx context.Context, f SystemArtifactFilter) ([]SystemArtifactEvent, error)
	GetSystemArtifactByKey(ctx context.Context, key string) ([]SystemArtifactEvent, error)

	// User artifact operations.
	SaveUserArtifact(ctx context.Context, a UserArtifact) error
	GetUserArtifactByKey(ctx context.Context, key string) (*UserArtifact, error)
	ListUserArtifacts(ctx context.Context, f UserArtifactFilter) (*UserArtifactPage, error)
	// ListAllUserArtifacts returns all matching artifacts; Page/PerPage are ignored.
	ListAllUserArtifacts(ctx context.Context, f UserArtifactFilter) ([]UserArtifact, error)
	DeleteUserArtifact(ctx context.Context, key string) error

	// Close releases DB resources.
	Close() error
}

// SQLiteStore implements ArtifactStore using SQLite (via modernc.org/sqlite).
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database at dbPath and runs schema migrations.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open artifact db: %w", err)
	}
	// Single writer is sufficient; WAL mode improves concurrent reads.
	db.SetMaxOpenConns(1)

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate artifact db: %w", err)
	}
	return s, nil
}

// migrate creates tables and indexes.
func (s *SQLiteStore) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL,
    agent_name  TEXT NOT NULL DEFAULT '',
    started_at  DATETIME NOT NULL,
    ended_at    DATETIME
);

CREATE TABLE IF NOT EXISTS system_artifact_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id   TEXT NOT NULL REFERENCES sessions(id),
    agent_id     TEXT NOT NULL,
    key          TEXT NOT NULL,
    actual_path  TEXT NOT NULL DEFAULT '',
    operation    TEXT NOT NULL,
    occurred_at  DATETIME NOT NULL,
    tool_name    TEXT NOT NULL DEFAULT '',
    content_sha  TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sae_session  ON system_artifact_events(session_id);
CREATE INDEX IF NOT EXISTS idx_sae_agent    ON system_artifact_events(agent_id);
CREATE INDEX IF NOT EXISTS idx_sae_key      ON system_artifact_events(key);
CREATE INDEX IF NOT EXISTS idx_sae_occurred ON system_artifact_events(occurred_at);

CREATE TABLE IF NOT EXISTS user_artifacts (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    actual_path  TEXT NOT NULL,
    filename     TEXT NOT NULL,
    size         INTEGER NOT NULL DEFAULT 0,
    mime_type    TEXT NOT NULL DEFAULT '',
    content_sha  TEXT NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ua_key        ON user_artifacts(key);
CREATE INDEX IF NOT EXISTS idx_ua_created_at ON user_artifacts(created_at);
`
	_, err := s.db.Exec(ddl)
	return err
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// UpsertSession inserts or replaces a session record.
func (s *SQLiteStore) UpsertSession(ctx context.Context, sess Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO sessions(id, agent_id, agent_name, started_at, ended_at)
		 VALUES(?,?,?,?,?)`,
		sess.ID, sess.AgentID, sess.AgentName,
		sess.StartedAt.UTC().Format(time.RFC3339Nano),
		nullTime(sess.EndedAt),
	)
	return err
}

// CloseSession sets ended_at to now for the given session.
func (s *SQLiteStore) CloseSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET ended_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339Nano), sessionID,
	)
	return err
}

// SaveSystemArtifactEvent inserts a new system artifact event row.
func (s *SQLiteStore) SaveSystemArtifactEvent(ctx context.Context, e SystemArtifactEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_artifact_events
		 (session_id, agent_id, key, actual_path, operation, occurred_at, tool_name, content_sha)
		 VALUES(?,?,?,?,?,?,?,?)`,
		e.SessionID, e.AgentID, e.Key, e.ActualPath, e.Operation,
		e.OccurredAt.UTC().Format(time.RFC3339Nano),
		e.ToolName, e.ContentSHA,
	)
	return err
}

// ListSystemArtifacts returns a paginated, filtered list of system artifact events.
//
// Filtering strategy:
//  1. SQL handles session_id, agent_id, operation, since/until.
//  2. Go-side doublestar.Match filters the Key by Q glob.
//  3. IncludeDeleted=false excludes keys whose most recent operation is "delete".
func (s *SQLiteStore) ListSystemArtifacts(ctx context.Context, f SystemArtifactFilter) (*SystemArtifactPage, error) {
	perPage := normalizePerPage(f.PerPage)
	page := normalizePage(f.Page)

	all, err := s.filterSystemArtifacts(ctx, f)
	if err != nil {
		return nil, err
	}

	total := len(all)
	offset := (page - 1) * perPage
	if offset >= total {
		return &SystemArtifactPage{TotalCount: total, Page: page, PerPage: perPage}, nil
	}
	end := offset + perPage
	if end > total {
		end = total
	}
	return &SystemArtifactPage{
		TotalCount: total,
		Page:       page,
		PerPage:    perPage,
		Items:      all[offset:end],
	}, nil
}

// ListAllSystemArtifacts returns all matching system artifact events.
// Page and PerPage on the filter are ignored.
func (s *SQLiteStore) ListAllSystemArtifacts(ctx context.Context, f SystemArtifactFilter) ([]SystemArtifactEvent, error) {
	return s.filterSystemArtifacts(ctx, f)
}

func (s *SQLiteStore) filterSystemArtifacts(ctx context.Context, f SystemArtifactFilter) ([]SystemArtifactEvent, error) {
	// Build WHERE predicates that SQL can handle.
	var where []string
	var args []any

	if len(f.SessionIDs) > 0 {
		ph := placeholders(len(f.SessionIDs))
		where = append(where, "session_id IN ("+ph+")")
		for _, id := range f.SessionIDs {
			args = append(args, id)
		}
	}
	if len(f.AgentIDs) > 0 {
		ph := placeholders(len(f.AgentIDs))
		where = append(where, "agent_id IN ("+ph+")")
		for _, id := range f.AgentIDs {
			args = append(args, id)
		}
	}
	if f.Operation != "" {
		where = append(where, "operation=?")
		args = append(args, f.Operation)
	}
	if f.Since != nil {
		where = append(where, "occurred_at>=?")
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	if f.Until != nil {
		where = append(where, "occurred_at<=?")
		args = append(args, f.Until.UTC().Format(time.RFC3339Nano))
	}

	baseWhere := ""
	if len(where) > 0 {
		baseWhere = "WHERE " + strings.Join(where, " AND ")
	}

	orderCol := safeSystemSortCol(f.Sort)
	orderDir := safeOrder(f.Order)

	q := fmt.Sprintf(
		`SELECT id, session_id, agent_id, key, actual_path, operation, occurred_at, tool_name, content_sha
		 FROM system_artifact_events %s ORDER BY %s %s`,
		baseWhere, orderCol, orderDir,
	)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list system artifacts: %w", err)
	}
	defer rows.Close()

	var all []SystemArtifactEvent
	for rows.Next() {
		var e SystemArtifactEvent
		var ts string
		if err := rows.Scan(&e.ID, &e.SessionID, &e.AgentID, &e.Key,
			&e.ActualPath, &e.Operation, &ts, &e.ToolName, &e.ContentSHA); err != nil {
			return nil, fmt.Errorf("scan system artifact: %w", err)
		}
		e.OccurredAt, _ = time.Parse(time.RFC3339Nano, ts)
		all = append(all, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Go-side: glob filter.
	if f.Q != "" {
		filtered := all[:0]
		for _, e := range all {
			if ok, _ := doublestar.Match(f.Q, e.Key); ok {
				filtered = append(filtered, e)
			}
		}
		all = filtered
	}

	// Go-side: exclude deleted keys (latest op per key is "delete").
	if !f.IncludeDeleted {
		all = excludeDeletedKeys(all)
	}
	return all, nil
}

// excludeDeletedKeys returns only those events belonging to keys whose
// most recent operation is NOT "delete".
func excludeDeletedKeys(events []SystemArtifactEvent) []SystemArtifactEvent {
	// Determine the latest operation per key (events are already ordered by occurred_at).
	latest := make(map[string]string)
	latestTime := make(map[string]time.Time)
	for _, e := range events {
		if t, ok := latestTime[e.Key]; !ok || e.OccurredAt.After(t) {
			latestTime[e.Key] = e.OccurredAt
			latest[e.Key] = e.Operation
		}
	}
	// Keep events whose key is alive.
	out := make([]SystemArtifactEvent, 0, len(events))
	for _, e := range events {
		if latest[e.Key] != OperationDelete {
			out = append(out, e)
		}
	}
	return out
}

// GetSystemArtifactByKey returns all events for the given key, ordered by occurred_at asc.
func (s *SQLiteStore) GetSystemArtifactByKey(ctx context.Context, key string) ([]SystemArtifactEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, agent_id, key, actual_path, operation, occurred_at, tool_name, content_sha
		 FROM system_artifact_events WHERE key=? ORDER BY occurred_at ASC`,
		key,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SystemArtifactEvent
	for rows.Next() {
		var e SystemArtifactEvent
		var ts string
		if err := rows.Scan(&e.ID, &e.SessionID, &e.AgentID, &e.Key,
			&e.ActualPath, &e.Operation, &ts, &e.ToolName, &e.ContentSHA); err != nil {
			return nil, err
		}
		e.OccurredAt, _ = time.Parse(time.RFC3339Nano, ts)
		events = append(events, e)
	}
	return events, rows.Err()
}

// SaveUserArtifact inserts or replaces a user artifact record.
func (s *SQLiteStore) SaveUserArtifact(ctx context.Context, a UserArtifact) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO user_artifacts
		 (id, key, actual_path, filename, size, mime_type, content_sha, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Key, a.ActualPath, a.Filename, a.Size, a.MIMEType, a.ContentSHA,
		a.CreatedAt.UTC().Format(time.RFC3339Nano),
		a.UpdatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

// GetUserArtifactByKey returns the user artifact for the given key, or nil if not found.
func (s *SQLiteStore) GetUserArtifactByKey(ctx context.Context, key string) (*UserArtifact, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, key, actual_path, filename, size, mime_type, content_sha, created_at, updated_at
		 FROM user_artifacts WHERE key=?`,
		key,
	)
	var a UserArtifact
	var cat, uat string
	err := row.Scan(&a.ID, &a.Key, &a.ActualPath, &a.Filename, &a.Size,
		&a.MIMEType, &a.ContentSHA, &cat, &uat)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, cat)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, uat)
	return &a, nil
}

// ListUserArtifacts returns a paginated, filtered list of user artifacts.
func (s *SQLiteStore) ListUserArtifacts(ctx context.Context, f UserArtifactFilter) (*UserArtifactPage, error) {
	perPage := normalizePerPage(f.PerPage)
	page := normalizePage(f.Page)

	all, err := s.filterUserArtifacts(ctx, f)
	if err != nil {
		return nil, err
	}

	total := len(all)
	offset := (page - 1) * perPage
	if offset >= total {
		return &UserArtifactPage{TotalCount: total, Page: page, PerPage: perPage}, nil
	}
	end := offset + perPage
	if end > total {
		end = total
	}
	return &UserArtifactPage{
		TotalCount: total,
		Page:       page,
		PerPage:    perPage,
		Items:      all[offset:end],
	}, nil
}

// ListAllUserArtifacts returns all matching user artifacts.
// Page and PerPage on the filter are ignored.
func (s *SQLiteStore) ListAllUserArtifacts(ctx context.Context, f UserArtifactFilter) ([]UserArtifact, error) {
	return s.filterUserArtifacts(ctx, f)
}

func (s *SQLiteStore) filterUserArtifacts(ctx context.Context, f UserArtifactFilter) ([]UserArtifact, error) {
	orderCol := safeUserSortCol(f.Sort)
	orderDir := safeOrder(f.Order)

	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf(
			`SELECT id, key, actual_path, filename, size, mime_type, content_sha, created_at, updated_at
			 FROM user_artifacts ORDER BY %s %s`,
			orderCol, orderDir,
		),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []UserArtifact
	for rows.Next() {
		var a UserArtifact
		var cat, uat string
		if err := rows.Scan(&a.ID, &a.Key, &a.ActualPath, &a.Filename, &a.Size,
			&a.MIMEType, &a.ContentSHA, &cat, &uat); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, cat)
		a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, uat)
		all = append(all, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Go-side: glob filter.
	if f.Q != "" {
		filtered := all[:0]
		for _, a := range all {
			if ok, _ := doublestar.Match(f.Q, a.Key); ok {
				filtered = append(filtered, a)
			}
		}
		all = filtered
	}
	return all, nil
}

// DeleteUserArtifact removes the user artifact record for the given key.
func (s *SQLiteStore) DeleteUserArtifact(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_artifacts WHERE key=?`, key)
	return err
}

// ---- helpers ----

// DefaultPerPage is the safety default when PerPage is omitted or non-positive.
const DefaultPerPage = 100

func normalizePerPage(n int) int {
	if n <= 0 {
		return DefaultPerPage
	}
	return n
}

func normalizePage(n int) int {
	if n <= 0 {
		return 1
	}
	return n
}

func placeholders(n int) string {
	p := make([]string, n)
	for i := range n {
		p[i] = "?"
	}
	return strings.Join(p, ",")
}

func safeSystemSortCol(s string) string {
	switch s {
	case "key", "operation":
		return s
	default:
		return "occurred_at"
	}
}

func safeUserSortCol(s string) string {
	switch s {
	case "key", "size", "updated_at":
		return s
	default:
		return "created_at"
	}
}

func safeOrder(s string) string {
	if strings.ToLower(s) == "desc" {
		return "DESC"
	}
	return "ASC"
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}
