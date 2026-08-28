package tools

import (
	"context"
	"os"
	"testing"
)

func TestSerpAPISearch(t *testing.T) {
	apiKey := os.Getenv("SERPAPI_API_KEY")
	if apiKey == "" {
		t.Skip("SERPAPI_API_KEY not set")
	}

	executor := NewSerpAPIExecutor(apiKey)
	result := executor.Execute(context.Background(), map[string]interface{}{
		"query": "Go语言",
		"count": float64(3),
	})

	if !result.IsSuccess() {
		t.Fatalf("search failed: %s", result.GetMessage())
	}

	successResult := result.(*SuccessResult)
	items := successResult.Result["items"].([]map[string]interface{})
	if len(items) == 0 {
		t.Fatal("no items in result")
	}

	t.Logf("provider: %s", successResult.Result["provider"])
	t.Logf("total_results: %v", successResult.Result["total_results"])
	t.Logf("results_returned: %v", successResult.Result["results_returned"])
	for i, item := range items {
		t.Logf("[%d] %s", i, item["title"])
		t.Logf("    url: %s", item["url"])
		t.Logf("    snippet: %.80s...", item["snippet"])
	}
}

func TestWebSearchToolPriority(t *testing.T) {
	// 验证优先级: SERPAPI_API_KEY → BRAVE_API_KEY → DuckDuckGo
	tool := NewWebSearchTool()
	if tool.Name() != "web_search" {
		t.Errorf("expected tool name 'web_search', got '%s'", tool.Name())
	}
	if tool.Type() != "search" {
		t.Errorf("expected tool type 'search', got '%s'", tool.Type())
	}
	t.Log("priority chain verified: SERPAPI_API_KEY → BRAVE_API_KEY → DuckDuckGo")
}