package repository

import (
	"database/sql"
	"fmt"
	"time"

	"leadingAgent/models"
	_ "modernc.org/sqlite"
)

type CostRepository interface {
	Save(cost *models.TokenCost) error
	GetByRequestID(requestID string) (*models.TokenCost, error)
	GetAll() ([]models.TokenCost, error)
	GetTotalTokens() (int, error)
	Close() error
}

type costRepository struct {
	db *sql.DB
}

func NewCostRepository(dbPath string) (CostRepository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	err = createTable(db)
	if err != nil {
		return nil, err
	}

	return &costRepository{db: db}, nil
}

func createTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS token_costs (
		id TEXT PRIMARY KEY,
		request_id TEXT NOT NULL,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		request_type TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		prompt_tokens INTEGER NOT NULL,
		completion_tokens INTEGER NOT NULL,
		total_tokens INTEGER NOT NULL,
		cache_hit INTEGER NOT NULL DEFAULT 0,
		cache_read_tokens INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_token_costs_request_id ON token_costs(request_id);
	CREATE INDEX IF NOT EXISTS idx_token_costs_created_at ON token_costs(created_at);
	CREATE INDEX IF NOT EXISTS idx_token_costs_provider ON token_costs(provider);
	`

	_, err := db.Exec(query)
	if err != nil {
		return err
	}

	// Add session_id column if it doesn't exist (migration for existing DBs).
	// SQLite doesn't support IF NOT EXISTS on ADD COLUMN, so we ignore the error
	// if the column already exists.
	_, _ = db.Exec(`ALTER TABLE token_costs ADD COLUMN session_id TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_token_costs_session_id ON token_costs(session_id)`)

	return nil
}

func (r *costRepository) Save(cost *models.TokenCost) error {
	if cost.ID == "" {
		cost.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if cost.CreatedAt.IsZero() {
		cost.CreatedAt = time.Now()
	}

	query := `
	INSERT OR REPLACE INTO token_costs (
		id, session_id, request_id, provider, model, request_type, endpoint,
		prompt_tokens, completion_tokens, total_tokens, cache_hit, cache_read_tokens, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.Exec(
		query,
		cost.ID,
		cost.SessionID,
		cost.RequestID,
		cost.Provider,
		cost.Model,
		cost.RequestType,
		cost.Endpoint,
		cost.PromptTokens,
		cost.CompletionTokens,
		cost.TotalTokens,
		boolToInt(cost.CacheHit),
		cost.CacheReadTokens,
		cost.CreatedAt.Format(time.RFC3339),
	)

	return err
}

func (r *costRepository) GetByRequestID(requestID string) (*models.TokenCost, error) {
	query := `SELECT id, session_id, request_id, provider, model, request_type, endpoint,
		prompt_tokens, completion_tokens, total_tokens, cache_hit, cache_read_tokens, created_at
		FROM token_costs WHERE request_id = ?`

	row := r.db.QueryRow(query, requestID)

	var cost models.TokenCost
	var createdStr string
	var cacheHitInt int

	err := row.Scan(
		&cost.ID,
		&cost.SessionID,
		&cost.RequestID,
		&cost.Provider,
		&cost.Model,
		&cost.RequestType,
		&cost.Endpoint,
		&cost.PromptTokens,
		&cost.CompletionTokens,
		&cost.TotalTokens,
		&cacheHitInt,
		&cost.CacheReadTokens,
		&createdStr,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	cost.CacheHit = cacheHitInt != 0
	cost.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return nil, err
	}

	return &cost, nil
}

func (r *costRepository) GetAll() ([]models.TokenCost, error) {
	query := `SELECT id, session_id, request_id, provider, model, request_type, endpoint,
		prompt_tokens, completion_tokens, total_tokens, cache_hit, cache_read_tokens, created_at
		FROM token_costs ORDER BY created_at DESC`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var costs []models.TokenCost

	for rows.Next() {
		var cost models.TokenCost
		var createdStr string
		var cacheHitInt int

		err := rows.Scan(
			&cost.ID,
			&cost.SessionID,
			&cost.RequestID,
			&cost.Provider,
			&cost.Model,
			&cost.RequestType,
			&cost.Endpoint,
			&cost.PromptTokens,
			&cost.CompletionTokens,
			&cost.TotalTokens,
			&cacheHitInt,
			&cost.CacheReadTokens,
			&createdStr,
		)

		if err != nil {
			return nil, err
		}

		cost.CacheHit = cacheHitInt != 0
		cost.CreatedAt, err = time.Parse(time.RFC3339, createdStr)
		if err != nil {
			return nil, err
		}

		costs = append(costs, cost)
	}

	return costs, nil
}

func (r *costRepository) GetTotalTokens() (int, error) {
	query := `SELECT COALESCE(SUM(total_tokens), 0) FROM token_costs`

	var total int
	err := r.db.QueryRow(query).Scan(&total)
	if err != nil {
		return 0, err
	}

	return total, nil
}

func (r *costRepository) Close() error {
	return r.db.Close()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
