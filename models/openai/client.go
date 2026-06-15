package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"leadingAgent/agent/foundation"

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
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDef struct {
	Type     string          `json:"type"`
	Function ToolDefFunction `json:"function"`
}

type ToolDefFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ChatRequest struct {
	Model           string          `json:"model"`
	Messages        []Message       `json:"messages"`
	Tools           []ToolDef       `json:"tools,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
	Thinking        json.RawMessage `json:"thinking,omitempty"`
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

type StreamChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int     `json:"index"`
		Delta        Message `json:"delta"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

func (c *Client) Chat(messages []Message, tools []ToolDef, maxTokens int, reasoningEffort string, thinking json.RawMessage) (*ChatResponse, error) {
	req := &ChatRequest{
		Model:           c.model,
		Messages:        messages,
		Tools:           tools,
		MaxTokens:       maxTokens,
		ReasoningEffort: reasoningEffort,
		Thinking:        thinking,
	}

	var resp ChatResponse
	httpResp, err := c.client.R().
		SetBody(req).
		SetResult(&resp).
		Post("/chat/completions")

	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode() != 200 {
		return nil, fmt.Errorf("chat api: %s (status %d)", httpResp.String(), httpResp.StatusCode())
	}

	return &resp, nil
}

func (c *Client) StreamChat(messages []Message, tools []ToolDef, maxTokens int, reasoningEffort string, thinking json.RawMessage, handler func(*StreamChatResponse) error) error {
	req := &ChatRequest{
		Model:           c.model,
		Messages:        messages,
		Tools:           tools,
		Stream:          true,
		MaxTokens:       maxTokens,
		ReasoningEffort: reasoningEffort,
		Thinking:        thinking,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/chat/completions", c.apiURL)

	httpClient := &http.Client{}
	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}

		if i := bytes.Index(data, []byte("\n")); i >= 0 {
			return i + 1, data[0:i], nil
		}

		if atEOF {
			return len(data), data, nil
		}

		return 0, nil, nil
	})

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		prefix := []byte("data: ")
		if !bytes.HasPrefix(line, prefix) {
			continue
		}

		line = bytes.TrimPrefix(line, prefix)

		if string(line) == "[DONE]" {
			break
		}

		var streamResp StreamChatResponse
		if err := json.Unmarshal(line, &streamResp); err != nil {
			return err
		}

		if err := handler(&streamResp); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}

	return nil
}

func ConvertMessages(messages []foundation.Message) []Message {
	result := make([]Message, 0, len(messages))
	for _, msg := range messages {
		dsMsg := Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		}

		if len(msg.ToolCalls) > 0 {
			dsMsg.Content = ""
			for _, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Input)
				dsMsg.ToolCalls = append(dsMsg.ToolCalls, ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: FunctionCall{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}

		if msg.ToolResult != nil {
			dsMsg.Content = msg.ToolResult.Content
			dsMsg.ToolCallID = msg.ToolResult.ToolUseID
			dsMsg.Role = "tool"
		}

		result = append(result, dsMsg)
	}
	return result
}
