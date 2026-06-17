package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"leadingAgent/agent/foundation"
	"leadingAgent/models/openai"
)

const criticSystemPrompt = `你是一个严格但务实的回答评审员。你会看到用户的原始请求和助手给出的最终回答，
请判断这个回答是否真正解决了用户的请求（是否完整、准确、没有遗漏关键步骤）。
只输出 JSON，不要任何其他文字：{"approved": true|false, "feedback": "如果 approved 为 false，给出具体、可执行的修改建议；否则留空"}`

type criticVerdict struct {
	Approved bool   `json:"approved"`
	Feedback string `json:"feedback"`
}

// reviewFinalAnswer asks a lightweight, separate LLM call to judge whether the
// assistant's final answer actually satisfies the user's original request.
// It fails open (approved=true) on any error reaching the model or parsing its
// verdict, so the critic can never block the main loop from returning a response.
func (a *Agent) reviewFinalAnswer(ctx context.Context, model *foundation.Model, userQuery, answer string) criticVerdict {
	client := a.resolveClient(model)

	msgs := []openai.Message{
		{Role: "system", Content: criticSystemPrompt},
		{Role: "user", Content: fmt.Sprintf("用户请求：\n%s\n\n助手回答：\n%s", userQuery, answer)},
	}

	resp, err := client.Chat(msgs, nil, 1024, "low", nil)
	if err != nil {
		a.logger.Printf("[Critic] review call failed (fail-open, approved): %v", err)
		return criticVerdict{Approved: true}
	}
	if resp == nil || len(resp.Choices) == 0 {
		a.logger.Printf("[Critic] review call returned empty response (fail-open, approved)")
		return criticVerdict{Approved: true}
	}
	if resp.Usage.TotalTokens > 0 {
		a.accumulateUsage("critic", resp.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}

	raw := strings.TrimSpace(resp.Choices[0].Message.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var verdict criticVerdict
	if err := json.Unmarshal([]byte(raw), &verdict); err != nil {
		a.logger.Printf("[Critic] failed to parse verdict %q (fail-open, approved): %v", raw, err)
		return criticVerdict{Approved: true}
	}

	a.logger.Printf("[Critic] verdict: approved=%v feedback=%q", verdict.Approved, verdict.Feedback)
	return verdict
}
