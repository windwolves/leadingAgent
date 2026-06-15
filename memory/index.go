package memory

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// IndexEntry 内存索引条目，从 Markdown front matter 解析得到。
type IndexEntry struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Category  string    `json:"category"`
	Tags      []string  `json:"tags"`
	Score     float64   `json:"score"`
	Hash      string    `json:"hash"`
	Title     string    `json:"title"`
	FilePath  string    `json:"file_path"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Index 内存索引，支持按 ID / Hash / User 快速查询。
type Index struct {
	mu     sync.RWMutex
	byUser map[string][]*IndexEntry
	byID   map[string]*IndexEntry
	byHash map[string]*IndexEntry // key: "userID:hash"
}

// NewIndex 创建空索引。
func NewIndex() *Index {
	return &Index{
		byUser: make(map[string][]*IndexEntry),
		byID:   make(map[string]*IndexEntry),
		byHash: make(map[string]*IndexEntry),
	}
}

// Upsert 添加或更新索引条目。
func (idx *Index) Upsert(e *IndexEntry) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// 如果已存在同 ID，先删除旧条目
	if old, ok := idx.byID[e.ID]; ok {
		idx.removeLocked(old)
	}

	idx.byID[e.ID] = e
	idx.byHash[e.UserID+":"+e.Hash] = e
	idx.byUser[e.UserID] = append(idx.byUser[e.UserID], e)
}

// Remove 删除索引条目。
func (idx *Index) Remove(id, userID string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	e, ok := idx.byID[id]
	if !ok || e.UserID != userID {
		return
	}
	idx.removeLocked(e)
}

func (idx *Index) removeLocked(e *IndexEntry) {
	delete(idx.byID, e.ID)
	delete(idx.byHash, e.UserID+":"+e.Hash)

	entries := idx.byUser[e.UserID]
	for i, entry := range entries {
		if entry.ID == e.ID {
			idx.byUser[e.UserID] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
}

// GetByID 按 ID 查询。
func (idx *Index) GetByID(id, userID string) *IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	e, ok := idx.byID[id]
	if !ok || e.UserID != userID {
		return nil
	}
	return e
}

// GetByHash 按 userID + hash 查询，用于去重。
func (idx *Index) GetByHash(userID, hash string) *IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.byHash[userID+":"+hash]
}

// ListByUser 按用户列出记忆，按更新时间倒序。
func (idx *Index) ListByUser(userID string, limit, offset int) []*IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	entries := idx.byUser[userID]
	if offset >= len(entries) {
		return nil
	}

	end := offset + limit
	if end > len(entries) || limit <= 0 {
		end = len(entries)
	}

	result := make([]*IndexEntry, end-offset)
	copy(result, entries[offset:end])

	// 按 UpdatedAt 倒序排序
	sortEntriesDesc(result)
	return result
}

// Search 关键词搜索，在 Title 中匹配。
func (idx *Index) Search(userID, query string, limit int) []*IndexEntry {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	query = strings.ToLower(query)
	var matched []*IndexEntry
	for _, e := range idx.byUser[userID] {
		if strings.Contains(strings.ToLower(e.Title), query) {
			matched = append(matched, e)
		}
	}

	sortEntriesDesc(matched)
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched
}

func sortEntriesDesc(entries []*IndexEntry) {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].UpdatedAt.Before(entries[j].UpdatedAt) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}

// LoadFromFile 从 index.json 加载索引快照。
func (idx *Index) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var entries []*IndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.byID = make(map[string]*IndexEntry, len(entries))
	idx.byHash = make(map[string]*IndexEntry, len(entries))
	idx.byUser = make(map[string][]*IndexEntry)

	for _, e := range entries {
		idx.byID[e.ID] = e
		idx.byHash[e.UserID+":"+e.Hash] = e
		idx.byUser[e.UserID] = append(idx.byUser[e.UserID], e)
	}
	return nil
}

// SaveToFile 将当前索引导出为 index.json。
func (idx *Index) SaveToFile(path string) error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	entries := make([]*IndexEntry, 0, len(idx.byID))
	for _, e := range idx.byID {
		entries = append(entries, e)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
