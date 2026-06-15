# Long-Term Memory System Design

> 版本：v0.1 · 状态：设计中

本文档描述 `leadingAgent` 项目的长期记忆子系统的设计方案，采用 Markdown 文件存储（Claude Code 风格），Provider 接口支持未来切换存储后端。

数据模型入口：[memory/memory_item.go](file:///Users/leading/Developer/Projects/leadingAgent/memory/memory_item.go)

---

## 1. 设计目标

1. **人类可读**：记忆以 Markdown 文件存储，任何编辑器可直接阅读和编辑。
2. **Git 友好**：文本文件，支持 `git diff` / `git log` 原生版本控制。
3. **可插拔**：Provider 接口允许未来切换到 SQLite / Vector DB，接口不变。
4. **多用户隔离**：按 `userID` 分目录，强制防止水平越权。
5. **按需触发**：Agent 通过工具调用显式写入，不自动追加注释，避免噪声。
6. **与 session 架构对齐**：沿用 `Repository → Manager → Service` 分层模式。

---

## 2. 架构分层

```
┌──────────────────────────────────────────────────┐
│  services/memory_service.go                      │  业务编排层
│  · Recall / Remember / 去重                      │
│  · 注入 AgentService 的 system prompt             │
└────────────────────┬─────────────────────────────┘
                     │
┌────────────────────▼─────────────────────────────┐
│  memory/provider.go  (Provider 接口)              │  接入抽象层
│  · Create / Get / Update / Delete                │
│  · Search / ListByUser / GetByHash               │
└──┬──────────────┬────────────────────────────────┘
   │              │
   ▼              ▼
┌──────────┐  ┌──────────────────┐
│InMemory  │  │ MarkdownProvider │                   实现层
│Provider  │  │ (生产默认)         │
│(测试用)   │  │                  │
└──────────┘  └──────────────────┘
```

---

## 3. Provider 接口

```go
type Provider interface {
    Create(ctx context.Context, item *MemoryItem) error
    Get(ctx context.Context, id, userID string) (*MemoryItem, error)
    Update(ctx context.Context, item *MemoryItem) error
    Delete(ctx context.Context, id, userID string) error
    GetByHash(ctx context.Context, userID, hash string) (*MemoryItem, error)
    Search(ctx context.Context, userID, query string, limit int) ([]*MemoryItem, error)
    ListByUser(ctx context.Context, userID string, limit, offset int) ([]*MemoryItem, error)
}
```

### 设计要点

- **全部方法带 `userID`**：与 session 一致，强制水平越权校验。
- **`GetByHash`**：去重查询。Agent 写入前先算 hash → 查是否已存在 → 存在则更新 Score 和时间戳，不重复插入。
- **`Search`**：关键词匹配降级实现。Markdown Provider 读 body 做 `strings.Contains`。未来切 Vector Provider 时接口不变。
- **`context.Context` 第一参数**：支持超时取消、链路追踪。

---

## 4. Markdown Provider 实现（默认）

### 4.1 目录结构与命名

```
data/memories/
├── index.json                    # 内存索引快照（启动加速，git ignored）
├── user-leading/                 # per-user 目录
│   ├── INDEX.md                  # 人类可读的记忆分类索引
│   ├── personal/                 # 分类目录
│   │   └── 2026-06-10-name-is-bob.md
│   ├── preferences/
│   │   └── 2026-06-10-favorite-model.md
│   ├── technical/
│   │   └── 2026-06-11-project-uses-go.md
│   └── facts/
│       └── 2026-06-11-company-name.md
└── user-{other}/
```

**文件命名规则**：`{YYYY-MM-DD}-{slug}.md`

- 日期 → 按时间排序/归档
- slug → 从记忆内容前 3-4 个英文单词自动生成（小写，连字符分隔）

**分类目录**：由 `MemoryItem.Metadata["category"]` 驱动，支持 `personal / preferences / technical / facts / custom`。

### 4.2 单文件格式（YAML front matter + Markdown body）

```markdown
---
id: mem_3f7a2b1c
user_id: leading
category: personal
tags: [name, identity]
score: 0.85
hash: sha256:a1b2c3d4
created_at: 2026-06-10T14:30:00Z
updated_at: 2026-06-10T14:30:00Z
---

# User Name

The user's name is Bob. He prefers to be called Bob rather than Robert.
```

- `---` 包裹 YAML → Provider 解析得到 `MemoryItem` 全部结构化字段
- front matter 之后 → 自由格式的人类可读内容
- `id`：8 位 hex 随机串
- `hash`：`category + body` 的 SHA256 前 12 位，用于去重

### 4.3 索引机制

#### 内存索引（`IndexEntry`，启动时构建）

```go
type IndexEntry struct {
    ID        string
    UserID    string
    Category  string
    Tags      []string
    Score     float64
    Hash      string
    Title     string   // 从 body 第一行 # heading 提取
    FilePath  string   // 如 "user-leading/personal/2026-06-10-name-is-bob.md"
    UpdatedAt time.Time
}
```

**构建流程**：

```
启动 → Scan("data/memories/user-*/**/*.md")
     → 对每个 .md 仅解析 front matter（不读 body）
     → 构建 map[userID][]IndexEntry
     → 写入 index.json 快照
```

#### `index.json` 持久化快照

每次写操作后异步更新。启动时优先从 `index.json` 加载（O(1)），再校验文件是否存在（清理已删除条目）。

#### 每用户 `INDEX.md`（人类可读）

```markdown
# Memories for leading

## personal
- [User Name](personal/2026-06-10-name-is-bob.md) — score: 0.85

## preferences
- [Favorite Model](preferences/2026-06-10-favorite-model.md) — score: 0.70
```

仅面向人类浏览和 git diff，不参与程序检索。

---

## 5. 记忆写入机制

### 唯一触发方式：`remember_fact` 工具调用

Agent 在 ReAct 推理循环中，由 LLM 决定何时调用：

```
用户: "My name is Bob"
  → Agent 推理: "用户告诉我名字了，要不要记？system prompt 说遇到个人信息应该记"
  → Agent 调用工具: remember_fact(category="personal", content="...")
  → 工具处理器写入: data/memories/user-leading/personal/2026-06-11-name-is-bob.md
  → Agent 收到 result: "Memory saved (id=mem_3f7a2b1c)"
  → Agent 继续推理，生成最终回复
```

### 为什么不用 `<!-- MEMORY: -->` 注释

| | 工具调用 | HTML 注释 |
|---|---|---|
| 额外 round-trip | 有（1 次 tool_call + tool_result） | 无 |
| 格式可靠性 | 结构化字段，无解析错误 | 依赖 LLM 输出正确格式 |
| 反馈闭环 | tool_result 可反馈去重/更新结果 | 无反馈 |
| 可观测 | 工具调用日志 = 审计追踪 | 需从 raw content 中 grep |
| 按需触发 | LLM 觉得该记才调 | 每轮都追加注释（即使没学新东西） |

### 去重逻辑

```
Remember(category, content):
  1. hash = SHA256(category + content)[:12]
  2. existing = GetByHash(userID, hash)
  3. if existing != nil:
       existing.Score = min(existing.Score + 0.05, 1.0)  // 强化已有记忆
       existing.UpdatedAt = now
       Update(existing)
       return "Memory reinforced (existing)"
  4. else:
       item = new MemoryItem(content, category, hash, score=0.5)
       Create(item)
       return "Memory saved (new)"
```

---

## 6. 与 AgentService 的集成

```
AgentService.StreamChat() 流程:
  1. GetOrCreate session                              // 已有
  2. memSvc.Recall(userID, userMessage, 5)            // 新增：召回 top-5 相关记忆
  3. 注入 system_prompt:
     "## User Memories\n- Bob's name is Bob\n- Uses deepseek-chat\n"
  4. ExecuteStreaming(model, systemPrompt+memories, history, message)
  5. memSvc.Remember(userID, assistantContent)        // 新增：提取工具写入的记忆（若调用）
  6. Append assistant msg                              // 已有
```

### `MemoryService.Recall(query, limit)`

1. 调用 `Provider.Search(userID, query, limit)`
2. 按 Score 降序排序
3. 格式化为文本行：

```text
## Relevant Memories
- Bob is a backend developer working on a Go project (score: 0.85)
- Bob prefers concise responses without emojis (score: 0.70)
```

4. 注入 `updated.SystemPrompt` 头部

### `MemoryService.Remember(userID, text)`

目前为占位方法。`remember_fact` 工具直接调 `Provider.Create/Update`，无需通过此方法。保留该方法供未来非工具场景使用。

---

## 7. 并发控制

| 层级 | 策略 |
|---|---|
| **文件写入** | 同一 user 的对话在 session 内串行，不存在并发写同一记忆 |
| **去重** | `GetByHash` + `Create` 之间可能有多请求竞态；用 per-user `sync.Mutex` 保护 |
| **索引更新** | 写操作后同步更新内存索引，异步写 `index.json`（不阻塞请求） |

---

## 8. 版本控制

| 维度 | 机制 |
|---|---|
| **Git 追踪** | `data/memories/` 目录加入 git（`.md` + `INDEX.md`），`index.json` gitignored |
| **内容更新** | `Update` 覆盖写 `.md` 文件，`git diff` 可见差异 |
| **回滚** | `git checkout -- data/memories/user-*/` |
| **乐观锁** | 不需要——单 user 串行写，无冲突场景 |

---

## 9. 文件规划

```
memory/
├── memory_item.go            # 已有，MemoryItem 数据模型
├── provider.go               # Provider 接口 + 错误定义
├── markdown_provider.go      # Markdown 文件存储实现（生产默认）
├── inmemory_provider.go      # InMemory 实现（单元测试用）
├── index.go                  # 索引结构 + index.json 读写 + 启动重建

services/
├── memory_service.go         # MemoryService: Recall / Remember

tools/
└── remember_fact.go          # 新增工具：Agent 调用写入长期记忆

data/memories/                # 运行时数据（git tracked）
├── .gitkeep
├── index.json                # git ignored
└── user-leading/
    ├── INDEX.md
    └── ...
```

---

## 10. 装配链（cmd/gateway/main.go）

```go
// 步骤 1-2：已有
repo := session.NewSQLiteRepository(dbPath)
mgr := session.NewManager(repo, session.WithTTL(24*time.Hour))

// 步骤 3：新增 Memory 链路
memProvider := memory.NewMarkdownProvider("data/memories")
memSvc := services.NewMemoryService(memProvider)

// 步骤 4：服务层
sessSvc := services.NewSessionService(mgr)
svc := services.NewAgentService(agent, model, sessSvc, memSvc)

// 步骤 5：Handler
ah := handlers.NewAgentHandler(svc, sessSvc)
```

---

## 11. 后续演进

| 阶段 | 内容 | 状态 |
|---|---|---|
| **Phase 1** | Markdown Provider + MemoryService.Recall + `remember_fact` 工具 | 已完成 |
| **Phase 2** | System prompt 注入召回记忆，Agent 多轮对话中体现"记忆" | 进行中 — Recall 注入已实现，LLM 调用工具取决于 prompt 工程 |
| **Phase 3** | Vector Provider（Milvus / pgvector），Search 走 embedding 语义相似度 | 未开始 |
| **Phase 4** | 记忆衰减：Score 按 TTL 线性衰减；低分记忆定时归档 | 未开始 |
| **Phase 5** | 记忆摘要：同一 category 多条相似记忆合并为一条概括性记忆 | 未开始 |

### 当前实现对照

| 设计文件 | 实际文件 | 说明 |
|---|---|---|
| `memory/provider.go` | [memory/provider.go](file:///Users/leading/Developer/Projects/leadingAgent/memory/provider.go) | Provider 接口 + ErrNotFound/ErrDuplicate/ErrInvalid |
| `memory/markdown_provider.go` | [memory/markdown_provider.go](file:///Users/leading/Developer/Projects/leadingAgent/memory/markdown_provider.go) | YAML front matter + body，index.json 快照 + 扫描重建 |
| `memory/inmemory_provider.go` | [memory/inmemory_provider.go](file:///Users/leading/Developer/Projects/leadingAgent/memory/inmemory_provider.go) | 测试用内存实现 |
| `memory/index.go` | [memory/index.go](file:///Users/leading/Developer/Projects/leadingAgent/memory/index.go) | IndexEntry + byID/byHash/byUser 索引 + index.json 读写 |
| `services/memory_service.go` | [services/memory_service.go](file:///Users/leading/Developer/Projects/leadingAgent/services/memory_service.go) | Recall（关键词搜索 → 格式化注入）+ Remember（hash 去重） |
| `tools/remember_fact.go` | [agent/tools/remember_fact.go](file:///Users/leading/Developer/Projects/leadingAgent/agent/tools/remember_fact.go) | Agent 第 8 个工具，通过 WriteMemoryFunc 回调写入 |

### 装配链（已实现）

```go
// cmd/gateway/main.go
memProvider := memory.NewMarkdownProvider("data/memories")
memSvc := services.NewMemoryService(memProvider)
tools.SetWriteMemoryFunc(memSvc.Remember)

sessSvc := services.NewSessionService(mgr)
svc := services.NewAgentService(agent, model, sessSvc, memSvc)
ah := handlers.NewAgentHandler(svc, sessSvc)
```

### Phase 2 剩余工作

LLM 当前尚未主动调用 `remember_fact` 工具。原因：DeepSeek 模型在非显式要求时倾向于直接回复而非调用非搜索类工具。待解决方向：

1. 强化 system prompt 措辞（"IMPORTANT: You MUST call remember_fact when you learn..."）
2. 或切换到指令跟随能力更强的模型版本
