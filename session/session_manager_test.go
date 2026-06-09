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
	m := NewManager(NewInMemoryRepository(), WithTTL(100*time.Millisecond))
	defer m.Close()
	ctx := context.Background()

	s, err := m.Create(ctx, "", "u1")
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(150 * time.Millisecond)
	if _, err := m.Get(ctx, s.ID, "u1"); err != ErrExpired {
		t.Fatalf("expected ErrExpired, got %v", err)
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
