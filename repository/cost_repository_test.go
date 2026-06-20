package repository

import (
	"os"
	"testing"
	"time"

	"leadingAgent/models"
)

func newTestRepo(t *testing.T) CostRepository {
	t.Helper()
	f, err := os.CreateTemp("", "cost_test_*.db")
	if err != nil {
		t.Fatalf("failed to create temp db: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	repo, err := NewCostRepository(f.Name())
	if err != nil {
		t.Fatalf("NewCostRepository failed: %v", err)
	}
	t.Cleanup(func() { repo.Close() })
	return repo
}

func TestSave_AndGetAll_WithSessionID(t *testing.T) {
	repo := newTestRepo(t)

	cost := &models.TokenCost{
		SessionID:        "session-abc",
		RequestID:        "req-1",
		Provider:         "deepseek",
		Model:            "deepseek-chat",
		RequestType:      "callModel",
		Endpoint:         "",
		PromptTokens:     120,
		CompletionTokens: 80,
		TotalTokens:      200,
		CreatedAt:        time.Now(),
	}

	if err := repo.Save(cost); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	all, err := repo.GetAll()
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 record, got %d", len(all))
	}

	got := all[0]
	if got.SessionID != "session-abc" {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, "session-abc")
	}
	if got.PromptTokens != 120 {
		t.Errorf("PromptTokens: got %d, want 120", got.PromptTokens)
	}
	if got.CompletionTokens != 80 {
		t.Errorf("CompletionTokens: got %d, want 80", got.CompletionTokens)
	}
	if got.TotalTokens != 200 {
		t.Errorf("TotalTokens: got %d, want 200", got.TotalTokens)
	}
}

func TestSave_EmptySessionID_Allowed(t *testing.T) {
	repo := newTestRepo(t)

	cost := &models.TokenCost{
		SessionID:        "",
		RequestID:        "req-2",
		Provider:         "deepseek",
		Model:            "deepseek-chat",
		RequestType:      "chat",
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		CreatedAt:        time.Now(),
	}

	if err := repo.Save(cost); err != nil {
		t.Fatalf("Save with empty SessionID failed: %v", err)
	}

	all, _ := repo.GetAll()
	if all[0].SessionID != "" {
		t.Errorf("expected empty SessionID, got %q", all[0].SessionID)
	}
}

func TestGetTotalTokens_SumsAll(t *testing.T) {
	repo := newTestRepo(t)

	for i, total := range []int{100, 200, 300} {
		_ = repo.Save(&models.TokenCost{
			RequestID:   "req-" + string(rune('a'+i)),
			Provider:    "deepseek",
			Model:       "deepseek-chat",
			RequestType: "callModel",
			TotalTokens: total,
			CreatedAt:   time.Now(),
		})
	}

	got, err := repo.GetTotalTokens()
	if err != nil {
		t.Fatalf("GetTotalTokens failed: %v", err)
	}
	if got != 600 {
		t.Errorf("GetTotalTokens: got %d, want 600", got)
	}
}

func TestSchemaIdempotent_AlterColumnTwice(t *testing.T) {
	f, err := os.CreateTemp("", "cost_idempotent_*.db")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	f.Close()
	defer os.Remove(f.Name())

	// Open twice — second open triggers ALTER TABLE which must not fail even
	// if session_id column already exists.
	r1, err := NewCostRepository(f.Name())
	if err != nil {
		t.Fatalf("first open failed: %v", err)
	}
	r1.Close()

	r2, err := NewCostRepository(f.Name())
	if err != nil {
		t.Fatalf("second open failed (schema migration not idempotent): %v", err)
	}
	r2.Close()
}
