package session

import (
	"context"
	"testing"
	"time"

	"leadingAgent/agent/foundation"
)

func TestCreateAndAppend(t *testing.T) {
	m := NewManager(NewInMemoryRepository(), WithTTL(time.Hour))
	defer m.Close()

	ctx := context.Background()
	s, err := m.Create(ctx, "", "u1")
	if err != nil || s.ID == "" {
		t.Fatalf("Create failed: %v", err)
	}

	if got, err := m.Get(ctx, s.ID, "u1"); err != nil || got.ID != s.ID {
		t.Fatalf("Get failed: %v got=%+v", err, got)
	}

	// 水平越权防护：错的 user 查不到
	if _, err := m.Get(ctx, s.ID, "other"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	updated, err := m.Append(ctx, s.ID, "u1", foundation.Message{Role: foundation.RoleUser, Content: "hi"})
	if err != nil || len(updated.Messages) != 1 {
		t.Fatalf("Append failed: %v updated=%+v", err, updated)
	}

	updated, err = m.Append(ctx, s.ID, "u1", foundation.Message{Role: foundation.RoleAssistant, Content: "hello"})
	if err != nil || len(updated.Messages) != 2 {
		t.Fatalf("Append second failed: %v", err)
	}

	// caller 可指定 id
	custom, err := m.Create(ctx, "my-session", "u1")
	if err != nil || custom.ID != "my-session" {
		t.Fatalf("Create with custom id failed: %v id=%s", err, custom.ID)
	}
	// 重复 id => ErrConflict
	if _, err := m.Create(ctx, "my-session", "u1"); err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestExpiry(t *testing.T) {
	// 场景 1：过期但在续期窗口内 → 应被续期（相同 ID）
	m1 := NewManager(NewInMemoryRepository(), WithTTL(100*time.Millisecond))
	defer m1.Close()
	ctx := context.Background()

	s1, err := m1.Create(ctx, "", "u1")
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)

	// 读操作：过期会话仍然可读
	if _, err := m1.Get(ctx, s1.ID, "u1"); err != nil {
		t.Fatalf("expected Get to succeed for expired session, got %v", err)
	}

	// 写操作：过期会话不可再追加消息
	if _, err := m1.Append(ctx, s1.ID, "u1", foundation.Message{Role: foundation.RoleUser, Content: "x"}); err != ErrExpired {
		t.Fatalf("expected ErrExpired, got %v", err)
	}

	// GetOrCreate：过期但在默认 7 天续期窗口内 → 应被续期（相同 ID）
	renewed, err := m1.GetOrCreate(ctx, s1.ID, "u1")
	if err != nil {
		t.Fatalf("GetOrCreate (renew) failed: %v", err)
	}
	if renewed.ID != s1.ID {
		t.Fatalf("expected renewed session to keep ID, got %s (old=%s)", renewed.ID, s1.ID)
	}
	if renewed.ExpiresAt.Before(s1.ExpiresAt) {
		t.Fatal("expected renewed ExpiresAt to be later")
	}

	// 场景 2：超出续期窗口 → 应创建新会话（不同 ID）
	m2 := NewManager(NewInMemoryRepository(),
		WithTTL(100*time.Millisecond),
		WithRenewWindow(10*time.Millisecond), // 很短的续期窗口
	)
	defer m2.Close()

	s2, err := m2.Create(ctx, "", "u2")
	if err != nil {
		t.Fatal(err)
	}
	// 先追加一条消息，使会话被视为"有内容"
	if _, err := m2.Append(ctx, s2.ID, "u2", foundation.Message{Role: foundation.RoleUser, Content: "hi"}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond) // 远超 TTL(100ms) + renewWindow(10ms)

	newSess, err := m2.GetOrCreate(ctx, s2.ID, "u2")
	if err != nil {
		t.Fatalf("GetOrCreate (beyond window) failed: %v", err)
	}
	if newSess.ID == s2.ID {
		t.Fatal("expected a new session ID when beyond renew window, got the old one")
	}
	if replacedFrom, ok := newSess.MetaData["replaced_from"]; !ok || replacedFrom != s2.ID {
		t.Fatalf("expected MetaData.replaced_from=%s, got %v", s2.ID, replacedFrom)
	}
}

func TestStateTerminal(t *testing.T) {
	m := NewManager(NewInMemoryRepository(), WithTTL(time.Hour))
	defer m.Close()
	ctx := context.Background()

	s, _ := m.Create(ctx, "", "u1")
	if err := m.Complete(ctx, s.ID, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Append(ctx, s.ID, "u1", foundation.Message{Role: foundation.RoleUser, Content: "x"}); err != ErrGone {
		t.Fatalf("expected ErrGone, got %v", err)
	}
}

func TestGetOrCreate(t *testing.T) {
	m := NewManager(NewInMemoryRepository(), WithTTL(time.Hour))
	defer m.Close()
	ctx := context.Background()

	// 空 id => 新建
	s1, err := m.GetOrCreate(ctx, "", "u1")
	if err != nil || s1.ID == "" {
		t.Fatalf("GetOrCreate new failed: %v", err)
	}
	// 已有 id => 复用
	s2, err := m.GetOrCreate(ctx, s1.ID, "u1")
	if err != nil || s2.ID != s1.ID {
		t.Fatalf("GetOrCreate reuse failed: %v", err)
	}
}

func TestGC(t *testing.T) {
	repo := NewInMemoryRepository()
	m := NewManager(repo, WithTTL(100*time.Millisecond))
	defer m.Close()

	ctx := context.Background()
	_, _ = m.Create(ctx, "", "u1")

	time.Sleep(200 * time.Millisecond)
	// 手动触发一次 GC
	m.runGC()

	// 过期会话应该被置成 EXPIRED
	list, _ := repo.ListByUser(ctx, "u1", 10)
	if len(list) == 0 {
		t.Fatalf("session list is empty")
	}
	if list[0].State != StateExpired {
		t.Fatalf("expected EXPIRED, got %s", list[0].State)
	}
}

func TestUpdateTokenUsage_PersistsToSession(t *testing.T) {
	m := NewManager(NewInMemoryRepository(), WithTTL(time.Hour))
	defer m.Close()

	ctx := context.Background()
	s, err := m.Create(ctx, "", "u1")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := m.UpdateTokenUsage(ctx, s.ID, "u1", 100, 50, 150); err != nil {
		t.Fatalf("UpdateTokenUsage failed: %v", err)
	}

	got, err := m.Get(ctx, s.ID, "u1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.TokenUsage.Prompt != 100 {
		t.Errorf("Prompt: got %d, want 100", got.TokenUsage.Prompt)
	}
	if got.TokenUsage.Completion != 50 {
		t.Errorf("Completion: got %d, want 50", got.TokenUsage.Completion)
	}
	if got.TokenUsage.Total != 150 {
		t.Errorf("Total: got %d, want 150", got.TokenUsage.Total)
	}
}

func TestUpdateTokenUsage_WrongUser(t *testing.T) {
	m := NewManager(NewInMemoryRepository(), WithTTL(time.Hour))
	defer m.Close()

	ctx := context.Background()
	s, _ := m.Create(ctx, "", "u1")

	err := m.UpdateTokenUsage(ctx, s.ID, "other", 100, 50, 150)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateTokenUsage_EmptyID(t *testing.T) {
	m := NewManager(NewInMemoryRepository(), WithTTL(time.Hour))
	defer m.Close()

	err := m.UpdateTokenUsage(context.Background(), "", "u1", 100, 50, 150)
	if err != ErrInvalid {
		t.Fatalf("expected ErrInvalid, got %v", err)
	}
}
