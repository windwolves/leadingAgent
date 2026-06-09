package session

import (
	"context"
	"sync"
	"time"
)

// InMemoryRepository 纯内存实现，单机/测试用。
type InMemoryRepository struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{sessions: make(map[string]*Session)}
}

func (r *InMemoryRepository) Create(_ context.Context, s *Session) error {
	if s == nil || s.ID == "" {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[s.ID]; ok {
		return ErrConflict
	}
	cp := *s
	r.sessions[s.ID] = &cp
	return nil
}

func (r *InMemoryRepository) Get(_ context.Context, id string) (*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *InMemoryRepository) Update(_ context.Context, s *Session) error {
	if s == nil || s.ID == "" {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.sessions[s.ID]
	if !ok {
		return ErrNotFound
	}
	if existing.Version != s.Version-1 && existing.Version != s.Version {
		// 调用方在 AppendMessage / Touch 里会 ++Version，
		// 所以正常写入时 s.Version == existing.Version + 1；
		// 放宽到 == existing.Version 是为"非版本递增式覆盖"场景兜底。
		return ErrConflict
	}
	cp := *s
	r.sessions[s.ID] = &cp
	return nil
}

func (r *InMemoryRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[id]; !ok {
		return ErrNotFound
	}
	delete(r.sessions, id)
	return nil
}

func (r *InMemoryRepository) ListByUser(_ context.Context, userID string, limit int) ([]*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		if s.UserID == userID {
			cp := *s
			out = append(out, &cp)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *InMemoryRepository) ListExpired(_ context.Context, before time.Time, limit int) ([]*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Session, 0)
	for _, s := range r.sessions {
		if s.ExpiresAt.Before(before) && !s.State.IsTerminal() {
			cp := *s
			out = append(out, &cp)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (r *InMemoryRepository) Touch(_ context.Context, id string, newExpiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return ErrNotFound
	}
	s.ExpiresAt = newExpiresAt
	s.UpdatedAt = time.Now().UTC()
	s.Version++
	return nil
}
