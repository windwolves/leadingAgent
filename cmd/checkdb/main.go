package main

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "cost.db")
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		return
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, request_id, provider, model, request_type, endpoint, prompt_tokens, completion_tokens, total_tokens, cache_hit, cache_read_tokens, created_at FROM token_costs ORDER BY created_at DESC")
	if err != nil {
		fmt.Printf("Failed to query: %v\n", err)
		return
	}
	defer rows.Close()

	var id, reqId, provider, model, reqType, endpoint, createdAt string
	var pt, ct, tt, cacheHitInt, cacheReadTokens int

	fmt.Println("=== Token Cost Records ===")
	for rows.Next() {
		err := rows.Scan(&id, &reqId, &provider, &model, &reqType, &endpoint, &pt, &ct, &tt, &cacheHitInt, &cacheReadTokens, &createdAt)
		if err != nil {
			fmt.Printf("Failed to scan row: %v\n", err)
			continue
		}
		cacheHit := "No"
		if cacheHitInt != 0 {
			cacheHit = "Yes"
		}
		fmt.Printf("ID: %s\nRequestID: %s\nProvider: %s\nModel: %s\nRequestType: %s\nEndpoint: %s\nPromptTokens: %d\nCompletionTokens: %d\nTotalTokens: %d\nCacheHit: %s\nCacheReadTokens: %d\nCreatedAt: %s\n\n", 
			id, reqId, provider, model, reqType, endpoint, pt, ct, tt, cacheHit, cacheReadTokens, createdAt)
	}

	var totalTokens int
	err = db.QueryRow("SELECT COALESCE(SUM(total_tokens), 0) FROM token_costs").Scan(&totalTokens)
	if err != nil {
		fmt.Printf("Failed to get total tokens: %v\n", err)
		return
	}
	fmt.Printf("Total Tokens Used: %d\n", totalTokens)
}
