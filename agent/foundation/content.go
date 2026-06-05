package foundation

type TextContent struct {
	/** Discriminator: this segment is plain text. */
	Type string `json:"type"`
	/** UTF-8 text body. */
	Content string `json:"content"`
}

func NewTextContent(content string) *TextContent {
	return &TextContent{
		Type:    "text",
		Content: content,
	}
}

/**
 * Image referenced by URL, for multimodal user input.
 */
type ImageURLContent struct {
	/** Discriminator: this segment is an image URL. */
	Type string `json:"type"`
	/** Image URL. */
	ImageURL struct {
		/** HTTPS (or other) URL of the image resource. */
		URL string `json:"url"`
		/**
		 * Optional vision detail level; provider-specific.
		 * - `auto` — let the model decide resolution tradeoffs.
		 * - `high` / `low` — bias toward more or less visual detail.
		 */
		Detail string `json:"detail"`
	} `json:"image_url"`
}

func NewImageURLContent(url string, detail string) *ImageURLContent {
	return &ImageURLContent{
		Type: "image_url",
		ImageURL: struct {
			URL    string `json:"url"`
			Detail string `json:"detail"`
		}{
			Detail: detail,
			URL:    url,
		},
	}
}

type ThinkingContent struct {
	/** Discriminator: this segment is model reasoning text. */
	Type string `json:"type"`
	/** Opaque reasoning or chain-of-thought string from the model. */
	Thinking string `json:"thinking"`
}

func NewThinkingContent(thinking string) *ThinkingContent {
	return &ThinkingContent{
		Type:     "thinking",
		Thinking: thinking,
	}
}

type ToolUseContent struct {
	/** Discriminator: this segment is a tool call. */
	Type string `json:"type"`
	/** Stable identifier for this invocation; used to correlate with ToolResultContent. */
	ID string `json:"id"`
	/** Registered tool name the model selected. */
	Name string `json:"name"`
	/** JSON-serializable arguments passed to the tool. */
	Input map[string]interface{} `json:"input"`
}

func NewToolUseContent(id, name string, input map[string]interface{}) *ToolUseContent {
	return &ToolUseContent{
		Type:  "tool_use",
		ID:    id,
		Name:  name,
		Input: input,
	}
}

type ToolResultContent struct {
	/** Discriminator: this segment is a tool execution result. */
	Type string `json:"type"`
	/** Matches ToolUseContent.ID of the call this result answers. */
	ToolUseID string `json:"tool_use_id"`
	/** Human- or machine-readable outcome (often JSON string) from the tool runtime. */
	Content string `json:"content"`
}

func NewToolResultContent(toolUseID, content string) *ToolResultContent {
	return &ToolResultContent{
		Type:      "tool_result",
		ToolUseID: toolUseID,
		Content:   content,
	}
}

/** Content allowed in a system message. */
type SystemMessageContent []TextContent

type UserMessageContent []interface{}
type AssistantMessageContent []interface{}
type ToolMessageContent []ToolResultContent
