# Context 实现与结构分析

> 版本：v0.2 · 日期：2026-06-14

本文档记录 `leadingAgent` 项目中上下文管理的完整链路、数据结构和当前缺口。

---

## 1. Context 全链路

```
HTTP 请求
  │
  ▼
handlers/agent_handler.go         — 解析 ChatRequest{Message, SessionId, UserId}
  │                                  HandleChat 超时 5 分钟，HandleSessions 超时 5 秒
  ▼
services/agent_service.go         — GetOrCreate → AppendUserMessage → memory.Recall 注入
  │                                 → history = Messages[:n-1] (裁剪历史)
  ▼                              ┌─ memory/markdown_provider.go ── 从 Markdown 文件召回 5 条记忆
  │                              │
session/session_manager.go        — 会话生命周期 / 乐观锁 / 越权校验 / 后台 GC
  │
  ▼
session/sqlite_repository.go      — messages 以 JSON 列落盘
  │
  ▼
agent/agent.go                    — buildMessages → ReAct 循环 → LLM 调用
  │                                 [System(prompt+memories), ...history, User(query)]
  ▼
models/deepseek/client.go         — ConvertMessages → POST /chat/completions
```

---

## 2. Context 的三层来源

| 来源 | 对应字段 | 设置时机 | 注入位置 |
|---|---|---|---|
| System Prompt | `Session.SystemPrompt` | `Manager.Create` 时从 Manager 默认值复制 | `buildMessages` 第一条 |
| 长期记忆 | `memories.Recall()` 返回值 | `AgentService` 每轮对话前动态查询 | 追加到 System Prompt 末尾 `## User Memories` |
| 消息历史 | `Session.Messages` | 每轮 `AppendUserMessage` + `AppendAssistantMessage` 追加 | `buildMessages` 中间段 |
| 当前输入 | 方法参数 `userQuery` | 前端实时传入 | `buildMessages` 最后一条 |

> **记忆注入**：`AgentService.StreamChat` / `Chat` 在获取 Session 后调用 `s.memories.Recall(ctx, userID, message, 5)`，召回 5 条最相关长期记忆，以 `## User Memories` 格式追加到 System Prompt 末尾，再传入 Agent。详见第 9 节。

### `buildMessages` 拼装结果

```json
[
  {"role": "system",   "content": "You are a helpful assistant...\n## User Memories\n- 用户名叫 Bob\n- 偏好 Python"},
  {"role": "user",     "content": "上次说了什么"},
  {"role": "assistant", "content": "我们讨论了..."},
  {"role": "tool",     "tool_result": {"content": "..."}},
  {"role": "user",     "content": "我的名字是Bob"}
]
```

### 参数传递路径

```
session.Manager.systemPrompt
  → Session.SystemPrompt (创建时复制)
    → AgentService.StreamChat: prompt = updated.SystemPrompt + "\n" + memories.Recall(...)
      → Agent.buildMessages(injectedPrompt, history, userQuery)
        → [system+memories, ...history, user]
```

### 两处 System Prompt 默认值

**session.Manager 默认（[session_manager.go](file:///Users/leading/Developer/Projects/leadingAgent/session/session_manager.go#L41)）：**

```
You are a helpful assistant. When you learn personal facts about the user
(name, preferences, technical background, etc.), call the remember_fact tool
to save them for future conversations. Always include the userId parameter.
```

**agent.go 默认（用作文本兜底）：**

```
You are a helpful assistant. When you need information, use the available tools
to search or take action. When you learn personal facts about the user, use the
remember_fact tool to save them.
```

> 两处均包含 `remember_fact` 工具使用指引。Session 级别的 Prompt 更详细（含 `userId` 参数说明），创建 Session 时若未指定则使用 Manager 默认值。

---

## 3. 历史消息裁剪逻辑

位于 [services/agent_service.go](file:///Users/leading/Developer/Projects/leadingAgent/services/agent_service.go#L60-L63)：

```go
var history []foundation.Message
if n := len(updated.Messages); n > 1 {
    history = updated.Messages[:n-1]   // 去掉最后一条（刚追加的当前用户消息）
}
```

**逻辑**：先把当前用户消息追加到 Session，再取 `Messages[:n-1]` 作为历史注入 Agent。当前用户消息单独作为 `userQuery` 参数传入，避免在 messages 数组中重复。

---

## 4. ReAct 循环中的 Context 增长

```
buildMessages → [system+memories, history..., user]
       │
       ▼
   think() → LLM 返回
       │
   ├── finish_reason=stop → 返回 assistant content
   │
   └── 有 tool_calls → executeParallel(工具并发执行)
           │
           ▼
       追加 tool_result 到 messages 数组
           │
           ▼
       回到 think() 继续（messages 持续增长）
```

**关键点**：

- 工具调用的中间过程（assistant 的 tool_calls 消息 + tool 的 tool_result 消息）**仅在当前请求的 `messages` 切片中原地追加**，不会持久化到 `Session.Messages`。
- 只有最终的 user 消息 + 最终的 assistant 回复（不含 tool_calls）会 `Append` 到 Session 并落盘。
- 循环终止条件：LLM 返回 `finish_reason=stop` 而非 `tool_calls`。
- `remember_fact` 工具在循环中执行时，会通过全局回调 `WriteMemoryFunc` 调用 `MemoryService.Remember`，将记忆写入 Markdown 文件并持久化索引。

---

## 5. Session 中 Context 相关字段

| 字段 | 类型 | 当前值/行为 | 状态 |
|---|---|---|---|
| `SystemPrompt` | `string` | Session 创建时从 Manager 默认值复制，可被记忆注入动态追加 | 工作中 |
| `Messages` | `[]foundation.Message` | SQLite 中 JSON 列存储全量历史（仅 user + final assistant） | 工作中 |
| `MaxMessages` | `int` | 默认 200，达到后 `IsWritable()=false` | 硬上限，不截断 |
| `State` | `SessionState` | CREATED → ACTIVE（首轮对话后）→ PAUSED/COMPLETED/EXPIRED/ERROR/ARCHIVED | 工作中 |
| `ErrorCount` | `int` | `RecordError` 递增，达阈值自动置 ERROR 状态 | 工作中 |
| `MetaData` | `map[string]interface{}` | 初始化空 map，从未写入 | 预留 |
| `TokenUsage` | `TokenUsage` | 结构存在，`client.go`/`agent.go` 仅日志打印，未回写 | 预留 |
| `ModelConfig` | `ModelConfig` | 会话级模型配置（Name/Provider/Temperature） | 未使用 |
| `ToolCalls` | `[]ToolCallMeta` | 工具调用审计（ID/名称/耗时/成功） | 未回写 |
| `Version` | `int64` | 乐观锁，每次 Append 递增（最多重试 3 次） | 工作中 |
| `ExpiresAt` | `time.Time` | TTL 默认 1h，后台 GC 每 60 秒回收过期会话 | 工作中 |

### Session 生命周期与并发控制

- **per-session 串行化**：`lockFor(id)` 懒加载互斥锁，确保同 session 并发写安全
- **越权校验**：每次 Get/GetOrCreate/Append 均校验 userID 匹配
- **乐观锁重试**：Append 操作最多重试 3 次（处理并发写冲突）
- **终态保护**：会话进入 PAUSED/COMPLETED/EXPIRED/ERROR 状态后拒绝写入
- **容量控制**：`maxPerUser` 默认 100，超限自动将最旧会话标记为 COMPLETED
- **后台 GC**：每 60 秒检查过期会话，locks 池超过 2000 时自动清理

---

## 6. foundation.Message 数据模型

定义位置：[agent/foundation/message.go](file:///Users/leading/Developer/Projects/leadingAgent/agent/foundation/message.go)

```go
type Role = "system" | "user" | "assistant" | "tool"

type Message struct {
    Role       Role
    Content    string             // 纯文本
    ToolCalls  []ToolUseContent   // assistant 工具调用
    ToolResult *ToolResultContent // tool 执行结果
}
```

### 相关 Content 类型（[agent/foundation/content.go](file:///Users/leading/Developer/Projects/leadingAgent/agent/foundation/content.go)）

| 类型 | 用途 |
|---|---|
| `TextContent` | 纯文本 |
| `ToolUseContent` | 模型返回的工具调用：Type="tool_use"、ID、Name、Input |
| `ToolResultContent` | 工具执行结果：Type="tool_result"、ToolUseID、Content |
| `ThinkingContent` | 思维链（预留） |
| `ImageURLContent` | 多模态图片（预留） |

### 高阶消息类型（预留，未使用）

```go
type SystemMessage    struct { Role RoleSystem,    Content []SystemContent }
type UserMessage      struct { Role RoleUser,      Content []UserContent }
type AssistantMessage struct { Role RoleAssistant, Content []AssistantContent }
type ToolMessage      struct { Role RoleTool,      Content []ToolContent }
```

### DeepSeek API 消息转换（[models/deepseek/client.go](file:///Users/leading/Developer/Projects/leadingAgent/models/deepseek/client.go)）

`ConvertMessages` 将 `foundation.Message` 转为 DeepSeek API 格式：

| foundation 场景 | DeepSeek 映射 |
|---|---|
| 普通消息 | Role + Content 直传 |
| 有 `ToolCalls` | Content 置空，ToolCalls → DeepSeek `ToolCall`（Index=0，Type="function"） |
| 有 `ToolResult` | Role → "tool"，Content → ToolResult.Content，ToolCallID → ToolResult.ToolUseID |

---

## 7. Streaming 事件类型

定义位置：[agent/agent.go](file:///Users/leading/Developer/Projects/leadingAgent/agent/agent.go)

```go
type StreamEvent struct {
    Type       string                 // thinking / text_delta / tool_call / tool_result / done / error
    Content    string
    Tool       string                 // tool_call 时的工具名称
    Result     string
    Input      map[string]interface{} // tool_call 时的输入参数
    Turns      string                 // 当前轮次信息
    SessionID  string
}
```

| 事件类型 | 触发时机 |
|---|---|
| `thinking` | reasoning_content（DeepSeek V4 推理内容） |
| `text_delta` | assistant 文本增量（包括 reasoning_content 合并） |
| `tool_call` | LLM 决定调用工具 |
| `tool_result` | 工具执行完成 |
| `done` | 本轮对话结束 |
| `error` | 异常错误 |

**SSE 格式**：每个事件序列化为 JSON，以 `data: ...\n\n` 输出，每次 flush。

---

## 8. DeepSeek API 模型调用

定义位置：[agent/agent.go](file:///Users/leading/Developer/Projects/leadingAgent/agent/agent.go) 的 `callModel` / `callModelStreaming`

### 请求参数

| 参数 | 来源 | 默认值 |
|---|---|---|
| API Key | 环境变量 `DEEPSEEK_API_KEY` | - |
| Base URL | 环境变量 `DEEPSEEK_BASE_URL` | - |
| Model | 环境变量 `DEEPSEEK_MODEL` | `deepseek-chat` |
| MaxTokens | 硬编码 | 8000 |
| ReasoningEffort | 硬编码 | `medium` |
| Temperature | 硬编码 | 0.0 |

> 注：`normalizeModelName` 函数将模型名统一为小写用于内部判断。

### Reasoning Content 处理

- **非流式**：解析 `ChatResponse.Choices[].Message.ReasoningContent`，拼接到最终 assistant content 前
- **流式**：累积 `StreamChatResponse.Choices[].Delta.ReasoningContent`，通过 `text_delta` 事件增量发送
- **注**：V4 系列模型（`deepseek-chat`、`deepseek-v4` 等）支持 reasoning_content

### Token 使用量

`ChatResponse.Usage` 包含 PromptTokens / CompletionTokens / TotalTokens，当前仅在 [agent.go](file:///Users/leading/Developer/Projects/leadingAgent/agent/agent.go#L350-L353) 中日志打印，**未回写到 Session.TokenUsage**。

---

## 9. 长期记忆（Memory）集成

> **状态：已完全落地，不再只是"集成计划"**

### 组件全景

```
AgentService.StreamChat()
  │
  ├─ (1) Recall: s.memories.Recall(ctx, userID, message, 5)
  │        → MemoryService.Recall → MarkdownProvider.Search → 返回格式化记忆文本
  │        → 注入到 System Prompt: "## User Memories\n- ..."
  │
  ├─ (2) ExecuteStreaming: Agent 运行 ReAct 循环
  │        → 当 LLM 返回 tool_calls 含 remember_fact 时
  │        → RememberFactTool.Execute → WriteMemoryFunc (全局回调)
  │        → MemoryService.Remember → MarkdownProvider.Create → 写 .md + 更新 index.json
  │
  └─ (3) AppendAssistantMessage: 持久化到 Session
```

### 涉及的模块

| 模块 | 文件 | 职责 |
|---|---|---|
| `MemoryService` | [services/memory_service.go](file:///Users/leading/Developer/Projects/leadingAgent/services/memory_service.go) | 提供 `Recall` / `Remember` 两个高层方法 |
| `memory.Provider` | [memory/provider.go](file:///Users/leading/Developer/Projects/leadingAgent/memory/provider.go) | 接口：Create / Get / Update / Delete / GetByHash / Search / ListByUser |
| `MarkdownProvider` | [memory/markdown_provider.go](file:///Users/leading/Developer/Projects/leadingAgent/memory/markdown_provider.go) | Markdown 文件存储 + JSON 索引，同步 flush |
| `MemoryIndex` | [memory/index.go](file:///Users/leading/Developer/Projects/leadingAgent/memory/index.go) | 内存索引：Upsert / Remove / Search / GetByID / GetByHash |
| `RememberFactTool` | [agent/tools/remember_fact.go](file:///Users/leading/Developer/Projects/leadingAgent/agent/tools/remember_fact.go) | 工具定义 + 全局回调 `WriteMemoryFunc` 桥接 |

### 记忆生命周期

1. **写入**：Agent 通过 `remember_fact` 工具调用 → `MemoryService.Remember` → `MarkdownProvider.Create` → 写入 `data/memories/{userId}/{category}/{id}.md` + 更新 `index.json`
2. **召回**：每轮对话前 `MemoryService.Recall` → `MarkdownProvider.Search` 按关键词匹配 5 条
3. **持久化**：`Create` / `Update` / `Delete` 均同步 flush `index.json`（先释放 `p.mu` 写锁再 flush，避免 I/O 阻塞并发写入）

---

## 10. 现有工具（8 个）

| 工具 | 定义文件 | 用途 |
|---|---|---|
| `BashTool` | `agent/tools/bash_tool.go` | 执行 shell 命令 |
| `ReadFileTool` | `agent/tools/read_file.go` | 读取文件（支持 start_line/end_line/max_chars） |
| `WriteFileTool` | `agent/tools/write_file.go` | 写入文件 |
| `StrReplaceTool` | `agent/tools/str_replace.go` | 文本替换 |
| `GlobSearchTool` | `agent/tools/glob_search.go` | 文件匹配 |
| `GrepSearchTool` | `agent/tools/grep_search.go` | 内容搜索 |
| `BingSearchTool` | `agent/tools/bing_search.go` | 网络搜索 |
| `RememberFactTool` | `agent/tools/remember_fact.go` | 记录用户偏好和事实到长期记忆 |

每个工具返回结果有长度限制（`max_chars` 默认 12000），防止单次工具调用占用过多上下文。

---

## 11. 当前缺口

| 缺口 | 现状 | 风险 |
|---|---|---|
| **无超长截断** | 历史全量注入，不做 token 计数 | 长对话超出 128K context window → LLM 报错 |
| **无摘要压缩** | 没有"早期消息压缩为摘要"的逻辑 | token 消耗线性增长 |
| **TokenUsage 空置** | 结构存在但 `agent.go` 仅日志打印，未回写 `Session.TokenUsage` | 无法做成本统计 |
| **ToolCalls 空置** | 审计结构 `ToolCallMeta` 存在但未回写 | 无法追踪工具调用频率/成功率 |
| **MetaData 空置** | 字段存在但从未写入 | 无法存储用户画像等扩展信息 |
| **ModelConfig 未使用** | `Session.ModelConfig` 存在但数据流未接通 | 无法按会话切换模型 |

### 已解决的缺口

| 缺口 | 解决方案 |
|---|---|
| ~~System Prompt 静态~~ | 记忆模块已注入：`memories.Recall()` 动态追加 `## User Memories` 到 System Prompt |
| ~~Memory 模块集成~~ | 已完全落地（MemoryService + MarkdownProvider + remember_fact 工具 + 索引持久化） |
