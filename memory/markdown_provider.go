package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// frontMatter 对应 Markdown 文件的 YAML front matter。
type frontMatter struct {
	ID        string    `yaml:"id"`
	UserID    string    `yaml:"user_id"`
	Category  string    `yaml:"category"`
	Tags      []string  `yaml:"tags"`
	Score     float64   `yaml:"score"`
	Hash      string    `yaml:"hash"`
	CreatedAt time.Time `yaml:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at"`
}

// MarkdownProvider 基于 Markdown 文件的记忆存储实现。
type MarkdownProvider struct {
	root  string
	index *Index
	mu    sync.RWMutex
}

// NewMarkdownProvider 创建 Markdown Provider。
// root 为 data/memories/ 目录路径。
func NewMarkdownProvider(root string) (*MarkdownProvider, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create memories root: %w", err)
	}

	p := &MarkdownProvider{
		root:  root,
		index: NewIndex(),
	}

	// 尝试从 index.json 加载，失败则从文件重建
	indexPath := filepath.Join(root, "index.json")
	if err := p.index.LoadFromFile(indexPath); err != nil {
		log.Printf("[MarkdownProvider] index.json not found, rebuilding from files...")
		if err := p.rebuildIndex(); err != nil {
			return nil, fmt.Errorf("rebuild index: %w", err)
		}
	}

	return p, nil
}

// rebuildIndex 扫描所有 .md 文件重新构建内存索引。
func (p *MarkdownProvider) rebuildIndex() error {
	pattern := filepath.Join(p.root, "user-*", "*", "*.md")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	for _, fp := range matches {
		fm, _, err := p.parseFile(fp)
		if err != nil {
			log.Printf("[MarkdownProvider] skip %s: %v", fp, err)
			continue
		}

		rel, _ := filepath.Rel(p.root, fp)
		p.index.Upsert(&IndexEntry{
			ID:        fm.ID,
			UserID:    fm.UserID,
			Category:  fm.Category,
			Tags:      fm.Tags,
			Score:     fm.Score,
			Hash:      fm.Hash,
			Title:     extractTitle(fp),
			FilePath:  rel,
			UpdatedAt: fm.UpdatedAt,
		})
	}

	log.Printf("[MarkdownProvider] index rebuilt: %d entries", len(matches))
	return p.flushIndex()
}

// parseFile 解析 .md 文件的 YAML front matter 和 body。
func (p *MarkdownProvider) parseFile(path string) (*frontMatter, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}

	content := string(data)
	// 提取 YAML front matter
	re := regexp.MustCompile(`^---\s*\n([\s\S]*?)\n---\s*\n([\s\S]*)$`)
	matches := re.FindStringSubmatch(content)
	if len(matches) != 3 {
		return nil, "", fmt.Errorf("no valid front matter in %s", path)
	}

	var fm frontMatter
	if err := yaml.Unmarshal([]byte(matches[1]), &fm); err != nil {
		return nil, "", fmt.Errorf("invalid front matter in %s: %w", path, err)
	}
	return &fm, strings.TrimSpace(matches[2]), nil
}

// toMemoryItem 将 front matter + body 转为 MemoryItem。
func (p *MarkdownProvider) toMemoryItem(fm *frontMatter, body string) *MemoryItem {
	tags := make(map[string]string)
	tags["category"] = fm.Category
	for _, t := range fm.Tags {
		tags[t] = t
	}

	return &MemoryItem{
		ID:        fm.ID,
		Memory:    body,
		UserID:    fm.UserID,
		Hash:      fm.Hash,
		Metadata:  tags,
		Score:     float32(fm.Score),
		CreatedAt: fm.CreatedAt,
		UpdatedAt: fm.UpdatedAt,
	}
}

func (p *MarkdownProvider) filePath(userID, category, id, title string) string {
	// 文件名: YYYY-MM-DD-slug.md
	// 返回相对路径（相对 p.root），由调用方拼接成绝对路径
	slug := slugify(title)
	date := time.Now().Format("2006-01-02")
	filename := fmt.Sprintf("%s-%s.md", date, slug)
	return filepath.Join("user-"+userID, category, filename)
}

// flushIndex 异步写 index.json。
func (p *MarkdownProvider) flushIndex() error {
	indexPath := filepath.Join(p.root, "index.json")
	return p.index.SaveToFile(indexPath)
}

// ---------- Provider 实现 ----------

func (p *MarkdownProvider) Create(_ context.Context, item *MemoryItem) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if item == nil || item.UserID == "" {
		return ErrInvalid
	}

	// 生成 ID（如果未提供）
	if item.ID == "" {
		item.ID = newID()
	}

	// 检查去重
	existing := p.index.GetByHash(item.UserID, item.Hash)
	if existing != nil {
		return ErrDuplicate
	}

	category := "facts"
	if cat, ok := item.Metadata["category"]; ok && cat != "" {
		category = cat
	}

	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now

	// 写入 .md 文件
	rel := p.filePath(item.UserID, category, item.ID, item.Memory)
	abs := filepath.Join(p.root, rel)

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	fm := frontMatter{
		ID:        item.ID,
		UserID:    item.UserID,
		Category:  category,
		Tags:      metadataKeys(item.Metadata),
		Score:     float64(item.Score),
		Hash:      item.Hash,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}

	if err := p.writeFile(abs, &fm, item.Memory); err != nil {
		return err
	}

	// 更新内存索引
	p.index.Upsert(&IndexEntry{
		ID:        item.ID,
		UserID:    item.UserID,
		Category:  category,
		Tags:      fm.Tags,
		Score:     fm.Score,
		Hash:      item.Hash,
		Title:     extractTitleFromMemory(item.Memory),
		FilePath:  rel,
		UpdatedAt: item.UpdatedAt,
	})

	go func() { _ = p.flushIndex() }()
	return nil
}

func (p *MarkdownProvider) Get(_ context.Context, id, userID string) (*MemoryItem, error) {
	p.mu.RLock()
	entry := p.index.GetByID(id, userID)
	p.mu.RUnlock()

	if entry == nil {
		return nil, ErrNotFound
	}

	abs := filepath.Join(p.root, entry.FilePath)
	fm, body, err := p.parseFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return p.toMemoryItem(fm, body), nil
}

func (p *MarkdownProvider) Update(_ context.Context, item *MemoryItem) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if item == nil || item.ID == "" {
		return ErrInvalid
	}

	entry := p.index.GetByID(item.ID, item.UserID)
	if entry == nil {
		return ErrNotFound
	}

	category := "facts"
	if cat, ok := item.Metadata["category"]; ok && cat != "" {
		category = cat
	}

	item.UpdatedAt = time.Now().UTC()

	fm := frontMatter{
		ID:        item.ID,
		UserID:    item.UserID,
		Category:  category,
		Tags:      metadataKeys(item.Metadata),
		Score:     float64(item.Score),
		Hash:      item.Hash,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}

	abs := filepath.Join(p.root, entry.FilePath)
	if err := p.writeFile(abs, &fm, item.Memory); err != nil {
		return err
	}

	// 更新索引
	entry2 := *entry
	entry2.Score = fm.Score
	entry2.UpdatedAt = item.UpdatedAt
	entry2.Title = extractTitleFromMemory(item.Memory)
	p.index.Upsert(&entry2)

	go func() { _ = p.flushIndex() }()
	return nil
}

func (p *MarkdownProvider) Delete(_ context.Context, id, userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	entry := p.index.GetByID(id, userID)
	if entry == nil {
		return ErrNotFound
	}

	abs := filepath.Join(p.root, entry.FilePath)
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}

	p.index.Remove(id, userID)
	go func() { _ = p.flushIndex() }()
	return nil
}

func (p *MarkdownProvider) GetByHash(_ context.Context, userID, hash string) (*MemoryItem, error) {
	p.mu.RLock()
	entry := p.index.GetByHash(userID, hash)
	p.mu.RUnlock()

	if entry == nil {
		return nil, ErrNotFound
	}

	abs := filepath.Join(p.root, entry.FilePath)
	fm, body, err := p.parseFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	return p.toMemoryItem(fm, body), nil
}

func (p *MarkdownProvider) Search(ctx context.Context, userID, query string, limit int) ([]*MemoryItem, error) {
	p.mu.RLock()
	entries := p.index.Search(userID, query, limit)
	p.mu.RUnlock()

	// 如果关键词没命中，fallback 到按更新时间倒序返回前 N 条
	// 少量记忆的场景下，确保至少能找到一些
	if len(entries) == 0 {
		p.mu.RLock()
		entries = p.index.ListByUser(userID, limit, 0)
		p.mu.RUnlock()
	}

	items := make([]*MemoryItem, 0, len(entries))
	for _, entry := range entries {
		abs := filepath.Join(p.root, entry.FilePath)
		fm, body, err := p.parseFile(abs)
		if err != nil {
			log.Printf("[MarkdownProvider] skip %s: %v", abs, err)
			continue
		}
		items = append(items, p.toMemoryItem(fm, body))
	}
	return items, nil
}

func (p *MarkdownProvider) ListByUser(ctx context.Context, userID string, limit, offset int) ([]*MemoryItem, error) {
	p.mu.RLock()
	entries := p.index.ListByUser(userID, limit, offset)
	p.mu.RUnlock()

	items := make([]*MemoryItem, 0, len(entries))
	for _, entry := range entries {
		abs := filepath.Join(p.root, entry.FilePath)
		fm, body, err := p.parseFile(abs)
		if err != nil {
			continue
		}
		items = append(items, p.toMemoryItem(fm, body))
	}
	return items, nil
}

// ---------- 辅助函数 ----------

func (p *MarkdownProvider) writeFile(path string, fm *frontMatter, body string) error {
	fmYaml, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("marshal front matter: %w", err)
	}

	content := fmt.Sprintf("---\n%s---\n\n# %s\n\n%s\n", string(fmYaml), extractTitleFromMemory(body), body)
	return os.WriteFile(path, []byte(content), 0o644)
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ComputeHash 计算 category + content 的 SHA256 前 12 位。
func ComputeHash(category, content string) string {
	h := sha256.Sum256([]byte(category + "\x00" + content))
	return hex.EncodeToString(h[:])[:12]
}

func slugify(s string) string {
	// 简单 slug：取前 5 个英文单词，转小写，连字符分隔
	words := strings.Fields(strings.ToLower(s))
	if len(words) > 5 {
		words = words[:5]
	}
	return strings.Join(words, "-")
}

func metadataKeys(m map[string]string) []string {
	seen := make(map[string]bool)
	var keys []string
	for k := range m {
		if k == "category" {
			continue
		}
		if !seen[k] {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	return keys
}

func extractTitle(path string) string {
	// 从文件路径提取标题（fallback）
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".md")
	// 去掉日期前缀 YYYY-MM-DD-
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`)
	return re.ReplaceAllString(base, "")
}

func extractTitleFromMemory(memory string) string {
	// 取第一行作为标题
	lines := strings.SplitN(memory, "\n", 2)
	title := strings.TrimSpace(lines[0])
	if len(title) > 80 {
		title = title[:80]
	}
	return title
}
