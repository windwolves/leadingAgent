package memory

import (
	"context"
	"errors"
)

var (
	ErrNotFound  = errors.New("memory not found")
	ErrDuplicate = errors.New("memory already exists")
	ErrInvalid   = errors.New("invalid memory item")
)

// Provider 记忆存储抽象接口，支持未来切换不同后端。
type Provider interface {
	Create(ctx context.Context, item *MemoryItem) error
	Get(ctx context.Context, id, userID string) (*MemoryItem, error)
	Update(ctx context.Context, item *MemoryItem) error
	Delete(ctx context.Context, id, userID string) error
	GetByHash(ctx context.Context, userID, hash string) (*MemoryItem, error)
	Search(ctx context.Context, userID, query string, limit int) ([]*MemoryItem, error)
	ListByUser(ctx context.Context, userID string, limit, offset int) ([]*MemoryItem, error)
}
