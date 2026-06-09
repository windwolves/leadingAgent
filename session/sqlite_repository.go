package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"leadingAgent/agent/foundation"

	_ "modernc.org/sqlite"
)

// SQLiteRepository 使用 modernc.org/sqlite 将会话落盘。
//
// 表结构策略：
//   - sessions 主表保存标量字段（id/user_id/state/system_prompt/时间戳/版本/error_count/max_messages）；
//   - 结构化可变字段（messages / tool_calls / model_config / meta / token_usage）以 JSON 列保存，
//     便于 Go 结构体 <-> DB 直接序列化，不需要做关联表的迁移与 JOIN 成本。
//   - 索引：user_id(查询用户会话列表)、expires_at + state(后台 GC)、updated_at(排序)。
type SQLiteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository 打开（或创建）dbPath 指定的 SQLite 文件。
// 传 ":memory:" 可得到内存数据库（用于测试或临时运行态）。
func NewSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	if dbPath == "" {
		return nil, ErrInvalid
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("session: open sqlite: %w", err)
	}
	// modernc 打开后仍可能在第一次 Exec 才报底层错，先 ping 一下。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("session: ping sqlite: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("session: migrate sqlite: %w", err)
	}
	return &SQLiteRepository{db: db}, nil
}

// Close 关闭底层数据库句柄。
func (r *SQLiteRepository) Close() error { return r.db.Close() }

// DB 暴露底层 *sql.DB，便于上层做集成或自行扩展查询。
func (r *SQLiteRepository) DB() *sql.DB { return r.db }

// -----------------------------------------------------------------------------
// schema 迁移
// -----------------------------------------------------------------------------

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id             TEXT PRIMARY KEY,
			user_id        TEXT NOT NULL,
			state          TEXT NOT NULL DEFAULT 'CREATED',
			system_prompt  TEXT NOT NULL DEFAULT '',
			messages       TEXT NOT NULL DEFAULT '[]',
			tool_calls     TEXT NOT NULL DEFAULT '[]',
			model_config   TEXT NOT NULL DEFAULT '{}',
			meta           TEXT NOT NULL DEFAULT '{}',
			token_usage    TEXT NOT NULL DEFAULT '{}',
			error_count    INTEGER NOT NULL DEFAULT 0,
			max_messages   INTEGER NOT NULL DEFAULT 0,
			created_at     DATETIME NOT NULL,
			updated_at     DATETIME NOT NULL,
			expires_at     DATETIME NOT NULL,
			version        INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_id    ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_state      ON sessions(state)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// 序列化工具
// -----------------------------------------------------------------------------

func toJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		// 不应发生，发生了返回一个可解析的空 JSON，避免上层全链路崩。
		switch v.(type) {
		case []foundation.Message, []ToolCallMeta:
			return "[]"
		}
		return "{}"
	}
	return string(b)
}

func fromJSON[T any](raw string, out *T) error {
	if raw == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), out)
}

// -----------------------------------------------------------------------------
// Create
// -----------------------------------------------------------------------------

func (r *SQLiteRepository) Create(ctx context.Context, s *Session) error {
	if s == nil || s.ID == "" {
		return ErrInvalid
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = s.CreatedAt
	}
	if s.ExpiresAt.IsZero() {
		s.ExpiresAt = s.CreatedAt.Add(time.Hour)
	}
	if s.State == "" {
		s.State = StateCreated
	}
	if s.Version == 0 {
		s.Version = 1
	}

	const q = `INSERT INTO sessions (
		id, user_id, state, system_prompt, messages, tool_calls,
		model_config, meta, token_usage, error_count, max_messages,
		created_at, updated_at, expires_at, version
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		s.ID,
		s.UserID,
		string(s.State),
		s.SystemPrompt,
		toJSON(s.Messages),
		toJSON(s.ToolCalls),
		toJSON(s.ModelConfig),
		toJSON(s.MetaData),
		toJSON(s.TokenUsage),
		s.ErrorCount,
		s.MaxMessages,
		s.CreatedAt.UTC().Format(time.RFC3339Nano),
		s.UpdatedAt.UTC().Format(time.RFC3339Nano),
		s.ExpiresAt.UTC().Format(time.RFC3339Nano),
		s.Version,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrConflict
		}
		return err
	}
	return nil
}

// -----------------------------------------------------------------------------
// Get
// -----------------------------------------------------------------------------

func (r *SQLiteRepository) Get(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, ErrInvalid
	}
	const q = `
		SELECT id, user_id, state, system_prompt, messages, tool_calls,
		       model_config, meta, token_usage, error_count, max_messages,
		       created_at, updated_at, expires_at, version
		FROM sessions WHERE id = ?
	`
	row := r.db.QueryRowContext(ctx, q, id)
	return scanSession(row.Scan)
}

// -----------------------------------------------------------------------------
// Update（乐观锁: 要求 s.Version == db.version + 1）
// -----------------------------------------------------------------------------

func (r *SQLiteRepository) Update(ctx context.Context, s *Session) error {
	if s == nil || s.ID == "" {
		return ErrInvalid
	}
	// 约定：调用方 AppendMessage/Touch 会 ++Version，写入时数据库内的 version 应为 s.Version-1。
	const q = `
		UPDATE sessions
		SET state         = ?,
		    system_prompt = ?,
		    messages      = ?,
		    tool_calls    = ?,
		    model_config  = ?,
		    meta          = ?,
		    token_usage   = ?,
		    error_count   = ?,
		    max_messages  = ?,
		    updated_at    = ?,
		    expires_at    = ?,
		    version       = ?
		WHERE id = ? AND version = ?
	`
	res, err := r.db.ExecContext(ctx, q,
		string(s.State),
		s.SystemPrompt,
		toJSON(s.Messages),
		toJSON(s.ToolCalls),
		toJSON(s.ModelConfig),
		toJSON(s.MetaData),
		toJSON(s.TokenUsage),
		s.ErrorCount,
		s.MaxMessages,
		s.UpdatedAt.UTC().Format(time.RFC3339Nano),
		s.ExpiresAt.UTC().Format(time.RFC3339Nano),
		s.Version,
		s.ID,
		s.Version-1,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// 无命中：要么会话不存在，要么版本冲突。
		if _, getErr := r.Get(ctx, s.ID); getErr != nil {
			if errors.Is(getErr, ErrNotFound) {
				return ErrNotFound
			}
			return getErr
		}
		return ErrConflict
	}
	return nil
}

// -----------------------------------------------------------------------------
// Delete
// -----------------------------------------------------------------------------

func (r *SQLiteRepository) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrInvalid
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// ListByUser
// -----------------------------------------------------------------------------

func (r *SQLiteRepository) ListByUser(ctx context.Context, userID string, limit int) ([]*Session, error) {
	if userID == "" {
		return nil, ErrInvalid
	}
	q := `
		SELECT id, user_id, state, system_prompt, messages, tool_calls,
		       model_config, meta, token_usage, error_count, max_messages,
		       created_at, updated_at, expires_at, version
		FROM sessions WHERE user_id = ?
		ORDER BY updated_at DESC
	`
	var args []interface{}
	args = append(args, userID)
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	return querySessions(ctx, r.db, q, args...)
}

// -----------------------------------------------------------------------------
// ListExpired
// -----------------------------------------------------------------------------

func (r *SQLiteRepository) ListExpired(ctx context.Context, before time.Time, limit int) ([]*Session, error) {
	// 与 InMemory 语义对齐：只挑出未到终态的过期会话，便于 GC 处理。
	q := `
		SELECT id, user_id, state, system_prompt, messages, tool_calls,
		       model_config, meta, token_usage, error_count, max_messages,
		       created_at, updated_at, expires_at, version
		FROM sessions
		WHERE expires_at < ?
		  AND state NOT IN ('COMPLETED','EXPIRED','ERROR','ARCHIVED')
		ORDER BY expires_at ASC
	`
	var args []interface{}
	args = append(args, before.UTC().Format(time.RFC3339Nano))
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	return querySessions(ctx, r.db, q, args...)
}

// -----------------------------------------------------------------------------
// Touch
// -----------------------------------------------------------------------------

func (r *SQLiteRepository) Touch(ctx context.Context, id string, newExpiresAt time.Time) error {
	if id == "" {
		return ErrInvalid
	}
	const q = `
		UPDATE sessions
		SET expires_at = ?, updated_at = ?, version = version + 1
		WHERE id = ?
	`
	res, err := r.db.ExecContext(ctx, q,
		newExpiresAt.UTC().Format(time.RFC3339Nano),
		time.Now().UTC().Format(time.RFC3339Nano),
		id,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// -----------------------------------------------------------------------------
// 内部扫描辅助
// -----------------------------------------------------------------------------

type scanFunc func(dest ...interface{}) error

func scanSession(scan scanFunc) (*Session, error) {
	var (
		s             Session
		state         string
		messagesStr   string
		toolCallsStr  string
		modelCfgStr   string
		metaStr       string
		tokenUsageStr string
		createdStr    string
		updatedStr    string
		expiresStr    string
	)
	err := scan(
		&s.ID, &s.UserID, &state, &s.SystemPrompt,
		&messagesStr, &toolCallsStr, &modelCfgStr, &metaStr, &tokenUsageStr,
		&s.ErrorCount, &s.MaxMessages,
		&createdStr, &updatedStr, &expiresStr, &s.Version,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.State = SessionState(state)
	if err := fromJSON(messagesStr, &s.Messages); err != nil {
		return nil, fmt.Errorf("session: parse messages: %w", err)
	}
	if err := fromJSON(toolCallsStr, &s.ToolCalls); err != nil {
		return nil, fmt.Errorf("session: parse tool_calls: %w", err)
	}
	if err := fromJSON(modelCfgStr, &s.ModelConfig); err != nil {
		return nil, fmt.Errorf("session: parse model_config: %w", err)
	}
	if err := fromJSON(metaStr, &s.MetaData); err != nil {
		// meta 允许空 map：解析失败置空即可，不阻断整条记录。
		s.MetaData = nil
	}
	if err := fromJSON(tokenUsageStr, &s.TokenUsage); err != nil {
		// 同上，失败安全。
		s.TokenUsage = TokenUsage{}
	}
	if s.CreatedAt, err = time.Parse(time.RFC3339Nano, createdStr); err != nil {
		return nil, fmt.Errorf("session: parse created_at: %w", err)
	}
	if s.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedStr); err != nil {
		return nil, fmt.Errorf("session: parse updated_at: %w", err)
	}
	if s.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresStr); err != nil {
		return nil, fmt.Errorf("session: parse expires_at: %w", err)
	}
	return &s, nil
}

func querySessions(ctx context.Context, db *sql.DB, q string, args ...interface{}) ([]*Session, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*Session, 0)
	for rows.Next() {
		s, err := scanSession(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// 错误判别辅助
// -----------------------------------------------------------------------------

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// modernc sqlite 的唯一冲突错误消息含 "constraint failed: UNIQUE constraint failed"。
	for _, key := range []string{"UNIQUE constraint failed", "unique constraint failed", "constraint failed"} {
		if contains(msg, key) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	n := len(sub)
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == sub {
			return true
		}
	}
	return false
}
