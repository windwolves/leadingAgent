package models

import "time"

type TokenCost struct {
	ID               string    `json:"id"`
	SessionID        string    `json:"session_id"`
	RequestID        string    `json:"request_id"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	RequestType      string    `json:"request_type"`
	Endpoint         string    `json:"endpoint"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CacheHit         bool      `json:"cache_hit"`
	CacheReadTokens  int       `json:"cache_read_tokens"`
	CreatedAt        time.Time `json:"created_at"`
}
