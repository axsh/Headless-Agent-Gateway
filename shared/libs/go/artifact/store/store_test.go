package store_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/axsh/arctic-tern/shared/libs/go/artifact/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) store.ArtifactStore {
	t.Helper()
	dir := t.TempDir()
	s, err := store.NewSQLiteStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func insertSession(t *testing.T, s store.ArtifactStore, id, agentID string) {
	t.Helper()
	err := s.UpsertSession(context.Background(), store.Session{
		ID: id, AgentID: agentID, StartedAt: time.Now(),
	})
	require.NoError(t, err)
}

// ---- Session tests ----

func TestUpsertAndCloseSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.UpsertSession(ctx, store.Session{
		ID: "sess-1", AgentID: "cursor", StartedAt: time.Now(),
	})
	require.NoError(t, err)

	err = s.CloseSession(ctx, "sess-1")
	require.NoError(t, err)
}

// ---- SystemArtifact tests ----

func TestSaveAndListSystemArtifacts_NoFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s1", "cursor")

	events := []store.SystemArtifactEvent{
		{SessionID: "s1", AgentID: "cursor", Key: "a/b.go", Operation: store.OperationCreate, OccurredAt: time.Now()},
		{SessionID: "s1", AgentID: "cursor", Key: "a/c.go", Operation: store.OperationCreate, OccurredAt: time.Now()},
	}
	for _, e := range events {
		require.NoError(t, s.SaveSystemArtifactEvent(ctx, e))
	}

	page, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{PerPage: 10})
	require.NoError(t, err)
	assert.Equal(t, 2, page.TotalCount)
	assert.Len(t, page.Items, 2)
}

func TestListSystemArtifacts_GlobFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s1", "cursor")

	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", Key: "a/b.go", Operation: store.OperationCreate, OccurredAt: time.Now(),
	})
	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", Key: "a/b.txt", Operation: store.OperationCreate, OccurredAt: time.Now(),
	})

	page, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{Q: "**/*.go", PerPage: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, page.TotalCount)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "a/b.go", page.Items[0].Key)
}

func TestListSystemArtifacts_SessionFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s1", "cursor")
	insertSession(t, s, "s2", "cursor")

	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", Key: "a.go", Operation: store.OperationCreate, OccurredAt: time.Now(),
	})
	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s2", AgentID: "cursor", Key: "b.go", Operation: store.OperationCreate, OccurredAt: time.Now(),
	})

	page, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{
		SessionIDs: []string{"s1"}, PerPage: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, page.TotalCount)
	assert.Equal(t, "a.go", page.Items[0].Key)
}

func TestListSystemArtifacts_TurnFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s1", "cursor")

	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", TurnID: "t1", Key: "a.go",
		Operation: store.OperationCreate, OccurredAt: time.Now(),
	})
	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", TurnID: "t2", Key: "b.go",
		Operation: store.OperationCreate, OccurredAt: time.Now(),
	})

	page, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{
		SessionIDs: []string{"s1"},
		TurnIDs:    []string{"t2"},
		PerPage:    10,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "b.go", page.Items[0].Key)
	assert.Equal(t, "t2", page.Items[0].TurnID)
}

func TestListSystemArtifacts_ExcludeDeleted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s1", "cursor")

	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", Key: "x.go", Operation: store.OperationCreate, OccurredAt: time.Now(),
	})
	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", Key: "x.go", Operation: store.OperationDelete, OccurredAt: time.Now().Add(time.Second),
	})
	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", Key: "y.go", Operation: store.OperationCreate, OccurredAt: time.Now(),
	})

	// IncludeDeleted=false (default): x.go should not appear because it was deleted.
	page, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{PerPage: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, page.TotalCount)
	assert.Equal(t, "y.go", page.Items[0].Key)

	// IncludeDeleted=true: all 3 events appear.
	pageAll, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{PerPage: 10, IncludeDeleted: true})
	require.NoError(t, err)
	assert.Equal(t, 3, pageAll.TotalCount)
}

func TestListSystemArtifacts_Pagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s1", "cursor")

	for i := range 5 {
		key := filepath.Join("pkg", string(rune('a'+i))+".go")
		_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s1", AgentID: "cursor", Key: key, Operation: store.OperationCreate, OccurredAt: time.Now(),
		})
	}

	page1, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{Page: 1, PerPage: 2, Sort: "key", Order: "asc"})
	require.NoError(t, err)
	assert.Equal(t, 5, page1.TotalCount)
	assert.Len(t, page1.Items, 2)

	page3, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{Page: 3, PerPage: 2, Sort: "key", Order: "asc"})
	require.NoError(t, err)
	assert.Len(t, page3.Items, 1)
}

func TestGetSystemArtifactByKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s1", "cursor")

	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", Key: "handler.go",
		ActualPath: "/proj/handler.go", Operation: store.OperationCreate, OccurredAt: time.Now(),
	})
	_ = s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s1", AgentID: "cursor", Key: "handler.go",
		ActualPath: "/proj/handler.go", Operation: store.OperationUpdate, OccurredAt: time.Now().Add(time.Second),
	})

	events, err := s.GetSystemArtifactByKey(ctx, "handler.go")
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, store.OperationCreate, events[0].Operation)
	assert.Equal(t, store.OperationUpdate, events[1].Operation)
}

// ---- UserArtifact tests ----

func TestSaveAndGetUserArtifact(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	art := store.UserArtifact{
		ID: "uuid-1", Key: "datasets/a.csv",
		ActualPath: "/tmp/uuid-1", Filename: "a.csv",
		Size: 100, MIMEType: "text/csv", ContentSHA: "abc",
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, s.SaveUserArtifact(ctx, art))

	got, err := s.GetUserArtifactByKey(ctx, "datasets/a.csv")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "datasets/a.csv", got.Key)
	assert.Equal(t, int64(100), got.Size)
}

func TestGetUserArtifact_NotFound(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetUserArtifactByKey(context.Background(), "missing.txt")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestListUserArtifacts_GlobFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	keys := []string{"datasets/a.csv", "datasets/b.csv", "configs/x.yaml"}
	for _, k := range keys {
		now := time.Now()
		_ = s.SaveUserArtifact(ctx, store.UserArtifact{
			ID: k, Key: k, ActualPath: "/tmp/" + k, Filename: filepath.Base(k),
			CreatedAt: now, UpdatedAt: now,
		})
	}

	page, err := s.ListUserArtifacts(ctx, store.UserArtifactFilter{Q: "datasets/**", PerPage: 10})
	require.NoError(t, err)
	assert.Equal(t, 2, page.TotalCount)

	pageAll, err := s.ListUserArtifacts(ctx, store.UserArtifactFilter{PerPage: 10})
	require.NoError(t, err)
	assert.Equal(t, 3, pageAll.TotalCount)
}

func TestDeleteUserArtifact(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	_ = s.SaveUserArtifact(ctx, store.UserArtifact{
		ID: "uuid-del", Key: "to-delete.txt", ActualPath: "/tmp/uuid-del",
		Filename: "to-delete.txt", CreatedAt: now, UpdatedAt: now,
	})

	require.NoError(t, s.DeleteUserArtifact(ctx, "to-delete.txt"))

	got, err := s.GetUserArtifactByKey(ctx, "to-delete.txt")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSaveUserArtifact_Upsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	art := store.UserArtifact{
		ID: "uuid-up", Key: "doc.txt", ActualPath: "/tmp/uuid-up",
		Filename: "doc.txt", Size: 10, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, s.SaveUserArtifact(ctx, art))

	// Overwrite with same key but different content.
	art.Size = 20
	art.UpdatedAt = now.Add(time.Second)
	require.NoError(t, s.SaveUserArtifact(ctx, art))

	got, err := s.GetUserArtifactByKey(ctx, "doc.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(20), got.Size)
}

// Ensure the store file is removed after test.
func TestClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "close.db")
	s, err := store.NewSQLiteStore(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Close())
	_, err = os.Stat(dbPath)
	assert.NoError(t, err) // file should still exist, just closed
}

func TestListSystemArtifacts_DefaultPerPage100_NoHardMax(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s1", "cursor")

	for i := 0; i < 150; i++ {
		key := "bulk/file_" + pad3(i) + ".go"
		require.NoError(t, s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s1", AgentID: "cursor", Key: key,
			Operation: store.OperationCreate, OccurredAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		}))
	}

	def, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{SessionIDs: []string{"s1"}})
	require.NoError(t, err)
	assert.Equal(t, 100, def.PerPage)
	assert.Equal(t, 150, def.TotalCount)
	assert.Len(t, def.Items, 100)

	wide, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{
		SessionIDs: []string{"s1"}, PerPage: 200,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, wide.PerPage)
	assert.Equal(t, 150, wide.TotalCount)
	assert.Len(t, wide.Items, 150)
}

func TestListSystemArtifacts_FiftyItems_ExplicitPagination(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s50", "cursor")
	for i := 0; i < 50; i++ {
		require.NoError(t, s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s50", AgentID: "cursor", Key: "gen/file_" + pad3(i) + ".go",
			Operation: store.OperationCreate, OccurredAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		}))
	}
	p1, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{
		SessionIDs: []string{"s50"}, Page: 1, PerPage: 30, Sort: "key", Order: "asc",
	})
	require.NoError(t, err)
	assert.Equal(t, 50, p1.TotalCount)
	assert.Len(t, p1.Items, 30)

	p2, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{
		SessionIDs: []string{"s50"}, Page: 2, PerPage: 30, Sort: "key", Order: "asc",
	})
	require.NoError(t, err)
	assert.Len(t, p2.Items, 20)

	seen := map[string]struct{}{}
	for _, it := range append(p1.Items, p2.Items...) {
		seen[it.Key] = struct{}{}
	}
	assert.Len(t, seen, 50)
}

func TestListSystemArtifacts_AfterCloseSession_StillListed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s-close", "cursor")
	require.NoError(t, s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
		SessionID: "s-close", AgentID: "cursor", Key: "keep.go",
		Operation: store.OperationCreate, OccurredAt: time.Now(),
	}))
	require.NoError(t, s.CloseSession(ctx, "s-close"))

	page, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{
		SessionIDs: []string{"s-close"}, PerPage: 10,
	})
	require.NoError(t, err)
	require.Equal(t, 1, page.TotalCount)
	assert.Equal(t, "keep.go", page.Items[0].Key)
}

func TestListSystemArtifacts_SeventyUpdates_ThreePages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s70", "cursor")
	for i := 0; i < 70; i++ {
		key := "updated/file_" + pad3(i) + ".go"
		require.NoError(t, s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s70", AgentID: "cursor", Key: key,
			Operation: store.OperationCreate, OccurredAt: time.Now().Add(time.Duration(i) * time.Millisecond),
			ToolName: "Write",
		}))
		require.NoError(t, s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s70", AgentID: "cursor", Key: key,
			Operation: store.OperationUpdate, OccurredAt: time.Now().Add(time.Hour + time.Duration(i)*time.Millisecond),
			ToolName: "StrReplace",
		}))
	}

	var all []store.SystemArtifactEvent
	for pageNum := 1; pageNum <= 4; pageNum++ {
		page, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{
			SessionIDs: []string{"s70"}, Operation: store.OperationUpdate,
			Page: pageNum, PerPage: 30, Sort: "key", Order: "asc",
		})
		require.NoError(t, err)
		assert.Equal(t, 70, page.TotalCount)
		switch pageNum {
		case 1, 2:
			assert.Len(t, page.Items, 30)
		case 3:
			assert.Len(t, page.Items, 10)
		case 4:
			assert.Len(t, page.Items, 0)
		}
		all = append(all, page.Items...)
	}
	seen := map[string]struct{}{}
	for _, it := range all {
		seen[it.Key] = struct{}{}
		assert.Equal(t, store.OperationUpdate, it.Operation)
	}
	assert.Len(t, seen, 70)
	require.GreaterOrEqual(t, len(all), 70)
	assert.True(t, all[29].Key < all[30].Key)
	assert.True(t, all[59].Key < all[60].Key)
}

func TestListAllSystemArtifacts_ReturnsMoreThanDefaultPage(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	insertSession(t, s, "s-all", "cursor")
	for i := 0; i < 70; i++ {
		key := "all/file_" + pad3(i) + ".go"
		require.NoError(t, s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s-all", AgentID: "cursor", Key: key,
			Operation: store.OperationCreate, OccurredAt: time.Now().Add(time.Duration(i) * time.Millisecond),
		}))
		require.NoError(t, s.SaveSystemArtifactEvent(ctx, store.SystemArtifactEvent{
			SessionID: "s-all", AgentID: "cursor", Key: key,
			Operation: store.OperationUpdate, OccurredAt: time.Now().Add(time.Hour + time.Duration(i)*time.Millisecond),
		}))
	}
	all, err := s.ListAllSystemArtifacts(ctx, store.SystemArtifactFilter{SessionIDs: []string{"s-all"}})
	require.NoError(t, err)
	assert.Len(t, all, 140)

	page, err := s.ListSystemArtifacts(ctx, store.SystemArtifactFilter{SessionIDs: []string{"s-all"}})
	require.NoError(t, err)
	assert.Equal(t, 140, page.TotalCount)
	assert.Len(t, page.Items, 100)
}

func pad3(i int) string {
	return fmt.Sprintf("%03d", i)
}
