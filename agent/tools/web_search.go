package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"golang.org/x/net/html"
)

const (
	serpAPIEndpoint     = "https://serpapi.com/search"
	braveSearchEndpoint = "https://api.search.brave.com/res/v1/web/search"
	ddgSearchEndpoint   = "https://html.duckduckgo.com/html/"
)

type BraveSearchExecutor struct {
	apiKey string
	client *resty.Client
}

func NewBraveSearchExecutor(apiKey string) *BraveSearchExecutor {
	return &BraveSearchExecutor{
		apiKey: apiKey,
		client: resty.New().SetTimeout(10 * time.Second),
	}
}

type SerpAPIExecutor struct {
	apiKey string
	client *resty.Client
}

func NewSerpAPIExecutor(apiKey string) *SerpAPIExecutor {
	return &SerpAPIExecutor{
		apiKey: apiKey,
		client: resty.New().SetTimeout(30 * time.Second),
	}
}

func NewWebSearchTool() *Tool {
	var executor ToolExecutor
	if apiKey := os.Getenv("SERPAPI_API_KEY"); apiKey != "" {
		executor = NewSerpAPIExecutor(apiKey)
	} else if apiKey := os.Getenv("BRAVE_API_KEY"); apiKey != "" {
		executor = NewBraveSearchExecutor(apiKey)
	} else {
		executor = NewDuckDuckGoExecutor()
	}

	return NewTool(
		"web_search",
		"search",
		"Search the web for relevant pages. Supports SerpAPI, Brave Search, and DuckDuckGo.",
		[]ToolParameter{
			{
				Name:        "query",
				Type:        "string",
				Description: "The search query string (max 400 characters)",
				Required:    true,
			},
			{
				Name:        "count",
				Type:        "number",
				Description: "Number of search results to return (1-20, default 10)",
				Required:    false,
				Default:     10,
			},
		},
		executor,
	)
}

func (e *BraveSearchExecutor) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query string (max 400 characters)",
			},
			"count": map[string]interface{}{
				"type":        "number",
				"description": "Number of search results to return (1-20, default 10)",
			},
		},
		"required": []string{"query"},
	}
}

func (e *BraveSearchExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	startTime := time.Now()

	query, ok := params["query"].(string)
	if !ok || query == "" {
		return NewErrorResult("web_search", "search",
			"Missing required parameter: query",
			"MISSING_PARAM", time.Since(startTime), "缺少查询参数")
	}

	count := 10
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
	if count > 20 {
		count = 20
	}

	resp, err := e.client.R().
		SetContext(ctx).
		SetHeader("X-Subscription-Token", e.apiKey).
		SetHeader("Accept", "application/json").
		SetQueryParams(map[string]string{
			"q":     query,
			"count": fmt.Sprintf("%d", count),
		}).
		Get(braveSearchEndpoint)

	if err != nil {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("Brave search request failed: %v", err),
			"REQUEST_FAILED", time.Since(startTime), "搜索请求失败")
	}

	if resp.IsError() {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("Brave API returned HTTP %d: %s", resp.StatusCode(), string(resp.Body())),
			"API_ERROR", time.Since(startTime), "Brave API返回错误")
	}

	var braveResp BraveSearchResponse
	if err := json.Unmarshal(resp.Body(), &braveResp); err != nil {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("Failed to parse search response: %v", err),
			"PARSE_FAILED", time.Since(startTime), "解析搜索结果失败")
	}

	items := make([]map[string]interface{}, 0, len(braveResp.Web.Results))
	for _, r := range braveResp.Web.Results {
		item := map[string]interface{}{
			"title":   r.Title,
			"url":     r.URL,
			"snippet": r.Description,
		}
		if u, err := url.Parse(r.URL); err == nil {
			item["display_url"] = u.Host + u.Path
		}
		items = append(items, item)
	}

	result := map[string]interface{}{
		"query":            query,
		"total_results":    braveResp.Web.TotalResults,
		"results_returned": len(items),
		"items":            items,
		"provider":         "brave",
	}

	return NewSuccessResult("web_search", "search", result, time.Since(startTime),
		fmt.Sprintf("Brave搜索完成，找到约 %d 条相关结果，返回 %d 条", braveResp.Web.TotalResults, len(items)))
}

type BraveSearchResponse struct {
	Web BraveWeb `json:"web"`
}

type BraveWeb struct {
	TotalResults int64            `json:"total_results"`
	Results      []BraveWebResult `json:"results"`
}

type BraveWebResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

func (e *SerpAPIExecutor) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The search query string",
			},
			"count": map[string]interface{}{
				"type":        "number",
				"description": "Number of search results to return (1-100, default 10)",
			},
		},
		"required": []string{"query"},
	}
}

func (e *SerpAPIExecutor) Execute(ctx context.Context, params map[string]interface{}) ToolResult {
	startTime := time.Now()

	query, ok := params["query"].(string)
	if !ok || query == "" {
		return NewErrorResult("web_search", "search",
			"Missing required parameter: query",
			"MISSING_PARAM", time.Since(startTime), "缺少查询参数")
	}

	count := 10
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
	if count > 100 {
		count = 100
	}

	resp, err := e.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"api_key": e.apiKey,
			"engine":  "google",
			"q":       query,
			"num":     fmt.Sprintf("%d", count),
			"gl":      "cn",
			"hl":      "zh-cn",
		}).
		Get(serpAPIEndpoint)

	if err != nil {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("SerpAPI request failed: %v", err),
			"REQUEST_FAILED", time.Since(startTime), "搜索请求失败")
	}

	if resp.IsError() {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("SerpAPI returned HTTP %d: %s", resp.StatusCode(), string(resp.Body())),
			"API_ERROR", time.Since(startTime), "SerpAPI返回错误")
	}

	var serpResp SerpAPIResponse
	if err := json.Unmarshal(resp.Body(), &serpResp); err != nil {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("Failed to parse search response: %v", err),
			"PARSE_FAILED", time.Since(startTime), "解析搜索结果失败")
	}

	items := make([]map[string]interface{}, 0, len(serpResp.OrganicResults))
	for _, r := range serpResp.OrganicResults {
		item := map[string]interface{}{
			"title":   r.Title,
			"url":     r.Link,
			"snippet": r.Snippet,
		}
		if r.DisplayedLink != "" {
			item["display_url"] = r.DisplayedLink
		}
		items = append(items, item)
	}

	totalResults := int64(len(items))
	if serpResp.SearchInformation.TotalResults > 0 {
		totalResults = serpResp.SearchInformation.TotalResults
	}

	result := map[string]interface{}{
		"query":            query,
		"total_results":    totalResults,
		"results_returned": len(items),
		"items":            items,
		"provider":         "serpapi",
	}

	return NewSuccessResult("web_search", "search", result, time.Since(startTime),
		fmt.Sprintf("SerpAPI搜索完成，找到约 %d 条相关结果，返回 %d 条", totalResults, len(items)))
}

type SerpAPIResponse struct {
	SearchInformation SerpAPISearchInformation `json:"search_information"`
	OrganicResults    []SerpAPIOrganicResult   `json:"organic_results"`
}

type SerpAPISearchInformation struct {
	TotalResults int64 `json:"total_results"`
}

type SerpAPIOrganicResult struct {
	Position      int    `json:"position"`
	Title         string `json:"title"`
	Link          string `json:"link"`
	Snippet       string `json:"snippet"`
	DisplayedLink string `json:"displayed_link"`
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
		SetQueryParam("q", query).
		Get(ddgSearchEndpoint)

	if err != nil {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("DuckDuckGo search request failed: %v", err),
			"REQUEST_FAILED", time.Since(startTime), "搜索请求失败")
	}

	if resp.IsError() {
		return NewErrorResult("web_search", "search",
			fmt.Sprintf("DuckDuckGo returned HTTP %d", resp.StatusCode()),
			"API_ERROR", time.Since(startTime), "DuckDuckGo错误")
	}

	items := parseDDGHTML(resp.Body(), count)

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

// parseDDGHTML 从 DuckDuckGo HTML 搜索结果页提取标题、链接和摘要。
func parseDDGHTML(body []byte, maxResults int) []map[string]interface{} {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil
	}

	type rawResult struct {
		url, title, snippet string
	}
	var results []rawResult
	var current *rawResult

	var inResult bool
	var inLink, inSnippet bool

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "div" {
				for _, a := range n.Attr {
					if a.Key == "class" && (strings.Contains(a.Val, "result") || strings.Contains(a.Val, "results_links")) {
						if current != nil && current.url != "" {
							results = append(results, *current)
						}
						current = &rawResult{}
						inResult = true
						break
					}
				}
			}

			if n.Data == "a" && inResult && current != nil {
				for _, a := range n.Attr {
					if a.Key == "class" && strings.Contains(a.Val, "result__a") {
						for _, aa := range n.Attr {
							if aa.Key == "href" {
								current.url = aa.Val
							}
						}
						inLink = true
						break
					}
					if a.Key == "class" && strings.Contains(a.Val, "result__snippet") {
						inSnippet = true
						break
					}
				}
			}
		}

		if n.Type == html.TextNode {
			if inLink && current != nil {
				current.title += n.Data
			}
			if inSnippet && current != nil {
				current.snippet += n.Data
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode {
			if n.Data == "a" && inLink {
				inLink = false
			}
			if n.Data == "a" && inSnippet {
				inSnippet = false
			}
		}
	}

	walk(doc)

	if current != nil && current.url != "" {
		results = append(results, *current)
	}

	items := make([]map[string]interface{}, 0, len(results))
	for i, r := range results {
		if i >= maxResults {
			break
		}
		item := map[string]interface{}{
			"title":   strings.TrimSpace(r.title),
			"snippet": strings.TrimSpace(r.snippet),
			"url":     r.url,
		}
		if u, err := url.Parse(r.url); err == nil {
			item["display_url"] = u.Host + u.Path
		}
		items = append(items, item)
	}

	return items
}