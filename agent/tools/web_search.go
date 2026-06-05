package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	bingSearchEndpoint = "https://api.bing.microsoft.com/v7.0/search"
	ddgSearchEndpoint  = "https://api.duckduckgo.com/"
)

type BingSearchExecutor struct {
	apiKey string
	client *resty.Client
}

func NewBingSearchExecutor(apiKey string) *BingSearchExecutor {
	return &BingSearchExecutor{
		apiKey: apiKey,
		client: resty.New().SetTimeout(10 * time.Second),
	}
}

func NewBingSearchTool() *Tool {
	apiKey := os.Getenv("BING_SEARCH_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("BING_API_KEY")
	}

	var executor ToolExecutor
	if apiKey != "" {
		executor = NewBingSearchExecutor(apiKey)
	} else {
		executor = NewDuckDuckGoExecutor()
	}

	return NewTool(
		"web_search",
		"search",
		"Search the web using Bing Search API. Returns relevant web pages for the given query.",
		[]ToolParameter{
			{
				Name:        "query",
				Type:        "string",
				Description: "The search query string (max 4096 characters)",
				Required:    true,
			},
			{
				Name:        "count",
				Type:        "number",
				Description: "Number of search results to return (1-50, default 5)",
				Required:    false,
				Default:     5,
			},
			{
				Name:        "offset",
				Type:        "number",
				Description: "Zero-based offset for pagination",
				Required:    false,
				Default:     0,
			},
			{
				Name:        "mkt",
				Type:        "string",
				Description: "Market code (e.g. zh-CN, en-US, default en-US)",
				Required:    false,
				Default:     "en-US",
			},
		},
		executor,
	)
}

func (e *BingSearchExecutor) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query string (max 4096 characters)",
			},
			"count": map[string]interface{}{
				"type":        "number",
				"description": "Number of search results to return (1-50, default 5)",
			},
			"offset": map[string]interface{}{
				"type":        "number",
				"description": "Zero-based offset for pagination",
			},
			"mkt": map[string]interface{}{
				"type":        "string",
				"description": "Market code (e.g. zh-CN, en-US, default en-US)",
			},
		},
		"required": []string{"query"},
	}
}

func (e *BingSearchExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	startTime := time.Now()

	query, ok := params["query"].(string)
	if !ok || query == "" {
		return NewErrorResult("web_search", "search",
			"Missing required parameter: query",
			"MISSING_PARAM", time.Since(startTime), "缺少查询参数")
	}

	count := 5
	if v, ok := params["count"]; ok {
		switch val := v.(type) {
		case float64:
			count = int(val)
		case int:
			count = val
		}
	}
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}

	offset := 0
	if v, ok := params["offset"]; ok {
		switch val := v.(type) {
		case float64:
			offset = int(val)
		case int:
			offset = val
		}
	}

	mkt := "en-US"
	if v, ok := params["mkt"].(string); ok && v != "" {
		mkt = v
	}

	resp, err := e.client.R().
		SetContext(ctx).
		SetHeader("Ocp-Apim-Subscription-Key", e.apiKey).
		SetQueryParams(map[string]string{
			"q":               query,
			"count":           fmt.Sprintf("%d", count),
			"offset":          fmt.Sprintf("%d", offset),
			"mkt":             mkt,
			"textDecorations": "true",
			"textFormat":      "Raw",
		}).
		Get(bingSearchEndpoint)

	if err != nil {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("Search request failed: %v", err),
			"REQUEST_FAILED", time.Since(startTime), "搜索请求失败")
	}

	if resp.IsError() {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("Bing API returned HTTP %d: %s", resp.StatusCode(), string(resp.Body())),
			"API_ERROR", time.Since(startTime), "Bing API返回错误")
	}

	var bingResp BingSearchResponse
	if err := json.Unmarshal(resp.Body(), &bingResp); err != nil {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("Failed to parse search response: %v", err),
			"PARSE_FAILED", time.Since(startTime), "解析搜索结果失败")
	}

	items := make([]map[string]interface{}, 0, len(bingResp.WebPages.Value))
	for _, page := range bingResp.WebPages.Value {
		items = append(items, map[string]interface{}{
			"name":              page.Name,
			"url":               page.URL,
			"snippet":           page.Snippet,
			"display_url":       page.DisplayURL,
			"date_last_crawled": page.DateLastCrawled,
		})
	}

	result := map[string]interface{}{
		"query":            query,
		"total_results":    bingResp.WebPages.TotalEstimatedMatches,
		"results_returned": len(items),
		"items":            items,
		"provider":         "bing",
	}

	return NewSuccessResult("web_search", "search", result, time.Since(startTime),
		fmt.Sprintf("Bing搜索完成，找到约 %d 条相关结果，返回 %d 条", bingResp.WebPages.TotalEstimatedMatches, len(items)))
}

type BingSearchResponse struct {
	WebPages BingWebPages `json:"webPages"`
}

type BingWebPages struct {
	TotalEstimatedMatches int64         `json:"totalEstimatedMatches"`
	Value                 []BingWebPage `json:"value"`
}

type BingWebPage struct {
	Name            string `json:"name"`
	URL             string `json:"url"`
	DisplayURL      string `json:"displayUrl"`
	Snippet         string `json:"snippet"`
	DateLastCrawled string `json:"dateLastCrawled"`
}

type DuckDuckGoExecutor struct {
	client *resty.Client
}

func NewDuckDuckGoExecutor() *DuckDuckGoExecutor {
	return &DuckDuckGoExecutor{
		client: resty.New().SetTimeout(10 * time.Second),
	}
}

func (e *DuckDuckGoExecutor) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query string",
			},
			"count": map[string]interface{}{
				"type":        "number",
				"description": "Number of search results to return (default 5)",
			},
		},
		"required": []string{"query"},
	}
}

func (e *DuckDuckGoExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	startTime := time.Now()

	query, ok := params["query"].(string)
	if !ok || query == "" {
		return NewErrorResult("web_search", "search",
			"Missing required parameter: query",
			"MISSING_PARAM", time.Since(startTime), "缺少查询参数")
	}

	count := 5
	if v, ok := params["count"]; ok {
		switch val := v.(type) {
		case float64:
			count = int(val)
		case int:
			count = val
		}
	}
	if count < 1 {
		count = 1
	}

	resp, err := e.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"q":       query,
			"format":  "json",
			"no_html": "1",
			"skip_disambig": "1",
		}).
		Get(ddgSearchEndpoint)

	if err != nil {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("DuckDuckGo search request failed: %v", err),
			"REQUEST_FAILED", time.Since(startTime), "搜索请求失败")
	}

	if resp.IsError() {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("DuckDuckGo API returned HTTP %d: %s", resp.StatusCode(), string(resp.Body())),
			"API_ERROR", time.Since(startTime), "DuckDuckGo API错误")
	}

	var ddgResp DuckDuckGoResponse
	if err := json.Unmarshal(resp.Body(), &ddgResp); err != nil {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("Failed to parse DuckDuckGo response: %v", err),
			"PARSE_FAILED", time.Since(startTime), "解析搜索结果失败")
	}

	items := make([]map[string]interface{}, 0, len(ddgResp.RelatedTopics))
	for _, topic := range ddgResp.RelatedTopics {
		if topic.Text == "" {
			continue
		}
		item := map[string]interface{}{
			"snippet": topic.Text,
		}
		if topic.FirstURL != "" {
			u, parseErr := url.Parse(topic.FirstURL)
			if parseErr == nil {
				item["url"] = topic.FirstURL
				item["display_url"] = u.Host + u.Path
			}
		}
		items = append(items, item)
	}

	if ddgResp.AbstractText != "" {
		abstractItem := map[string]interface{}{
			"snippet":     ddgResp.AbstractText,
			"url":         ddgResp.AbstractURL,
			"display_url": ddgResp.AbstractSource,
		}
		items = append([]map[string]interface{}{abstractItem}, items...)
	}

	if len(items) > count {
		items = items[:count]
	}

	total := int64(len(items))
	result := map[string]interface{}{
		"query":            query,
		"total_results":    total,
		"results_returned": len(items),
		"items":            items,
		"provider":         "duckduckgo",
	}

	return NewSuccessResult("web_search", "search", result, time.Since(startTime),
		fmt.Sprintf("DuckDuckGo搜索完成，返回 %d 条相关结果", len(items)))
}

type DuckDuckGoResponse struct {
	AbstractText   string      `json:"AbstractText"`
	AbstractURL    string      `json:"AbstractURL"`
	AbstractSource string      `json:"AbstractSource"`
	Heading        string      `json:"Heading"`
	RelatedTopics  []DDGTopic  `json:"RelatedTopics"`
}

type DDGTopic struct {
	FirstURL string `json:"FirstURL"`
	Text     string `json:"Text"`
}
