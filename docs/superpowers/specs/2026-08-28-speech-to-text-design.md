# 语音转文字（Speech-to-Text）功能设计

## 概述

为 Agent 系统新增语音转文字能力，用户可以上传本地音频文件（MP3/WAV/M4A），通过火山引擎豆包大模型录音文件识别服务将语音转录为文字。该功能作为独立 Service 对外提供 HTTP API，并在前端页面有独立入口。

## 技术选型

- **语音识别引擎**：火山引擎豆包大模型录音文件识别（volc.seedasr.auc）
- **音频中转**：火山引擎 TOS（对象存储）— 先上传音频到 TOS 获取 URL，再提交 ASR 任务
- **方言支持**：不指定 language 参数，模型自动识别中英文 + 上海话等方言
- **音频格式**：MP3、WAV、M4A

## 模块结构

新增 `speech` 模块，遵循项目已有的 Provider 接口 + Service 模式：

```
speech/
├── provider.go       # Uploader 接口定义
├── tos_uploader.go   # TOS 上传实现
├── asr_client.go     # 火山引擎 ASR 客户端（提交任务 + 轮询结果）
├── recognizer.go     # 语音识别 Service（编排层）
└── handler.go        # HTTP Handler
```

与现有模块独立，在 `cmd/gateway/main.go` 中注册 HTTP 路由。

## 架构与数据流

```
HTTP POST /api/speech/transcribe (multipart/form-data, file=<audio>)
  → Handler（接收文件，保存临时文件）
    → Recognizer.Transcribe(ctx, filePath)
      → Uploader.Upload(filePath) → TOS URL
      → ASRClient.Submit(audioURL) → taskID
      → 轮询 ASRClient.Query(taskID) → 识别文本
    → 返回 JSON: { "text": "...", "duration_ms": 12345 }
  → Handler 清理临时文件
```

## 接口设计

### Uploader 接口

```go
type Uploader interface {
    Upload(ctx context.Context, filePath string) (url string, err error)
}
```

### ASR 客户端

```go
type ASRClient struct { ... }

func (c *ASRClient) Submit(ctx context.Context, audioURL string) (taskID string, err error)
func (c *ASRClient) Query(ctx context.Context, taskID string) (text string, err error)
```

### Recognizer（编排层）

```go
type Recognizer struct {
    uploader Uploader
    asr      *ASRClient
}

func (r *Recognizer) Transcribe(ctx context.Context, filePath string) (text string, err error)
```

### HTTP API

```
POST /api/speech/transcribe
Content-Type: multipart/form-data
Body: file=<audio file>
```

成功响应：
```json
{
  "text": "识别结果文本...",
  "duration_ms": 12345
}
```

错误响应：
```json
{
  "error": "错误描述"
}
```

## 配置

通过 viper 管理，在 `config.yaml` 中配置：

```yaml
speech:
  tos:
    endpoint: "https://tos-cn-beijing.volces.com"
    region: "cn-beijing"
    access_key: "${TOS_ACCESS_KEY}"
    secret_key: "${TOS_SECRET_KEY}"
    bucket: "speech-audio-temp"
  asr:
    app_key: "${VOLC_ASR_APP_KEY}"
    resource_id: "volc.seedasr.auc"
    poll_interval: 2s
    poll_timeout: 300s
```

## 火山引擎 API 对接

### 提交任务

- 方法：POST
- 地址：`https://openspeech.bytedance.com/api/v3/auc/bigmodel/submit`
- Header：`X-Api-Key`、`X-Api-Resource-Id`、`X-Api-Request-Id`（UUID）、`X-Api-Sequence: -1`
- Body：JSON，包含 `audio.url`、`audio.format`、`request.model_name: "bigmodel"`

### 查询结果

- 方法：POST
- 地址：`https://openspeech.bytedance.com/api/v3/auc/bigmodel/query`
- 用同一个 `X-Api-Request-Id`（taskID）轮询
- 轮询间隔 2s，超时 300s

## 错误处理

| 阶段 | 错误类型 | HTTP 状态码 | 处理方式 |
|------|---------|------------|---------|
| 文件接收 | 格式不支持、文件过大 | 400 | 返回明确错误信息 |
| TOS 上传 | 网络超时、认证失败 | 502 | 重试 2 次后失败 |
| ASR 提交 | API 错误、配额不足 | 502 | 透传火山引擎错误信息 |
| ASR 轮询 | 超时未完成 | 504 | taskID 记录日志供排查 |
| 临时文件 | 任何情况 | N/A | defer 清理 |

## 依赖

- `github.com/volcengine/ve-tos-golang-sdk/v2` — TOS SDK
- `github.com/go-resty/resty/v2` — HTTP 客户端（已存在）
- `github.com/google/uuid` — 生成 taskID（已存在）
- `github.com/spf13/viper` — 配置管理（已存在）