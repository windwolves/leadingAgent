package memory

import "time"

type MemoryItem struct {
	ID        string            `json:"id"`
	Memory    string            `json:"memory"`
	UserID    string            `json:"userID"`
	Hash      string            `json:"hash"`
	Metadata  map[string]string `json:"metadata"`
	Score     float32           `json:"score"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}
