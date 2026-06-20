# Token Dashboard 设计文档

**日期：** 2026-06-20  
**状态：** 待实现

---

## 背景

目前项目中 token 消耗统计不完整：

- `ldagent_handler.go` 的直接 chat/stream 调用已持久化到 `cost.db`
- `Agent` 的每次 LLM 调用（含 critic）只在内存中累加，进程结束后丢失
- `cost.db` 缺少 `session_id` 字段，无法按 session 查询明细

本次工作分两部分：**补全持久化** + **新建 Token Dashboard 页面**。

---

## 一、数据库 Schema 变更

### `token_costs` 表新增字段

```sql
ALTER TABLE token_costs ADD COLUMN session_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_token_costs_session_id ON token_costs(session_id);
```

### `models.TokenCost` 同步更新

```go
type TokenCost struct {
    // 现有字段不变
    SessionID        string    `json:"session_id"`  // 新增
}
```

---

## 二、持久化补全

### 2.1 Agent 持久化回调

`Agent` 不直接依赖 `repository` 包，通过回调函数注入持久化能力：

```go
// agent/agent.go
type CostSaver func(caller, model, sessionID string, prompt, completion int)

type Agent struct {
    // 现有字段不变
    saveCost CostSaver // 可为 nil，nil 时跳过持久化
}

func (a *Agent) WithCostSaver(fn CostSaver) *Agent {
    a.saveCost = fn
    return a
}
```

`accumulateUsage` 修改：

```go
func (a *Agent) accumulateUsage(caller, model string, prompt, completion int) {
    // 现有内存累加逻辑不变
    if a.saveCost != nil {
        go a.saveCost(caller, model, a.sessionID, prompt, completion)
    }
}
```

`agent.go` 需持有 `sessionID` 字段（构造时传入）。

### 2.2 critic.go

无需改动。`reviewFinalAnswer` 已调用 `accumulateUsage`，回调会自动触发。

### 2.3 service 层注入

在创建 `Agent` 实例时注入 `CostSaver`：

```go
agent.New(...).WithCostSaver(func(caller, model, sessionID string, prompt, completion int) {
    cost := &models.TokenCost{
        RequestID:        uuid.New().String(),
        SessionID:        sessionID,
        Provider:         resolveProvider(model),
        Model:            model,
        RequestType:      caller, // "callModel" / "callModelStreaming" / "critic"
        Endpoint:         "",
        PromptTokens:     prompt,
        CompletionTokens: completion,
        TotalTokens:      prompt + completion,
    }
    costRepo.Save(cost)
})
```

### 2.4 Session usage 回写

`StreamEventDone` 事件触发时，service 层将 `UsageSummary()` 写回 session 的 `token_usage` 字段（`session/sqlite_repository.go` 已有该列）。

### 2.5 ldagent_handler.go 补充 session_id

现有的直接 chat/stream 调用写入 `cost.db` 时，从 request 中取 `SessionId` 填入 `TokenCost.SessionID`。

---

## 三、后端 HTTP 接口

确认或新增：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/costs` | 返回全量 `token_costs` 记录，按 `created_at` 倒序 |
| GET | `/api/costs/total` | 返回总 token 数（复用 `GetTotalTokens()`） |

`/api/costs` 响应结构：

```json
{
  "costs": [
    {
      "id": "...",
      "session_id": "...",
      "provider": "deepseek",
      "model": "deepseek-chat",
      "request_type": "callModel",
      "prompt_tokens": 120,
      "completion_tokens": 80,
      "total_tokens": 200,
      "created_at": "2026-06-20T10:00:00Z"
    }
  ]
}
```

---

## 四、前端路由重构

### 4.1 引入 React Router v7

```
npm install react-router-dom
```

### 4.2 文件结构

```
src/
├── main.tsx                      # 挂载 RouterProvider
├── router.tsx                    # 路由定义（新建）
├── layouts/
│   └── AppLayout.tsx             # 顶部导航栏 + <Outlet />（新建）
├── pages/
│   ├── ChatPage.tsx              # App.tsx 聊天逻辑迁移
│   └── TokenDashboardPage.tsx    # 新建
├── components/
│   ├── ChatMessage.tsx           # 不动
│   └── ChatInput.tsx             # 不动
└── App.tsx                       # 简化为 <RouterProvider router={router} />
```

### 4.3 路由表

```
/          → redirect to /chat
/chat      → ChatPage
/tokens    → TokenDashboardPage
```

### 4.4 AppLayout 导航栏

- 固定顶部，高度 48px
- 左侧：应用名
- 右侧：「聊天」「Token 统计」两个 `<NavLink>`，当前页高亮
- 聊天页的 session 侧边栏保留在 `ChatPage` 内部，不提升到 Layout

---

## 五、Token Dashboard 页面

### 5.1 汇总卡片（顶部）

4 张横排卡片，数据来自 `/api/costs` 前端聚合：

| 卡片 | 数据 |
|------|------|
| 总 Prompt Tokens | `SUM(prompt_tokens)` |
| 总 Completion Tokens | `SUM(completion_tokens)` |
| 总 Token 消耗 | `SUM(total_tokens)` |
| 请求总次数 | `COUNT(*)` |

### 5.2 时间趋势图（中部）

- Recharts `LineChart`
- X 轴：日期（按天聚合）
- Y 轴：token 数量
- 两条线：prompt tokens / completion tokens
- 时间范围切换：最近 7 天 / 30 天（前端过滤）

### 5.3 明细表格（下部）

两个 Tab：

**按 Session**

| 列 | 内容 |
|----|------|
| Session ID | 前 8 位 |
| 请求次数 | COUNT |
| Prompt Tokens | SUM |
| Completion Tokens | SUM |
| Total Tokens | SUM |
| 最后请求时间 | MAX(created_at) |

**按模型**

| 列 | 内容 |
|----|------|
| 模型名 | model |
| Provider | provider |
| 请求次数 | COUNT |
| Prompt Tokens | SUM |
| Completion Tokens | SUM |
| Total Tokens | SUM |

所有聚合在前端完成，无需额外后端接口。

---

## 六、实现范围汇总

| 模块 | 改动内容 |
|------|---------|
| `cost.db` schema | 新增 `session_id` 列和索引 |
| `models/cost.go` | 新增 `SessionID` 字段 |
| `repository/cost_repository.go` | `Save`/`GetAll` 支持 `session_id` |
| `agent/agent.go` | 新增 `CostSaver` 回调、持有 `sessionID`、`accumulateUsage` 触发回调 |
| `services/` | 注入 `CostSaver`；`StreamEventDone` 回写 session usage |
| `handlers/ldagent_handler.go` | 写入 `cost.db` 时填入 `session_id` |
| `gateway/` 或 `handlers/` | 新增/确认 `GET /api/costs` 路由 |
| `frontend/` | 引入 React Router v7，重构路由，新建 `TokenDashboardPage` |

---

## 七、不在本次范围内

- token 消耗的价格换算（成本估算）
- 实时 WebSocket 推送 token 数据
- 用户级别的 token 配额限制
