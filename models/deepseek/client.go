package deepseek

import (
	"github.com/go-resty/resty/v2"
)

type Client struct {
	apiKey string
	apiURL string
	model  string
	client *resty.Client
}

func NewClient(apiKey, apiURL, model string) *Client {
	return &Client{
		apiKey: apiKey,
		apiURL: apiURL,
		model:  model,
		client: resty.New().
			SetBaseURL(apiURL).
			SetAuthToken(apiKey).
			SetHeader("Content-Type", "application/json"),
	}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int     `json:"index"`
		Message Message `json:"message"`
		Finish  string  `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (c *Client) Chat(messages []Message) (*ChatResponse, error) {
	req := &ChatRequest{
		Model:    c.model,
		Messages: messages,
	}

	var resp ChatResponse
	_, err := c.client.R().
		SetBody(req).
		SetResult(&resp).
		Post("/chat/completions")

	if err != nil {
		return nil, err
	}

	return &resp, nil
}