package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"leadingAgent/agent/foundation"
	"leadingAgent/agent/tools"
	"leadingAgent/models/deepseek"
)

type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, input map[string]interface{}) tools.ToolResult
	InputSchema() map[string]interface{}
}

type Agent struct {
	tools      []Tool
	modelCaller func(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error)
}

func NewAgent(tools ...Tool) *Agent {
	a := &Agent{
		tools: tools,
	}
	a.modelCaller = a.callModel
	return a
}

func (a *Agent) Execute(ctx context.Context, model *foundation.Model, userQuery string) error {
	messages := []foundation.Message{
		{
			Role:    foundation.RoleSystem,
			Content: "You are a helpful assistant. When you need information, use the available tools to search or take action.",
		},
		{
			Role:    foundation.RoleUser,
			Content: userQuery,
		},
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		assistantMsg, err := a.think(ctx, model, messages)
		if err != nil {
			return err
		}

		messages = append(messages, *assistantMsg)

		if assistantMsg.ToolCall != nil {
			result := a.act(ctx, assistantMsg.ToolCall)

			content := result.GetMessage()
			if result.IsSuccess() {
				if sr, ok := result.(*tools.SuccessResult); ok {
					jsonBytes, err := json.Marshal(sr.Result)
					if err == nil {
						content = string(jsonBytes)
					}
				}
			}

			toolMsg := foundation.Message{
				Role: foundation.RoleTool,
				ToolResult: &foundation.ToolResultContent{
					Type:      "tool_result",
					ToolUseID: assistantMsg.ToolCall.ID,
					Content:   content,
				},
			}
			messages = append(messages, toolMsg)
		} else {
			log.Printf("[Agent] Final response: %s", assistantMsg.Content)
			return nil
		}
	}
}

func (a *Agent) think(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
	return a.modelCaller(ctx, model, messages)
}

func (a *Agent) act(ctx context.Context, toolCall *foundation.ToolUseContent) tools.ToolResult {
	for _, t := range a.tools {
		if t.Name() == toolCall.Name {
			return t.Execute(ctx, toolCall.Input)
		}
	}
	return tools.NewErrorResult(toolCall.Name, "function",
		fmt.Sprintf("tool %s not found", toolCall.Name),
		"TOOL_NOT_FOUND", 0, "工具未找到")
}

func (a *Agent) callModel(ctx context.Context, model *foundation.Model, messages []foundation.Message) (*foundation.Message, error) {
	apiKey := ""
	apiURL := "https://api.deepseek.com/v1"

	if key, ok := model.Options["api_key"].(string); ok && key != "" {
		apiKey = key
	}
	if url, ok := model.Options["api_url"].(string); ok && url != "" {
		apiURL = url
	}
	if apiKey == "" {
		apiKey = os.Getenv("DEEPSEEK_API_KEY")
	}
	if envURL := os.Getenv("DEEPSEEK_API_URL"); envURL != "" {
		apiURL = envURL
	}

	modelName := model.Name
	if modelName == "" {
		modelName = "deepseek-chat"
	}

	client := deepseek.NewClient(apiKey, apiURL, modelName)

	deepseekMessages := convertToDeepSeekMessages(messages)
	toolDefs := a.buildToolDefinitions()

	resp, err := client.Chat(deepseekMessages, toolDefs)
	if err != nil {
		return nil, fmt.Errorf("DeepSeek API call failed: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("DeepSeek returned empty choices")
	}

	choice := resp.Choices[0]
	foundationMsg := &foundation.Message{
		Role:    foundation.RoleAssistant,
		Content: choice.Message.Content,
	}

	if len(choice.Message.ToolCalls) > 0 {
		tc := choice.Message.ToolCalls[0]
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			args = map[string]interface{}{"raw": tc.Function.Arguments}
		}

		foundationMsg.ToolCall = &foundation.ToolUseContent{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: args,
		}
	}

	if resp.Usage.TotalTokens > 0 {
		log.Printf("[DeepSeek] model=%s tokens: prompt=%d completion=%d total=%d",
			resp.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	}

	return foundationMsg, nil
}

func convertToDeepSeekMessages(messages []foundation.Message) []deepseek.Message {
	result := make([]deepseek.Message, 0, len(messages))
	for _, msg := range messages {
		dsMsg := deepseek.Message{
			Role:    string(msg.Role),
			Content: msg.Content,
		}

		if msg.ToolCall != nil {
			argsJSON, _ := json.Marshal(msg.ToolCall.Input)
			dsMsg.Content = ""
			dsMsg.ToolCalls = []deepseek.ToolCall{
				{
					ID:   msg.ToolCall.ID,
					Type: "function",
					Function: deepseek.FunctionCall{
						Name:      msg.ToolCall.Name,
						Arguments: string(argsJSON),
					},
				},
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

func (a *Agent) buildToolDefinitions() []deepseek.ToolDef {
	defs := make([]deepseek.ToolDef, 0, len(a.tools))
	for _, t := range a.tools {
		schema := t.InputSchema()
		if schema == nil {
			schema = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			}
		}

		defs = append(defs, deepseek.ToolDef{
			Type: "function",
			Function: deepseek.ToolDefFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  schema,
			},
		})
	}
	return defs
}
