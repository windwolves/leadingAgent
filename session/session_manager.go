package session

import (
	"context"
	"leadingAgent/agent/foundation"
	"sync"
	"time"
)

// Manager 统一暴露的会话管理接口。
// 一个 Manager 实例可以在多 goroutine 中安全使用。
type Manager struct {
	repo         Repository
	ttl          time.Duration
	renewWindow  time.Duration
	maxPerUser   int
	maxMessages  int
	systemPrompt string

	// per-session 串行化写锁，避免同会话并发写入互相覆盖。
	locksMu sync.Mutex
	locks   map[string]*sync.Mutex

	stopCh chan struct{}
	once   sync.Once
}

type ManagerOption func(*Manager)

func WithTTL(ttl time.Duration) ManagerOption       { return func(m *Manager) { m.ttl = ttl } }
func WithRenewWindow(d time.Duration) ManagerOption { return func(m *Manager) { m.renewWindow = d } }
func WithMaxPerUser(n int) ManagerOption            { return func(m *Manager) { m.maxPerUser = n } }
func WithMaxMessages(n int) ManagerOption           { return func(m *Manager) { m.maxMessages = n } }
func WithSystemPrompt(p string) ManagerOption       { return func(m *Manager) { m.systemPrompt = p } }

// NewManager 创建 Manager。默认 TTL=1h，续期窗口=7 天，后台每 60s 回收过期会话。
func NewManager(repo Repository, opts ...ManagerOption) *Manager {
	m := &Manager{
		repo:         repo,
		ttl:          time.Hour,
		renewWindow:  7 * 24 * time.Hour,
		maxPerUser:   100,
		maxMessages:  200,
		systemPrompt: "You are a helpful assistant. When you learn personal facts about the user (name, preferences, technical background, etc.), call the remember_fact tool to save them for future conversations. Always include the userId parameter.",
		locks:        make(map[string]*sync.Mutex),
		stopCh:       make(chan struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}

	// 后台 GC：每 60s 检查一次过期会话。
	go m.gcLoop(60 * time.Second)
	return m
}

func (m *Manager) Close() error {
	m.once.Do(func() { close(m.stopCh) })
	return nil
}

// ---------------------------------------------------------------------------
// 生命周期 API
// ---------------------------------------------------------------------------

// Create 新建一个会话并立即落盘。若 id 非空，则使用该 id（失败返回 ErrConflict）；
// 否则内部生成唯一 id。
func (m *Manager) Create(ctx context.Context, id, userID string) (*Session, error) {
	if userID == "" {
		userID = "anon"
	}
	if id == "" {
		id = NewID()
	}

	// 软限制：单用户最多保留 N 条 session，超过则最旧的自动结束。
	if m.maxPerUser > 0 {
		existing, _ := m.repo.ListByUser(ctx, userID, m.maxPerUser+1)
		if len(existing) >= m.maxPerUser {
			// 按 UpdatedAt 升序，最旧的先置为 COMPLETED
			sortByUpdatedAtAsc(existing)
			for i := 0; i <= len(existing)-m.maxPerUser; i++ {
				old := existing[i]
				if !old.State.IsTerminal() {
					old.State = StateCompleted
					_ = m.repo.Update(ctx, old)
				}
			}
		}
	}

	now := time.Now().UTC()
	s := &Session{
		ID:           id,
		UserID:       userID,
		State:        StateCreated,
		SystemPrompt: m.systemPrompt,
		Messages:     nil,
		ToolCalls:    nil,
		ModelConfig:  ModelConfig{},
		MetaData:     map[string]interface{}{},
		MaxMessages:  m.maxMessages,
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpiresAt:    now.Add(m.ttl),
		Version:      1,
	}
	if err := m.repo.Create(ctx, s); err != nil {
		return nil, err
	}
	return s, nil
}

// Get 获取会话（只读副本）。只做 userID 水平越权校验，不拒绝过期会话。
// 过期/终态的拒绝放在写操作（Append）中。
func (m *Manager) Get(ctx context.Context, id, userID string) (*Session, error) {
	if id == "" {
		return nil, ErrInvalid
	}
	s, err := m.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if userID != "" && s.UserID != userID {
		// 水平越权防护：不匹配 userID 则当作不存在。
		return nil, ErrNotFound
	}
	return s, nil
}

// Renew 刷新会话的过期时间为 now + ttl，并将状态恢复为 ACTIVE。
// 可续期窗口：过期了 renewWindow 之内 或 renewWindow 内即将过期。
// 超过窗口的会话不可续期，返回 ErrGone，由上层决定是否创建新 session 并生成摘要。
func (m *Manager) Renew(ctx context.Context, id, userID string) (*Session, error) {
	if id == "" {
		return nil, ErrInvalid
	}
	mu := m.lockFor(id)
	mu.Lock()
	defer mu.Unlock()

	s, err := m.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if userID != "" && s.UserID != userID {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	diff := now.Sub(s.ExpiresAt) // > 0 表示已过期，< 0 表示未过期
	hasMessages := len(s.Messages) > 0

	// 拒绝条件：已过期超过 renewWindow 且 有内容 → 太久远，不可续期
	// 其他情况均放行：未过期 / 在窗口内过期 / 即将过期 / 空会话
	if hasMessages && diff > 0 && diff >= m.renewWindow {
		return nil, ErrGone
	}

	s.State = StateActive
	s.UpdatedAt = now
	s.ExpiresAt = now.Add(m.ttl)
	s.Version++
	if err := m.repo.Update(ctx, s); err != nil {
		return nil, err
	}
	return s, nil
}

// GetOrCreate 查现有会话或新建一个（id 为空时内部生成）。
// 改进逻辑：
//   - 若会话在 7 天可续期窗口内 → Renew 它，继续复用
//   - 若会话太老（> 7 天） → 创建新 session，Meta 中写入 replaced_from 供上层生成摘要
func (m *Manager) GetOrCreate(ctx context.Context, id, userID string) (*Session, error) {
	if id == "" {
		return m.Create(ctx, "", userID)
	}
	s, err := m.Get(ctx, id, userID)
	if err == nil {
		now := time.Now().UTC()
		needsRenew := s.State.IsTerminal() || s.ExpiresAt.Before(now)
		if !needsRenew {
			// 会话仍然有效，直接返回
			return s, nil
		}
		// 过期或已终态 → 尝试续期（7 天窗口内会成功）
		renewed, renewErr := m.Renew(ctx, id, userID)
		if renewErr == nil {
			return renewed, nil
		}
		// 不可续期 → 创建新 session，并把旧 session ID 写入 MetaData
		newSess, createErr := m.Create(ctx, "", userID)
		if createErr != nil {
			return nil, createErr
		}
		if len(s.Messages) > 0 {
			if newSess.MetaData == nil {
				newSess.MetaData = make(map[string]interface{})
			}
			newSess.MetaData["replaced_from"] = s.ID
		}
		return newSess, nil
	}
	return m.Create(ctx, "", userID)
}

// Append 在会话尾部追加消息，推进版本号，刷新过期时间，落盘。
// 会对同 session 串行化，内部带乐观锁重试（最多 3 次）。
func (m *Manager) Append(ctx context.Context, id, userID string, msgs ...foundation.Message) (*Session, error) {
	if id == "" || len(msgs) == 0 {
		return nil, ErrInvalid
	}

	mu := m.lockFor(id)
	mu.Lock()
	defer mu.Unlock()

	for attempt := 0; attempt < 3; attempt++ {
		s, err := m.repo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if userID != "" && s.UserID != userID {
			return nil, ErrNotFound
		}
		if s.State.IsTerminal() {
			return nil, ErrGone
		}
		if s.ExpiresAt.Before(time.Now().UTC()) {
			_ = m.markExpired(ctx, s)
			return nil, ErrExpired
		}
		if !s.IsWritable() {
			s.State = StateCompleted
			_ = m.repo.Update(ctx, s)
			return nil, ErrGone
		}

		s.State = StateActive
		s.Messages = append(s.Messages, msgs...)
		s.UpdatedAt = time.Now().UTC()
		s.ExpiresAt = s.UpdatedAt.Add(m.ttl)
		s.Version++

		err = m.repo.Update(ctx, s)
		if err == ErrConflict {
			time.Sleep(time.Millisecond * time.Duration(5*(attempt+1)))
			continue
		}
		if err != nil {
			return nil, err
		}
		return s, nil
	}
	return nil, ErrConflict
}

// Touch 仅刷新过期时间（心跳用）。
func (m *Manager) Touch(ctx context.Context, id, userID string) error {
	if id == "" {
		return ErrInvalid
	}
	s, err := m.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if userID != "" && s.UserID != userID {
		return ErrNotFound
	}
	if s.State.IsTerminal() {
		return ErrGone
	}
	return m.repo.Touch(ctx, id, time.Now().UTC().Add(m.ttl))
}

// Complete 将会话标记为完成。
func (m *Manager) Complete(ctx context.Context, id, userID string) error {
	return m.transitionState(ctx, id, userID, StateCompleted)
}

// Delete 硬删除会话。
func (m *Manager) Delete(ctx context.Context, id, userID string) error {
	if id == "" {
		return ErrInvalid
	}
	if s, err := m.repo.Get(ctx, id); err == nil {
		if userID != "" && s.UserID != userID {
			return ErrNotFound
		}
	}
	return m.repo.Delete(ctx, id)
}

// ListByUser 返回某用户的会话列表（最多 limit 条）。
func (m *Manager) ListByUser(ctx context.Context, userID string, limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = 50
	}
	return m.repo.ListByUser(ctx, userID, limit)
}

// RecordError 递增错误计数，超过阈值置 ERROR。
func (m *Manager) RecordError(ctx context.Context, id, userID string, threshold int) error {
	mu := m.lockFor(id)
	mu.Lock()
	defer mu.Unlock()

	s, err := m.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if userID != "" && s.UserID != userID {
		return ErrNotFound
	}
	s.ErrorCount++
	s.UpdatedAt = time.Now().UTC()
	s.Version++
	if threshold > 0 && s.ErrorCount >= threshold {
		s.State = StateError
	}
	return m.repo.Update(ctx, s)
}

func (m *Manager) transitionState(ctx context.Context, id, userID string, target SessionState) error {
	mu := m.lockFor(id)
	mu.Lock()
	defer mu.Unlock()

	s, err := m.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if userID != "" && s.UserID != userID {
		return ErrNotFound
	}
	s.State = target
	s.UpdatedAt = time.Now().UTC()
	s.Version++
	return m.repo.Update(ctx, s)
}

func (m *Manager) markExpired(ctx context.Context, s *Session) error {
	s.State = StateExpired
	s.UpdatedAt = time.Now().UTC()
	s.Version++
	return m.repo.Update(ctx, s)
}

// ---------------------------------------------------------------------------
// per-session 互斥锁懒加载池
// ---------------------------------------------------------------------------

func (m *Manager) lockFor(id string) *sync.Mutex {
	m.locksMu.Lock()
	defer m.locksMu.Unlock()
	if mu, ok := m.locks[id]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	m.locks[id] = mu
	return mu
}

// ---------------------------------------------------------------------------
// 后台 GC
// ---------------------------------------------------------------------------

func (m *Manager) gcLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.runGC()
		case <-m.stopCh:
			return
		}
	}
}

func (m *Manager) runGC() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	expired, err := m.repo.ListExpired(ctx, time.Now().UTC(), 500)
	if err != nil {
		return
	}
	for _, s := range expired {
		_ = m.markExpired(ctx, s)
	}

	// 简单回收 locks 池，避免无限增长
	m.locksMu.Lock()
	if n := len(m.locks); n > 2000 {
		m.locks = make(map[string]*sync.Mutex)
	}
	m.locksMu.Unlock()
}

// sortByUpdatedAtAsc 按 UpdatedAt 升序
func sortByUpdatedAtAsc(s []*Session) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].UpdatedAt.Before(s[j-1].UpdatedAt); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
