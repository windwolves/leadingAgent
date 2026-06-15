package services

import (
	"context"
	"fmt"
	"log"
	"strings"

	"leadingAgent/memory"
)

// MemoryService 封装长期记忆的召回与写入业务逻辑。
type MemoryService struct {
	provider memory.Provider
	logger   *log.Logger
}

// NewMemoryService 创建 MemoryService。
func NewMemoryService(provider memory.Provider) *MemoryService {
	return &MemoryService{
		provider: provider,
		logger:   log.Default(),
	}
}

// Recall 根据用户当前输入召回最相关的 long-term memories，格式化为文本。
// 返回空字符串表示无相关记忆。
func (s *MemoryService) Recall(ctx context.Context, userID, query string, limit int) string {
	items, err := s.provider.Search(ctx, userID, query, limit)
	if err != nil {
		s.logger.Printf("[MemoryService] recall error: %v", err)
		return ""
	}
	if len(items) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## User Memories\n")
	for _, item := range items {
		sb.WriteString(fmt.Sprintf("- %s\n", item.Memory))
	}
	return sb.String()
}

// Remember 写入一条长期记忆。
// category 如 "personal"、"preferences"、"technical"。
func (s *MemoryService) Remember(ctx context.Context, userID, category, content string) (string, error) {
	if userID == "" || content == "" {
		return "", fmt.Errorf("userID and content are required")
	}

	hash := memory.ComputeHash(category, content)

	// 去重：检查是否已存在
	existing, err := s.provider.GetByHash(ctx, userID, hash)
	if err == nil && existing != nil {
		// 强化已有记忆
		newScore := existing.Score + 0.05
		if newScore > 1.0 {
			newScore = 1.0
		}
		existing.Score = newScore
		if err := s.provider.Update(ctx, existing); err != nil {
			return "", fmt.Errorf("reinforce memory: %w", err)
		}
		s.logger.Printf("[MemoryService] reinforced: %s (score=%.2f)", existing.ID, newScore)
		return existing.ID, nil
	}

	// 新建记忆
	item := &memory.MemoryItem{
		Memory:   content,
		UserID:   userID,
		Hash:     hash,
		Score:    0.5,
		Metadata: map[string]string{"category": category},
	}
	if err := s.provider.Create(ctx, item); err != nil {
		return "", fmt.Errorf("create memory: %w", err)
	}

	s.logger.Printf("[MemoryService] saved: %s (category=%s)", item.ID, category)
	return item.ID, nil
}
