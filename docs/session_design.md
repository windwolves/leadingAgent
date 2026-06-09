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
- `WithMaxPerUser(n)`：单用户最多活跃会话（软上限，超出时最旧的自动进入 `COMPLETED`）
- `WithMaxMessages(n)`：单会话消息数上限
- `WithSystemPrompt(p)`：新建会话时使用的默认 system prompt

对外能力：

- `Create(ctx, userID)`：新建，立即落盘。
- `Get(ctx, id, userID)`：读取，强制用户一致性校验。
- `GetOrCreate(ctx, id, userID)`：便捷方法，客户端 `sessionId` 为空时自动创建。
- `Append(ctx, id, userID, msgs...)`：追加 + 刷新 `ExpiresAt` + 推进 `Version`，内部带乐观锁重试。
- `Touch(ctx, id, userID)`：心跳刷活。
- `Complete / Delete / ListByUser / RecordError`：终结态控制与审计。
- 后台 `gcLoop`：每 60s 扫描 `ListExpired` 并标记为 `EXPIRED`。

参考实现：[session\_manager.go](file:///Users/leading/Developer/Projects/leadingAgent/session/session_manager.go)

***

## 4. 状态机

```
          首条消息               交互后
 CREATED ──────────► ACTIVE ─────────────► ACTIVE (ExpiresAt 推进)
                        │
                        ├── 连续错误 ≥ N ─► ERROR
                        ├── 用户主动结束 ─► COMPLETED
                        ├── TTL 过期 ────► EXPIRED
                        └── 消息数超限 ───► COMPLETED

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

- 先 `GetOrCreate` / `Append(user)`
- `ExecuteStreaming` 过程中收集最终文本 `assistantContent`
- 结束后 `Append(assistant)`，保留历史
- 失败走 `RecordError(id, uid, 3)`

参考代码：[handlers/agent\_handler.go StreamChat](file:///Users/leading/Developer/Projects/leadingAgent/handlers/agent_handler.go#L102-L141)

***

## 6. 过期与回收策略

| 机制     | 说明                                            |
| ------ | --------------------------------------------- |
| 滑动 TTL | 每次 `Append/Touch` 更新 `ExpiresAt = now + ttl`  |
| 懒过期    | `Get` 时发现过期 → 直接改状态为 `EXPIRED`                |
| 后台 GC  | `gcLoop` 每 60s 扫描 `ListExpired` 标记过期；扫描窗口 10s |
| 会话数上限  | 单用户超过 `MaxPerUser`：最旧会话被置 `COMPLETED`         |
| 消息数上限  | 单会话超过 `MaxMessages`：拒绝继续写入并置 `COMPLETED`      |
| 错误熔断   | `RecordError` 超过阈值 → `ERROR`                  |

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
4. **会话摘要**：在消息数达到阈值时，让模型生成一次「上下文摘要」，替换早期消息。
5. **指标 & 审计**：让 `ToolCallMeta / TokenUsage` 与已有 `repository.CostRepository` 打通。
6. **WAL + 回放启动**：为多实例部署下的写可靠性兜底。

