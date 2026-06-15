# Agent Session 管理系统设计文档

> 版本：v0.1 · 状态：已实现并通过 `go test`

本文档描述 `leadingAgent` 项目的对话会话（Session）管理子系统的设计目标、核心组件、数据结构、状态机、交互流程、过期策略、持久化方案与安全考量，并给出当前实现与后续演进方向。

实现入口：[session/](file:///Users/leading/Developer/Projects/leadingAgent/session)

***

## 1. 设计目标

1. **保留多轮对话上下文**：让 Agent 的 `Chat` / `StreamChat` 具备跨请求的消息历史。
2. **可插拔存储**：不与具体数据库绑定；单机用内存、部署用 Postgres/Redis 可切换。
3. **可观察与可审计**：对工具调用、错误次数、token 用量有统一的承载字段。
4. **并发安全**：同一会话被多协程写入时不互相覆盖；不同会话互不阻塞。
5. **安全性**：防止水平越权（用户 A 读写用户 B 的会话）、防止 sessionId 枚举。
6. **可扩展**：后续接入持久化、WAL、成本统计、会话摘要等能力无需大改。

***

## 2. 架构分层

```
 ┌──────────────────────────┐
 │  外部入口 (HTTP/Thrift)  │
 │  handlers/agent_handler  │
 └───────────┬──────────────┘
             │ 会话语义委托
 ┌───────────▼──────────────┐
 │  session.Manager         │  生命周期 / 并发控制 / 状态机 / GC
 └───────────┬──────────────┘
             │ Repository 接口
 ┌───────────▼──────────────┐
 │  session.Repository      │  Create/Get/Update/Delete/ListByUser/ListExpired/Touch
 └───────────┬──────────────┘
             │ 实现层
 ┌───────────▼──────────────┐
 │ InMemory / Redis / PG /  │
 │ SQLite / WAL + replay    │
 └──────────────────────────┘
```

- **Manager**：业务语义（过期、错误计数、配额、用户隔离、状态机）。
- **Repository**：纯 CRUD 接口，实现只负责读写，不带业务逻辑。
- **Session**：数据载体；`Version` 字段承担乐观锁职责。

***

## 3. 核心组件

### 3.1 `session.Session`（状态载体）

| 字段                      | 类型                     | 说明                                  |
| ----------------------- | ---------------------- | ----------------------------------- |
| `ID`                    | `string`               | 128-bit 随机十六进制；不可枚举                 |
| `UserID`                | `string`               | 用于多租户隔离；未登录可填 `"anon"`              |
| `State`                 | `SessionState`         | 见 §4 状态机                            |
| `SystemPrompt`          | `string`               | 会话级 system prompt（可覆盖默认）            |
| `Messages`              | `[]foundation.Message` | 完整消息历史（user/assistant/tool）         |
| `ToolCalls`             | `[]ToolCallMeta`       | 工具调用审计：耗时/成功/错误                     |
| `ModelConfig`           | `ModelConfig`          | 模型、provider、temperature、max\_tokens |
| `MetaData`              | `map[string]any`       | 客户端自由字段                             |
| `TokenUsage`            | `TokenUsage`           | prompt / completion / total         |
| `ErrorCount`            | `int`                  | 连续失败计数；超过阈值 → `ERROR`               |
| `MaxMessages`           | `int`                  | 单会话最多消息数，防止无限增长                     |
| `CreatedAt / UpdatedAt` | `time.Time`            | 时间戳（UTC）                            |
| `ExpiresAt`             | `time.Time`            | 滑动过期时间                              |
| `Version`               | `int64`                | 乐观锁版本号，每次写 `+1`                     |

方法：

- `IsWritable() bool`：状态非终结且消息数未超过上限
- `AppendMessage(msgs...)`：追加并推进版本
- `Touch(ttl)`：刷新过期时间

参考实现：[session.go L72-L120](file:///Users/leading/Developer/Projects/leadingAgent/session/session.go#L72-L120)

### 3.2 `session.Repository`（存储抽象）

```go
type Repository interface {
    Create(ctx context.Context, s *Session) error
    Get(ctx context.Context, id string) (*Session, error)
    Update(ctx context.Context, s *Session) error   // 乐观锁：s.Version 须匹配
    Delete(ctx context.Context, id string) error
    ListByUser(ctx context.Context, userID string, limit int) ([]*Session, error)
    ListExpired(ctx context.Context, before time.Time, limit int) ([]*Session, error)
    Touch(ctx context.Context, id string, newExpiresAt time.Time) error
}
```

参考定义：[session.go L126-L134](file:///Users/leading/Developer/Projects/leadingAgent/session/session.go#L126-L134)

当前实现：`InMemoryRepository`（`sync.RWMutex` + `map[string]*Session`），已通过乐观锁冲突和并发测试。

### 3.3 `session.Manager`（生命周期管理）

构造：`NewManager(repo, ...opts)`，选项：

- `WithTTL(ttl)`：滑动过期窗口（默认 1h）
- `WithRenewWindow(d)`：续期宽限期（默认 7 天）；过期 7 天内的 session 仍可被恢复
- `WithMaxPerUser(n)`：单用户最多活跃会话（软上限，超出时最旧的自动进入 `COMPLETED`）
- `WithMaxMessages(n)`：单会话消息数上限
- `WithSystemPrompt(p)`：新建会话时使用的默认 system prompt

对外能力：

- `Create(ctx, userID)`：新建，立即落盘。
- `Get(ctx, id, userID)`：读取，强制用户一致性校验。
- `GetOrCreate(ctx, id, userID)`：**增强版**—按以下分支决策：
  1. 会话仍活跃 → 直接返回
  2. 会话过期 / 终态但在 `renewWindow` 内 → 调用 `Renew` 恢复（保留 ID，刷新 ExpiresAt）
  3. 会话超期太久 → 创建新 session，并在 `MetaData["replaced_from"]` 写入旧 ID 供上层生成摘要注入
- `Renew(ctx, id, userID)`：**新能力**—刷新 `ExpiresAt` 至 `now + ttl`，恢复至 `ACTIVE`；**仅当"过期超过 `renewWindow` 且 有内容"时**返回 `ErrGone`（空会话/未过期/在窗口内 均允许续期，参见 [session_manager.go:149-157](file:///Users/leading/Developer/Projects/leadingAgent/session/session_manager.go#L149-L157)）
- `Append(ctx, id, userID, msgs...)`：追加 + 刷新 `ExpiresAt` + 推进 `Version`，内部带乐观锁重试
- `Touch(ctx, id, userID)`：心跳刷活
- `Complete / Delete / ListByUser / RecordError`：终结态控制与审计
- 后台 `gcLoop`：每 60s 扫描 `ListExpired` 并标记为 `EXPIRED`

参考实现：[session\_manager.go](file:///Users/leading/Developer/Projects/leadingAgent/session/session_manager.go)

***

## 4. 状态机

```
          首条消息               交互后
 CREATED ──────────► ACTIVE ─────────────► ACTIVE (ExpiresAt 推进)
                        │
                        ├── 连续错误 ≥ N ─► ERROR
                        ├── 用户主动结束 ─► COMPLETED
                        ├── TTL 过期 ────► EXPIRED ──┐
                        └── 消息数超限 ───► COMPLETED  │
                                                        │
                                                        │  过期但在 renewWindow 内
                                     ︎                       ▲
                                                        │  Renew()
                                                        │
                           EXPIRED ──────────────────────┘

 COMPLETED / EXPIRED / ERROR ── 归档策略 ──► ARCHIVED
```

状态集合：

- `StateCreated`：已创建但未说话。
- `StateActive`：可写入。
- `StatePaused`：保留态（暂未暴露 API，后续可做"后台暂停/恢复"）。
- `StateCompleted`：用户或系统主动结束。
- `StateExpired`：TTL 到期，由 GC 或懒读取触发。
- `StateError`：连续失败超过阈值。
- `StateArchived`：已归档（冷存储，不可再写）。

终结判定：`SessionState.IsTerminal()`。参考：[session.go L37-L43](file:///Users/leading/Developer/Projects/leadingAgent/session/session.go#L37-L43)

***

## 5. 交互流程

### 5.1 正常单轮（同步 Chat）

```
 Client                   AgentHandler           SessionManager     Agent     Repo
   │  Chat(sessionId?,q)  │                       │                 │         │
   │──────────────────────►─ GetOrCreate(id,uid) ─┤                 │         │
   │                      │                       │─ Create/Get ───►│         │
   │                      │                       │◄── Session ─────│         │
   │                      │─ Append(user_msg) ───►                 │         │
   │                      │                       │─ Update ───────►│         │
   │                      │                       │                 │
   │                      │── Execute(ctx,q) ─────────────────────►│         │
   │                      │                       │                 │─ run ─►│(可接入工具调用/计费)
   │                      │                       │◄── response ────│         │
   │                      │─ Append(assistant_msg)─────────────────►│         │
   │◄─────────────────────│                       │                 │         │
   │   {response,sessionId}                       │                 │         │
```

参考代码：[handlers/agent\_handler.go Chat](file:///Users/leading/Developer/Projects/leadingAgent/handlers/agent_handler.go#L61-L99)

### 5.2 流式（StreamChat）

- 先 `GetOrCreate`：若旧 session 过期超 `renewWindow`，则创建新 session 并写入 `MetaData["replaced_from"]`
- **摘要注入（best effort）**：检测到 `MetaData["replaced_from"]` 非空 → 调 `Agent.Summarize()` 生成旧对话摘要 → 注入 system prompt
- `Append(user)`
- `ExecuteStreaming` 过程中收集最终文本 `assistantContent`
- 结束后 `Append(assistant)`，保留历史
- 失败走 `RecordError(id, uid, 3)`

参考代码：[handlers/agent\_handler.go StreamChat](file:///Users/leading/Developer/Projects/leadingAgent/handlers/agent_handler.go#L102-L141)、[services/agent\_service.go StreamChat](file:///Users/leading/Developer/Projects/leadingAgent/services/agent_service.go#L40-L108)

***

## 6. 过期、续期与摘要策略

| 机制     | 说明                                            |
| ------ | --------------------------------------------- |
| 滑动 TTL | 每次 `Append/Touch` 更新 `ExpiresAt = now + ttl`  |
| 懒过期    | `Get` 时发现过期 → 直接改状态为 `EXPIRED`                |
| 续期窗口    | `renewWindow`（默认 7 天）：过期 7 天内的 session 可被 `Renew()` 恢复，避免"稍过期即丢上下文" |
| 摘要注入    | 当 session 过期超 `renewWindow` 时，`GetOrCreate` 新建 session 并在 `MetaData["replaced_from"]` 写入旧 ID；AgentService 检测到此字段后，调用 `Agent.Summarize()` 生成旧对话的中文摘要，以 `## 上一段对话的摘要` 注入新 session 的 system prompt |
| 后台 GC  | `gcLoop` 每 60s 扫描 `ListExpired` 标记过期；扫描窗口 10s |
| 会话数上限  | 单用户超过 `MaxPerUser`：最旧会话被置 `COMPLETED`         |
| 消息数上限  | 单会话超过 `MaxMessages`：拒绝继续写入并置 `COMPLETED`      |
| 错误熔断   | `RecordError` 超过阈值 → `ERROR`                  |

### 续期决策表（GetOrCreate 行为）

```
session 状态                          | 结果
------------------------------------- | -----------------------------
  活跃（ExpiresAt > now，非 terminal） | 直接返回原 session
  过期 / 终态 且 在 renewWindow 内     | Renew → 原 ID + 新 ExpiresAt
  过期 > renewWindow                  | 新建 session + MetaData.replaced_from
  不存在（新用户）                    | 新建 session
```

### Renew 内部决策（精确条件）

`Renew` 的唯一拒绝条件：**已过期超过 `renewWindow` 且 `Messages` 非空**。其他情况一律允许续期：

| 场景 | `diff = now - ExpiresAt` | `hasMessages` | Renew 结果 |
|---|---|---|---|
| 尚未过期（还有 30 天） | < 0 | true | ✅ 允许续期 |
| 即将过期（还有 3 天） | < 0 | true | ✅ 允许续期 |
| 过期 3 天（在窗口内） | +3d | true | ✅ 允许续期 |
| 过期 30 天（超窗口，有内容） | +30d | true | ❌ `ErrGone` |
| 过期 30 天（空会话） | +30d | false | ✅ 允许续期 |

- 伪代码：`if hasMessages && diff > 0 && diff >= renewWindow → ErrGone`
- 实现位置：[session_manager.go:149-157](file:///Users/leading/Developer/Projects/leadingAgent/session/session_manager.go#L149-L157)
- 备注：旧版本条件 `!withinWindow && !willExpireSoon && hasMessages` 存在漏洞——未过期且距离到期还很远的会话会被错误拒绝，已修复为上述单一明确条件。

### 摘要生成（Agent.Summarize）

- **位置**：[agent/agent.go Summarize](file:///Users/leading/Developer/Projects/leadingAgent/agent/agent.go#L140-L212)
- **调用时机**：`AgentService.StreamChat/Chat` 中检测到 `MetaData["replaced_from"]` 非空
- **调用方式**：直接调 `deepseek.Chat`（空工具列表，不触发 `remember_fact` 等工具）
- **参数**：`messages[]`（旧 session 全部消息）、`maxChars=500`
- **产物**：中文简明摘要，以 `## 上一段对话的摘要` 段落拼入 system prompt
- **失败策略**：best effort — 摘要生成失败不阻塞主对话，仅打 warning 日志

参考：[services/agent_service.go StreamChat](file:///Users/leading/Developer/Projects/leadingAgent/services/agent_service.go#L40-L108)

***

## 7. 并发控制

1. **per-session 互斥**：`Manager.lockFor(id)` 按 ID 懒加载 `sync.Mutex`；同会话串行化，不同会话互不阻塞。
2. **乐观锁**：`Session.Version` 每次写 `+1`；`Repository.Update` 实现若版本不匹配返回 `ErrConflict`；`Manager.Append` 内部最多重试 3 次并指数退避。
3. **锁池回收**：当 `locks` 超过 2000 项时在 GC 协程里整体重建，避免长期运行内存慢漏。

***

## 8. 持久化与异常恢复

当前状态：**已完成接口，提供** **`InMemoryRepository`**。后续可按需扩展：

1. **单机部署**：`SQLiteRepository`（项目已引入 `modernc.org/sqlite`），表：
   - `sessions(id, user_id, state, system_prompt, created_at, updated_at, expires_at, version)`
   - `messages(id, session_id, role, content, tool_call_id, idx, created_at)`
   - `tool_calls(id, session_id, tool_name, latency_ms, success, error, created_at)`
2. **多实例部署**：Redis（热 session + TTL）+ Postgres（冷/归档）。
3. **WAL 写前日志**：每次 Update 先 append JSON 行到 `session.log`，再写主表；启动时回放未完成的写，解决 panic 导致刚生成的会话丢失问题。
4. **完整性**：每条 Session 可以附带 `sha256(content + secret)` 签名，加载时校验。

***

## 9. 安全性

| 维度            | 策略                                                                                     |
| ------------- | -------------------------------------------------------------------------------------- |
| 水平越权          | 所有 `Get/Append/Touch/Complete/Delete/RecordError` 强制校验 `userID` 一致；不匹配返回 `ErrNotFound` |
| SessionId 随机性 | `crypto/rand` 生成 128-bit，避免自增 ID 被枚举                                                   |
| 防会话伪造         | （可选）服务端返回 `sessionId.hmac(secret)`，下次请求校验                                              |
| 敏感数据擦除        | `MetaData` / `Messages` 写入前可挂 hook 做 PII 正则擦除                                          |
| 错误泄露          | 对外错误统一走 `ErrNotFound / ErrExpired / ErrGone / ErrInvalid`，不暴露底层存储错误                    |

***

## 10. 性能与可扩展建议

- **写路径批处理**：对极高 QPS 场景，把 Append 的 DB 写入放到后台 worker，以 ttl（如 500ms）+ batch size 合并落盘；请求路径只写 WAL/内存。
- **历史消息滚动窗口**：对较长会话，将前 N 轮 `assistant/user` 对压缩为一段摘要再喂模型，控制 token 爆炸。
- **只读副本**：`ListByUser` 与审计查询走只读从库，不打主写。
- **singleflight**：同一 sessionId 短时间被多次读取时，`golang.org/x/sync/singleflight` 抑制重复读。
- **指标**：`session_ops_total{op=append|get|create}`、`session_error_total{reason=expired|conflict|gone}`、`tool_call_latency_ms`、`token_usage_total` 接入 Prometheus。

***

## 11. 目录与代码

```
session/
├── session.go              # Session 数据结构 + Repository 接口 + InMemory 实现
├── session_manager.go      # Manager（生命周期/并发/GC）
└── session_manager_test.go # 单测：创建、追加、过期、终结、GC

handlers/
└── agent_handler.go        # Chat/StreamChat 已接入 SessionManager
```

运行：

```bash
go build ./...
go test  ./session/... -count=1
```

***

## 12. 后续演进路线（建议按序）

1. **SQLite Repository 实现**：使用现有 `modernc.org/sqlite` 依赖，把 session 落盘，取代重启丢历史的问题。DONE
2. **Agent 真正消费历史消息**：把 `Session.Messages` 注入 `agent.Agent.Execute(...)`，让多轮对话"真的能记得前文"。DONE
3. **HMAC 签名 sessionId**：防止客户端篡改。
4. **会话摘要（已实现）**：当 session 过期超过 `renewWindow` 时，通过 `Agent.Summarize()` 生成旧对话的中文摘要，注入新 session 的 system prompt，实现上下文平滑迁移。DONE（见 §6、[agent/agent.go Summarize](file:///Users/leading/Developer/Projects/leadingAgent/agent/agent.go#L140-L212)）
5. **指标 & 审计**：让 `ToolCallMeta / TokenUsage` 与已有 `repository.CostRepository` 打通。
6. **WAL + 回放启动**：为多实例部署下的写可靠性兜底。
7. **工作记忆窗口管理**：当 `len(Messages)` 逼近模型 context window 时，对早期轮次做自动摘要压缩（summarize early turns），替换原始消息以控制 token 消耗，避免简单截断导致上下文丢失。
8. **长期语义记忆（Semantic Memory）**：Agent 增加 `remember` 工具，将用户偏好、身份信息、常用配置等关键事实写入 `Session.MetaData` 或独立的 `user_facts` 表；每次 `GetOrCreate` 时自动检索并注入 system prompt，实现跨 session 的个性化记忆。
9. **情节记忆（Episodic Memory）**：引入 embedding 模型 + 向量数据库（如 Milvus / Pinecone / pgvector），对每段对话生成摘要 → 向量化 → 存入向量库。后续对话按语义相似度召回相关历史片段，作为额外上下文注入 LLM。
10. **过程记忆（Procedural Memory / Skill）**：对高频业务流程（如"处理退款"、"生成周报"）抽象为可复用的工作流定义，支持 Agent 按意图匹配后加载对应 procedure，实现从"每次推理"到"模式复用"的跃升。
11. **记忆优先级缓存**：高频访问的热点 session / user_facts 加一层 `sync.Map` 内存缓存，减少 SQLite 查询；缓存 TTL 与 session TTL 对齐，写穿透（write-through）保证一致性。
12. **续期窗口可观测性**：`GetOrCreate` 触发 Renew / 摘要注入的次数接入 metrics（`session_renewals_total`、`session_summaries_total`），便于观察过期策略是否合理。

