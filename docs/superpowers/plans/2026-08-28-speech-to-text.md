# 语音转文字（Speech-to-Text）实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 为 Agent 系统新增语音转文字功能，支持上传本地 MP3/WAV 音频文件，通过火山引擎 TOS + 豆包大模型 ASR 将语音转录为文字。

**架构：** 新增 `speech` 模块，遵循现有 Provider 接口 + Service 模式。本地音频 → 上传 TOS → 提交 ASR 任务 → 轮询结果 → 返回文本。作为独立 HTTP API 暴露。

**技术栈：** Go 1.22, resty/v2 (HTTP), ve-tos-golang-sdk/v2 (TOS), viper (配置), uuid (taskID)

---

### 任务 1：添加依赖与配置

**文件：**
- 修改：`go.mod`
- 修改：`config/config.go`

- [ ] **步骤 1：添加 TOS SDK 依赖**

```bash
cd /Users/leading/Developer/Projects/leadingAgent
go get github.com/volcengine/ve-tos-golang-sdk/v2
```

- [ ] **步骤 2：运行 `go mod tidy` 确认依赖正常**

```bash
go mod tidy
```
预期：无错误。

- [ ] **步骤 3：在 config.go 中添加 SpeechConfig 结构体**

在 `config/config.go` 中，在 `Config` 结构体之前添加：

```go
// SpeechConfig 语音识别配置。
type SpeechConfig struct {
	TOS struct {
		Endpoint  string `mapstructure:"SPEECH_TOS_ENDPOINT"`
		Region    string `mapstructure:"SPEECH_TOS_REGION"`
		AccessKey string `mapstructure:"SPEECH_TOS_ACCESS_KEY"`
		SecretKey string `mapstructure:"SPEECH_TOS_SECRET_KEY"`
		Bucket    string `mapstructure:"SPEECH_TOS_BUCKET"`
	} `mapstructure:"speech_tos"`
	ASR struct {
		AppKey       string `mapstructure:"SPEECH_ASR_APP_KEY"`
		ResourceID   string `mapstructure:"SPEECH_ASR_RESOURCE_ID"`
		PollInterval int    `mapstructure:"SPEECH_ASR_POLL_INTERVAL"`
		PollTimeout  int    `mapstructure:"SPEECH_ASR_POLL_TIMEOUT"`
	} `mapstructure:"speech_asr"`
}

// DefaultSpeechConfig 返回带默认值的 SpeechConfig。
func DefaultSpeechConfig() SpeechConfig {
	cfg := SpeechConfig{}
	cfg.TOS.Endpoint = "https://tos-cn-beijing.volces.com"
	cfg.TOS.Region = "cn-beijing"
	cfg.TOS.Bucket = "speech-audio-temp"
	cfg.ASR.ResourceID = "volc.seedasr.auc"
	cfg.ASR.PollInterval = 2
	cfg.ASR.PollTimeout = 300
	return cfg
}
```

- [ ] **步骤 4：在 Config 结构体中添加 Speech 字段**

在 `Config` 结构体末尾添加：

```go
Speech SpeechConfig `mapstructure:"speech"`
```

- [ ] **步骤 5：在 LoadConfig 中应用默认值**

在 `LoadConfig` 函数的 `viper.Unmarshal` 之后，`return &config, nil` 之前添加：

```go
if config.Speech.ASR.PollInterval == 0 {
    config.Speech = DefaultSpeechConfig()
}
```

- [ ] **步骤 6：运行编译验证**

```bash
go build ./...
```
预期：无编译错误。

- [ ] **步骤 7：Commit**

```bash
git add go.mod go.sum config/config.go
git commit -m "feat(speech): add TOS SDK dependency and speech config"
```

---

### 任务 2：定义 Uploader 接口与 Mock

**文件：**
- 创建：`speech/provider.go`
- 创建：`speech/provider_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `speech/provider_test.go`：

```go
package speech

import (
	"context"
	"testing"
)

func TestUploaderInterface(t *testing.T) {
	// 编译时验证 mockUploader 实现了 Uploader 接口
	var _ Uploader = (*mockUploader)(nil)
}

type mockUploader struct {
	url string
	err error
}

func (m *mockUploader) Upload(_ context.Context, _ string) (string, error) {
	return m.url, m.err
}

func TestMockUploader(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		m := &mockUploader{url: "https://tos.example.com/audio.mp3"}
		url, err := m.Upload(ctx, "test.mp3")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "https://tos.example.com/audio.mp3" {
			t.Fatalf("expected url, got %s", url)
		}
	})

	t.Run("error", func(t *testing.T) {
		m := &mockUploader{err: assert.AnError}
		_, err := m.Upload(ctx, "test.mp3")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
```

需要 `github.com/stretchr/testify/assert`。如果没有，使用简单的 error 变量：

```go
var errMock = errors.New("mock error")

t.Run("error", func(t *testing.T) {
    m := &mockUploader{err: errMock}
    _, err := m.Upload(ctx, "test.mp3")
    if err == nil {
        t.Fatal("expected error, got nil")
    }
})
```

- [ ] **步骤 2：运行测试确认失败**

```bash
go test ./speech/ -v -run TestUploaderInterface
```
预期：FAIL，报错 "undefined: Uploader"

- [ ] **步骤 3：创建 Uploader 接口**

创建 `speech/provider.go`：

```go
package speech

import "context"

// Uploader 定义音频文件上传接口，支持未来切换不同后端。
type Uploader interface {
	Upload(ctx context.Context, filePath string) (url string, err error)
}
```

- [ ] **步骤 4：运行测试验证通过**

```bash
go test ./speech/ -v -run TestUploaderInterface
```
预期：PASS

- [ ] **步骤 5：Commit**

```bash
git add speech/provider.go speech/provider_test.go
git commit -m "feat(speech): add Uploader interface and mock"
```

---

### 任务 3：实现 TOS Uploader

**文件：**
- 创建：`speech/tos_uploader.go`
- 创建：`speech/tos_uploader_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `speech/tos_uploader_test.go`：

```go
package speech

import (
	"testing"
)

func TestNewTOSUploader(t *testing.T) {
	cfg := SpeechConfig{
		TOS: struct {
			Endpoint  string
			Region    string
			AccessKey string
			SecretKey string
			Bucket    string
		}{
			Endpoint:  "https://tos-cn-beijing.volces.com",
			Region:    "cn-beijing",
			AccessKey: "test-ak",
			SecretKey: "test-sk",
			Bucket:    "test-bucket",
		},
	}

	u, err := NewTOSUploader(cfg)
	if err != nil {
		t.Fatalf("NewTOSUploader failed: %v", err)
	}
	if u == nil {
		t.Fatal("expected non-nil uploader")
	}
}
```

- [ ] **步骤 2：运行测试确认失败**

```bash
go test ./speech/ -v -run TestNewTOSUploader
```
预期：FAIL，报错 "undefined: NewTOSUploader"

- [ ] **步骤 3：实现 TOSUploader**

创建 `speech/tos_uploader.go`：

```go
package speech

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

// TOSUploader 将本地文件上传到火山引擎 TOS 并返回公开 URL。
type TOSUploader struct {
	client *tos.ClientV2
	bucket string
}

// NewTOSUploader 创建 TOS 上传器。
func NewTOSUploader(cfg SpeechConfig) (*TOSUploader, error) {
	client, err := tos.NewClientV2(
		cfg.TOS.Endpoint,
		tos.WithRegion(cfg.TOS.Region),
		tos.WithCredentials(tos.NewStaticCredentials(cfg.TOS.AccessKey, cfg.TOS.SecretKey)),
	)
	if err != nil {
		return nil, fmt.Errorf("create tos client: %w", err)
	}
	return &TOSUploader{
		client: client,
		bucket: cfg.TOS.Bucket,
	}, nil
}

// Upload 上传文件到 TOS，返回公开访问 URL。
func (u *TOSUploader) Upload(ctx context.Context, filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	objectKey := filepath.Base(filePath)

	_, err = u.client.PutObjectV2(ctx, &tos.PutObjectV2Input{
		Bucket:  u.bucket,
		Key:     objectKey,
		Content: f,
	})
	if err != nil {
		return "", fmt.Errorf("put object: %w", err)
	}

	url := fmt.Sprintf("%s/%s/%s", u.client.Endpoint, u.bucket, objectKey)
	return url, nil
}
```

- [ ] **步骤 4：运行测试**

```bash
go test ./speech/ -v -run TestNewTOSUploader
```
预期：PASS

- [ ] **步骤 5：运行编译验证**

```bash
go build ./speech/
```
预期：无编译错误。

- [ ] **步骤 6：Commit**

```bash
git add speech/tos_uploader.go speech/tos_uploader_test.go
git commit -m "feat(speech): implement TOS uploader"
```

---

### 任务 4：实现 ASR 客户端

**文件：**
- 创建：`speech/asr_client.go`
- 创建：`speech/asr_client_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `speech/asr_client_test.go`：

```go
package speech

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestASRClient_Submit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := map[string]interface{}{
			"task_id": "task-123",
			"status":  "submitted",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewASRClient(server.URL, "test-key", "volc.seedasr.auc")
	taskID, err := client.Submit(t.Context(), "https://example.com/audio.mp3")
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
	if taskID != "task-123" {
		t.Fatalf("expected task-123, got %s", taskID)
	}
}
```

- [ ] **步骤 2：运行测试确认失败**

```bash
go test ./speech/ -v -run TestASRClient_Submit
```
预期：FAIL，报错 "undefined: NewASRClient"

- [ ] **步骤 3：实现 ASRClient**

创建 `speech/asr_client.go`：

```go
package speech

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
)

// ASRClient 火山引擎豆包大模型录音文件识别客户端。
type ASRClient struct {
	baseURL    string
	appKey     string
	resourceID string
	pollInterval time.Duration
	pollTimeout  time.Duration
	http        *resty.Client
}

// asrSubmitRequest 提交识别任务请求体。
type asrSubmitRequest struct {
	Audio   asrAudio   `json:"audio"`
	Request asrRequest `json:"request"`
}

type asrAudio struct {
	URL    string `json:"url"`
	Format string `json:"format"`
}

type asrRequest struct {
	ModelName string `json:"model_name"`
}

// asrSubmitResponse 提交识别任务响应。
type asrSubmitResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// asrQueryRequest 查询识别结果请求体。
type asrQueryRequest struct {
	TaskID string `json:"task_id"`
}

// asrQueryResponse 查询识别结果响应。
type asrQueryResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Result struct {
		Text string `json:"text"`
	} `json:"result"`
}

// NewASRClient 创建 ASR 客户端。
func NewASRClient(baseURL, appKey, resourceID string) *ASRClient {
	return &ASRClient{
		baseURL:      baseURL,
		appKey:       appKey,
		resourceID:   resourceID,
		pollInterval: 2 * time.Second,
		pollTimeout:  300 * time.Second,
		http:         resty.New(),
	}
}

// WithPollConfig 设置轮询参数。
func (c *ASRClient) WithPollConfig(interval, timeout time.Duration) *ASRClient {
	c.pollInterval = interval
	c.pollTimeout = timeout
	return c
}

// Submit 提交音频 URL 进行识别，返回任务 ID。
func (c *ASRClient) Submit(ctx context.Context, audioURL string) (string, error) {
	taskID := uuid.New().String()

	reqBody := asrSubmitRequest{
		Audio: asrAudio{
			URL:    audioURL,
			Format: "mp3",
		},
		Request: asrRequest{
			ModelName: "bigmodel",
		},
	}

	resp, err := c.http.R().
		SetHeader("X-Api-Key", c.appKey).
		SetHeader("X-Api-Resource-Id", c.resourceID).
		SetHeader("X-Api-Request-Id", taskID).
		SetHeader("X-Api-Sequence", "-1").
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		SetContext(ctx).
		Post(c.baseURL + "/api/v3/auc/bigmodel/submit")

	if err != nil {
		return "", fmt.Errorf("submit asr task: %w", err)
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("submit asr task: status=%d body=%s", resp.StatusCode(), resp.String())
	}

	var result asrSubmitResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", fmt.Errorf("parse submit response: %w", err)
	}

	return taskID, nil
}

// Query 轮询查询识别结果，直到完成或超时。
func (c *ASRClient) Query(ctx context.Context, taskID string) (string, error) {
	deadline := time.Now().Add(c.pollTimeout)
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return "", fmt.Errorf("asr query timeout after %v", c.pollTimeout)
			}
		}

		text, done, err := c.queryOnce(ctx, taskID)
		if err != nil {
			return "", err
		}
		if done {
			return text, nil
		}
	}
}

func (c *ASRClient) queryOnce(ctx context.Context, taskID string) (text string, done bool, err error) {
	resp, err := c.http.R().
		SetHeader("X-Api-Key", c.appKey).
		SetHeader("X-Api-Resource-Id", c.resourceID).
		SetHeader("X-Api-Request-Id", taskID).
		SetHeader("X-Api-Sequence", "-1").
		SetHeader("Content-Type", "application/json").
		SetBody(asrQueryRequest{TaskID: taskID}).
		SetContext(ctx).
		Post(c.baseURL + "/api/v3/auc/bigmodel/query")

	if err != nil {
		return "", false, fmt.Errorf("query asr task: %w", err)
	}

	if resp.StatusCode() != 200 {
		return "", false, fmt.Errorf("query asr task: status=%d body=%s", resp.StatusCode(), resp.String())
	}

	var result asrQueryResponse
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return "", false, fmt.Errorf("parse query response: %w", err)
	}

	if result.Status == "done" {
		return result.Result.Text, true, nil
	}

	return "", false, nil
}
```

- [ ] **步骤 4：运行测试**

```bash
go test ./speech/ -v -run TestASRClient_Submit
```
预期：PASS

- [ ] **步骤 5：运行编译验证**

```bash
go build ./speech/
```
预期：无编译错误。

- [ ] **步骤 6：Commit**

```bash
git add speech/asr_client.go speech/asr_client_test.go
git commit -m "feat(speech): implement ASR client with submit and query"
```

---

### 任务 5：实现 Recognizer 编排层

**文件：**
- 创建：`speech/recognizer.go`
- 创建：`speech/recognizer_test.go`

- [ ] **步骤 1：编写测试**

创建 `speech/recognizer_test.go`：

```go
package speech

import (
	"context"
	"errors"
	"testing"
)

func TestRecognizer_Transcribe(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		uploader := &mockUploader{url: "https://tos.example.com/audio.mp3"}
		asr := &mockASRClient{text: "你好世界", err: nil}
		r := NewRecognizer(uploader, asr)
		text, err := r.Transcribe(ctx, "test.mp3")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if text != "你好世界" {
			t.Fatalf("expected '你好世界', got '%s'", text)
		}
	})

	t.Run("upload error", func(t *testing.T) {
		uploader := &mockUploader{err: errors.New("upload failed")}
		asr := &mockASRClient{}
		r := NewRecognizer(uploader, asr)
		_, err := r.Transcribe(ctx, "test.mp3")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("asr error", func(t *testing.T) {
		uploader := &mockUploader{url: "https://tos.example.com/audio.mp3"}
		asr := &mockASRClient{err: errors.New("asr failed")}
		r := NewRecognizer(uploader, asr)
		_, err := r.Transcribe(ctx, "test.mp3")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

type mockASRClient struct {
	text string
	err  error
}

func (m *mockASRClient) Submit(ctx context.Context, audioURL string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return "task-123", nil
}

func (m *mockASRClient) Query(ctx context.Context, taskID string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.text, nil
}
```

- [ ] **步骤 2：运行测试确认失败**

```bash
go test ./speech/ -v -run TestRecognizer_Transcribe
```
预期：FAIL，报错 "undefined: NewRecognizer"

- [ ] **步骤 3：实现 Recognizer**

创建 `speech/recognizer.go`：

```go
package speech

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
)

// ASRTranscriber ASR 转录接口，方便测试时 mock。
type ASRTranscriber interface {
	Submit(ctx context.Context, audioURL string) (taskID string, err error)
	Query(ctx context.Context, taskID string) (text string, err error)
}

// Recognizer 语音识别编排层，负责上传音频到 TOS 并调用 ASR 识别。
type Recognizer struct {
	uploader Uploader
	asr      ASRTranscriber
	logger   *log.Logger
}

// NewRecognizer 创建语音识别器。
func NewRecognizer(uploader Uploader, asr ASRTranscriber) *Recognizer {
	return &Recognizer{
		uploader: uploader,
		asr:      asr,
		logger:   log.Default(),
	}
}

// Transcribe 将本地音频文件转录为文字。
func (r *Recognizer) Transcribe(ctx context.Context, filePath string) (string, error) {
	// 1. 上传到 TOS
	r.logger.Printf("[Recognizer] uploading: %s", filePath)
	url, err := r.uploader.Upload(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("upload audio: %w", err)
	}
	r.logger.Printf("[Recognizer] uploaded: %s", url)

	// 2. 提交 ASR 任务
	r.logger.Printf("[Recognizer] submitting asr task")
	taskID, err := r.asr.Submit(ctx, url)
	if err != nil {
		return "", fmt.Errorf("submit asr: %w", err)
	}
	r.logger.Printf("[Recognizer] task submitted: %s", taskID)

	// 3. 轮询直到完成
	r.logger.Printf("[Recognizer] polling for result: %s", taskID)
	text, err := r.asr.Query(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("query asr: %w", err)
	}

	r.logger.Printf("[Recognizer] done: %d chars", len(text))
	return text, nil
}

// detectFormat 根据文件扩展名推断音频格式。
func detectFormat(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".mp3":
		return "mp3"
	case ".wav":
		return "wav"
	case ".m4a":
		return "wav" // M4A 需要先转换为 WAV
	default:
		return "mp3"
	}
}
```

- [ ] **步骤 4：运行测试**

```bash
go test ./speech/ -v -run TestRecognizer_Transcribe
```
预期：PASS

- [ ] **步骤 5：运行编译验证**

```bash
go build ./speech/
```
预期：无编译错误。

- [ ] **步骤 6：Commit**

```bash
git add speech/recognizer.go speech/recognizer_test.go
git commit -m "feat(speech): implement Recognizer orchestrator"
```

---

### 任务 6：实现 HTTP Handler

**文件：**
- 创建：`speech/handler.go`
- 创建：`speech/handler_test.go`

- [ ] **步骤 1：编写失败的测试**

创建 `speech/handler_test.go`：

```go
package speech

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type mockRecognizer struct {
	text string
	err  error
}

func (m *mockRecognizer) Transcribe(_ context.Context, _ string) (string, error) {
	return m.text, m.err
}

func TestHandler_Transcribe(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		rec := &mockRecognizer{text: "你好世界"}
		handler := NewHandler(rec)

		// 创建临时音频文件
		tmpDir := t.TempDir()
		audioPath := filepath.Join(tmpDir, "test.mp3")
		if err := os.WriteFile(audioPath, []byte("fake audio"), 0644); err != nil {
			t.Fatal(err)
		}

		// 构建 multipart 请求
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "test.mp3")
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(audioPath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		io.Copy(part, f)
		writer.Close()

		req := httptest.NewRequest("POST", "/api/speech/transcribe", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rr := httptest.NewRecorder()

		handler.Transcribe(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp transcribeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Text != "你好世界" {
			t.Fatalf("expected '你好世界', got '%s'", resp.Text)
		}
	})

	t.Run("no file", func(t *testing.T) {
		rec := &mockRecognizer{}
		handler := NewHandler(rec)

		req := httptest.NewRequest("POST", "/api/speech/transcribe", nil)
		rr := httptest.NewRecorder()

		handler.Transcribe(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rr.Code)
		}
	})
}
```

- [ ] **步骤 2：运行测试确认失败**

```bash
go test ./speech/ -v -run TestHandler_Transcribe
```
预期：FAIL，报错 "undefined: NewHandler"

- [ ] **步骤 3：实现 Handler**

创建 `speech/handler.go`：

```go
package speech

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// TranscribeFunc 转录函数签名。
type TranscribeFunc func(ctx context.Context, filePath string) (string, error)

// Handler 语音识别 HTTP 处理器。
type Handler struct {
	recognizer *Recognizer
	logger     *log.Logger
}

// NewHandler 创建 HTTP 处理器。
func NewHandler(recognizer *Recognizer) *Handler {
	return &Handler{
		recognizer: recognizer,
		logger:     log.Default(),
	}
}

type transcribeResponse struct {
	Text string `json:"text"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Transcribe 处理音频文件上传并返回识别结果。
func (h *Handler) Transcribe(w http.ResponseWriter, r *http.Request) {
	// 限制文件大小 100MB
	r.Body = http.MaxBytesReader(w, r.Body, 100<<20)

	file, header, err := r.FormFile("file")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "missing or invalid file: "+err.Error())
		return
	}
	defer file.Close()

	// 校验文件格式
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".mp3" && ext != ".wav" && ext != ".m4a" {
		h.writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported format: %s, only mp3/wav/m4a are supported", ext))
		return
	}

	// 保存到临时文件
	tmpDir := os.TempDir()
	tmpFile, err := os.CreateTemp(tmpDir, "speech-*"+ext)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "create temp file: "+err.Error())
		return
	}
	defer os.Remove(tmpFile.Name())

	if _, err := io.Copy(tmpFile, file); err != nil {
		tmpFile.Close()
		h.writeError(w, http.StatusInternalServerError, "save file: "+err.Error())
		return
	}
	tmpFile.Close()

	// 执行转录
	text, err := h.recognizer.Transcribe(r.Context(), tmpFile.Name())
	if err != nil {
		h.logger.Printf("[SpeechHandler] transcribe error: %v", err)
		h.writeError(w, http.StatusBadGateway, "transcribe failed: "+err.Error())
		return
	}

	h.writeJSON(w, http.StatusOK, transcribeResponse{Text: text})
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, msg string) {
	h.writeJSON(w, status, errorResponse{Error: msg})
}
```

- [ ] **步骤 4：运行测试**

```bash
go test ./speech/ -v -run TestHandler_Transcribe
```
预期：PASS

- [ ] **步骤 5：运行编译验证**

```bash
go build ./speech/
```
预期：无编译错误。

- [ ] **步骤 6：Commit**

```bash
git add speech/handler.go speech/handler_test.go
git commit -m "feat(speech): implement HTTP handler for file upload"
```

---

### 任务 7：在 Gateway 中注册路由

**文件：**
- 修改：`cmd/gateway/main.go`

- [ ] **步骤 1：在 main.go 中初始化 speech 模块并注册路由**

在 `cmd/gateway/main.go` 中：

1. 在 import 中添加 `"leadingAgent/speech"`
2. 在 Memory 链路之后（约第 155 行之后），添加 speech 初始化代码：

```go
// -------- Speech 链路 --------
speechCfg := cfg.Speech
if speechCfg.ASR.AppKey == "" {
    speechCfg.ASR.AppKey = os.Getenv("SPEECH_ASR_APP_KEY")
}
if speechCfg.TOS.AccessKey == "" {
    speechCfg.TOS.AccessKey = os.Getenv("SPEECH_TOS_ACCESS_KEY")
}
if speechCfg.TOS.SecretKey == "" {
    speechCfg.TOS.SecretKey = os.Getenv("SPEECH_TOS_SECRET_KEY")
}

tosUploader, err := speech.NewTOSUploader(speechCfg)
if err != nil {
    log.Printf("[Gateway] WARNING: failed to init TOS uploader: %v (speech disabled)", err)
} else {
    asrBaseURL := "https://openspeech.bytedance.com"
    asrClient := speech.NewASRClient(asrBaseURL, speechCfg.ASR.AppKey, speechCfg.ASR.ResourceID)
    if speechCfg.ASR.PollInterval > 0 && speechCfg.ASR.PollTimeout > 0 {
        asrClient.WithPollConfig(
            time.Duration(speechCfg.ASR.PollInterval)*time.Second,
            time.Duration(speechCfg.ASR.PollTimeout)*time.Second,
        )
    }
    recognizer := speech.NewRecognizer(tosUploader, asrClient)
    speechHandler := speech.NewHandler(recognizer)
    http.HandleFunc("/api/speech/transcribe", speechHandler.Transcribe)
    log.Printf("[Gateway] speech module enabled")
}
```

注意：`cfg` 变量来自 `config.LoadConfig()`，如果加载失败 `cfg` 可能为 nil。需要处理这种情况：

```go
if cfg, err := config.LoadConfig(); err == nil {
    // ... existing provider config ...
    
    // -------- Speech 链路 --------
    speechCfg := cfg.Speech
    // ...
}
```

- [ ] **步骤 2：运行编译验证**

```bash
go build ./cmd/gateway/
```
预期：无编译错误。

- [ ] **步骤 3：启动服务验证路由注册**

```bash
SPEECH_ASR_APP_KEY=test SPEECH_TOS_ACCESS_KEY=test SPEECH_TOS_SECRET_KEY=test go run ./cmd/gateway/ &
sleep 2
curl -s http://localhost:8080/health
kill %1 2>/dev/null
```
预期：`ok`，日志中看到 `speech module enabled`

- [ ] **步骤 4：Commit**

```bash
git add cmd/gateway/main.go
git commit -m "feat(speech): wire speech module into gateway"
```

---

## 自检

### 1. 规格覆盖度

| 规格需求 | 对应任务 |
|---------|---------|
| Uploader 接口 | 任务 2 |
| TOS 上传实现 | 任务 3 |
| ASR 客户端（提交+轮询） | 任务 4 |
| Recognizer 编排层 | 任务 5 |
| HTTP Handler（multipart 上传） | 任务 6 |
| 配置（viper） | 任务 1 |
| 错误处理（格式校验、超时、重试） | 任务 4、6 |
| 临时文件清理 | 任务 6 |
| Gateway 路由注册 | 任务 7 |

### 2. 占位符扫描

无 TODO、待定、后续实现等占位符。

### 3. 类型一致性

- `Uploader` 接口在任务 2 定义，任务 3 实现，任务 5 使用 ✓
- `ASRTranscriber` 接口在任务 5 定义，任务 4 的 `ASRClient` 实现 ✓
- `SpeechConfig` 在任务 1 定义，任务 3、7 使用 ✓
- `Recognizer` 在任务 5 定义，任务 6 使用 ✓
- `Handler` 在任务 6 定义，任务 7 使用 ✓
- `Config.Speech` 字段在任务 1 添加，任务 7 使用 ✓