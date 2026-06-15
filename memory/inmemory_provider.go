package memory

import (
	"context"
	"sync"
)

// InMemoryProvider 基于内存的 Provider 实现，仅用于单元测试。
type InMemoryProvider struct {
	mu   sync.RWMutex
	data map[string]*MemoryItem // key: id
}

// NewInMemoryProvider 创建内存 Provider。
func NewInMemoryProvider() *InMemoryProvider {
	return &InMemoryProvider{
		data: make(map[string]*MemoryItem),
	}
}

func (p *InMemoryProvider) Create(_ context.Context, item *MemoryItem) error {
	if item == nil || item.ID == "" {
		return ErrInvalid
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.data[item.ID]; ok {
		return ErrDuplicate
	}
	clone := *item
	p.data[item.ID] = &clone
	return nil
}

func (p *InMemoryProvider) Get(_ context.Context, id, userID string) (*MemoryItem, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	item, ok := p.data[id]
	if !ok || item.UserID != userID {
		return nil, ErrNotFound
	}
	clone := *item
	return &clone, nil
}

func (p *InMemoryProvider) Update(_ context.Context, item *MemoryItem) error {
	if item == nil || item.ID == "" {
		return ErrInvalid
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.data[item.ID]; !ok {
		return ErrNotFound
	}
	clone := *item
	p.data[item.ID] = &clone
	return nil
}

func (p *InMemoryProvider) Delete(_ context.Context, id, userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	item, ok := p.data[id]
	if !ok || item.UserID != userID {
		return ErrNotFound
	}
	delete(p.data, id)
	return nil
}

func (p *InMemoryProvider) GetByHash(_ context.Context, userID, hash string) (*MemoryItem, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, item := range p.data {
		if item.UserID == userID && item.Hash == hash {
			clone := *item
			return &clone, nil
		}
	}
	return nil, ErrNotFound
}

func (p *InMemoryProvider) Search(_ context.Context, userID, query string, limit int) ([]*MemoryItem, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []*MemoryItem
	for _, item := range p.data {
		if item.UserID == userID {
			clone := *item
			result = append(result, &clone)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (p *InMemoryProvider) ListByUser(_ context.Context, userID string, limit, offset int) ([]*MemoryItem, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var result []*MemoryItem
	for _, item := range p.data {
		if item.UserID == userID {
			clone := *item
			result = append(result, &clone)
		}
	}
	if offset >= len(result) {
		return nil, nil
	}
	result = result[offset:]
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result, nil
}
