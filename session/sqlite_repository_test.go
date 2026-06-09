package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"leadingAgent/agent/foundation"
)

func tempDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "sessions.db")
}

func newTestSQLite(t *testing.T) *SQLiteRepository {
	t.Helper()
	repo, err := NewSQLiteRepository(tempDB(t))
	if err != nil {
		t.Fatalf("NewSQLiteRepository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func baseSession(userID string) *Session {
	return &Session{
		ID:           NewID(),
		UserID:       userID,
		State:        StateActive,
		SystemPrompt: "be helpful",
		ModelConfig:  ModelConfig{Name: "test-model", Provider: "test", Temperature: 0.7},
		MetaData:     map[string]interface{}{"client": "test"},
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		Version:      1,
	}
}

func TestSQLite_CreateGet(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLite(t)

	s := baseSession("u1")
	s.Messages = append(s.Messages, foundation.Message{Role: foundation.RoleUser, Content: "hi"})
	s.ToolCalls = append(s.ToolCalls, ToolCallMeta{ID: "t1", Name: "f1", Success: true})

	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != "u1" || got.State != StateActive {
		t.Fatalf("unexpected user/state: %+v", got)
	}
	if got.SystemPrompt != s.SystemPrompt {
		t.Fatalf("system_prompt mismatch: got %q want %q", got.SystemPrompt, s.SystemPrompt)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "hi" {
		t.Fatalf("messages roundtrip failed: %+v", got.Messages)
	}
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "f1" {
		t.Fatalf("tool_calls roundtrip failed: %+v", got.ToolCalls)
	}
	if got.ModelConfig.Name != "test-model" || got.ModelConfig.Temperature != 0.7 {
		t.Fatalf("model_config roundtrip failed: %+v", got.ModelConfig)
	}
	if v, ok := got.MetaData["client"].(string); !ok || v != "test" {
		t.Fatalf("meta roundtrip failed: %+v", got.MetaData)
	}
}

func TestSQLite_CreateDuplicateReturnsConflict(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLite(t)
	s := baseSession("u1")
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, s); err != ErrConflict {
		t.Fatalf("second Create expected ErrConflict, got: %v", err)
	}
}

func TestSQLite_GetNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLite(t)
	if _, err := repo.Get(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestSQLite_UpdateOptimisticLock(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLite(t)
	s := baseSession("u1")
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 第一次 AppendMessage：版本 1->2
	s2, _ := repo.Get(ctx, s.ID)
	s2.AppendMessage(foundation.Message{Role: foundation.RoleUser, Content: "msg-1"})
	if err := repo.Update(ctx, s2); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// 再一次 AppendMessage：版本 2->3
	s3, _ := repo.Get(ctx, s.ID)
	s3.AppendMessage(foundation.Message{Role: foundation.RoleAssistant, Content: "reply"})
	if err := repo.Update(ctx, s3); err != nil {
		t.Fatalf("Update2: %v", err)
	}

	// 再拿最新确认 messages 数=2，且 version>=3
	final, _ := repo.Get(ctx, s.ID)
	if len(final.Messages) != 2 {
		t.Fatalf("expected 2 messages, got: %d", len(final.Messages))
	}
	if final.Version < 3 {
		t.Fatalf("expected version >=3, got: %d", final.Version)
	}

	// 故意用旧版本覆盖：应命中 ErrConflict
	stale := *s2
	stale.Messages = nil
	stale.Version = 99 // 人为构造不匹配
	if err := repo.Update(ctx, &stale); err != ErrConflict {
		t.Fatalf("expected ErrConflict on stale version, got: %v", err)
	}
}

func TestSQLite_Delete(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLite(t)
	s := baseSession("u1")
	_ = repo.Create(ctx, s)
	if err := repo.Delete(ctx, s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, s.ID); err != ErrNotFound {
		t.Fatalf("after delete expected ErrNotFound, got: %v", err)
	}
	if err := repo.Delete(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("delete unknown expected ErrNotFound, got: %v", err)
	}
}

func TestSQLite_ListByUser(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLite(t)
	for i := 0; i < 5; i++ {
		s := baseSession("u1")
		// 让 updated_at 不同，便于验证排序
		s.CreatedAt = s.CreatedAt.Add(time.Duration(i) * time.Minute)
		s.UpdatedAt = s.CreatedAt
		_ = repo.Create(ctx, s)
	}
	other := baseSession("u2")
	_ = repo.Create(ctx, other)

	list, err := repo.ListByUser(ctx, "u1", 0)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 5 {
		t.Fatalf("expected 5 for u1, got: %d", len(list))
	}
	// 按 updated_at DESC
	for i := 1; i < len(list); i++ {
		if list[i-1].UpdatedAt.Before(list[i].UpdatedAt) {
			t.Fatalf("expected DESC order")
		}
	}

	limit2, _ := repo.ListByUser(ctx, "u1", 2)
	if len(limit2) != 2 {
		t.Fatalf("expected limit 2, got: %d", len(limit2))
	}
}

func TestSQLite_ListExpired(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLite(t)

	good := baseSession("u1")
	good.ExpiresAt = time.Now().UTC().Add(time.Hour)
	_ = repo.Create(ctx, good)

	expired := baseSession("u1")
	expired.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	_ = repo.Create(ctx, expired)

	done := baseSession("u1")
	done.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	done.State = StateCompleted
	_ = repo.Create(ctx, done)

	list, err := repo.ListExpired(ctx, time.Now().UTC(), 100)
	if err != nil {
		t.Fatalf("ListExpired: %v", err)
	}
	if len(list) != 1 || list[0].ID != expired.ID {
		t.Fatalf("expected only expired non-terminal, got: %+v", list)
	}
}

func TestSQLite_Touch(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLite(t)
	s := baseSession("u1")
	_ = repo.Create(ctx, s)

	newExp := time.Now().UTC().Add(48 * time.Hour)
	if err := repo.Touch(ctx, s.ID, newExp); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, _ := repo.Get(ctx, s.ID)
	if got.ExpiresAt.Before(newExp.Add(-time.Second)) || got.ExpiresAt.After(newExp.Add(time.Second)) {
		t.Fatalf("expires_at not updated: got %v want ~%v", got.ExpiresAt, newExp)
	}
	if got.Version < 2 {
		t.Fatalf("version not bumped after touch: %d", got.Version)
	}

	if err := repo.Touch(ctx, "nope", newExp); err != ErrNotFound {
		t.Fatalf("Touch missing expected ErrNotFound, got: %v", err)
	}
}

func TestSQLite_ManagerIntegration(t *testing.T) {
	ctx := context.Background()
	repo := newTestSQLite(t)
	mgr := NewManager(repo, WithTTL(time.Hour), WithMaxMessages(50))
	defer mgr.Close()

	// Create + Append 走 manager 链路
	sess, err := mgr.GetOrCreate(ctx, "", "u1")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	userMsg := foundation.Message{Role: foundation.RoleUser, Content: "hello"}
	updated, err := mgr.Append(ctx, sess.ID, "u1", userMsg)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(updated.Messages) != 1 || updated.Messages[0].Content != "hello" {
		t.Fatalf("Append result unexpected: %+v", updated.Messages)
	}

	// 直接从存储读取，验证落盘
	fresh, err := repo.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Get after append: %v", err)
	}
	if len(fresh.Messages) != 1 {
		t.Fatalf("persisted messages: got %d want 1", len(fresh.Messages))
	}
}

func TestSQLite_InMemoryDBPath(t *testing.T) {
	// ":memory:" 应能打开并正常工作，但关闭后数据丢失。
	repo, err := NewSQLiteRepository(":memory:")
	if err != nil {
		t.Fatalf("open memory db: %v", err)
	}
	defer repo.Close()
	ctx := context.Background()
	s := baseSession("u1")
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Get(ctx, s.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

func TestSQLite_InvalidPathStillErrors(t *testing.T) {
	if _, err := NewSQLiteRepository(""); err == nil {
		t.Fatalf("expected error for empty path")
	}
}

func TestSQLite_FilePersistsAcrossReopen(t *testing.T) {
	// 用真实文件，关闭后再打开应能读到之前的 session。
	path := filepath.Join(t.TempDir(), "persist.db")
	repo1, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	ctx := context.Background()
	s := baseSession("u_across")
	s.Messages = append(s.Messages, foundation.Message{Role: foundation.RoleUser, Content: "across"})
	if err := repo1.Create(ctx, s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = repo1.Close()

	repo2, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer repo2.Close()
	got, err := repo2.Get(ctx, s.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != "across" {
		t.Fatalf("persistence broken: %+v", got.Messages)
	}
	_ = os.Remove(path) // 由 TempDir 兜底清理，这里做个显式删除
}
