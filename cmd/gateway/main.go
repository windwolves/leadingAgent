package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"leadingAgent/agent"
	"leadingAgent/agent/foundation"
	"leadingAgent/agent/tools"
	"leadingAgent/config"
	"leadingAgent/handlers"
	"leadingAgent/memory"
	"leadingAgent/repository"
	"leadingAgent/services"
	"leadingAgent/session"
)

func main() {
	// -------- 1) 加载模型配置（支持多 provider）--------
	provider := os.Getenv("LLM_PROVIDER")
	if provider == "" {
		provider = "deepseek"
	}

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	apiURL := os.Getenv("DEEPSEEK_API_URL")
	modelName := os.Getenv("DEEPSEEK_MODEL")

	maxTokens := 4096
	if raw := os.Getenv("DEEPSEEK_MAX_TOKENS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			maxTokens = v
		}
	}

	reasoningEffort := "low"
	if raw := os.Getenv("DEEPSEEK_REASONING_EFFORT"); raw != "" {
		reasoningEffort = raw
	}

	if cfg, err := config.LoadConfig(); err == nil {
		switch provider {
		case "doubao":
			if cfg.DoubaoAPIKey != "" {
				apiKey = cfg.DoubaoAPIKey
			}
			if cfg.DoubaoAPIURL != "" {
				apiURL = cfg.DoubaoAPIURL
			}
			if cfg.DoubaoModel != "" {
				modelName = cfg.DoubaoModel
			}
		default: // deepseek
			if cfg.DeepSeekAPIKey != "" {
				apiKey = cfg.DeepSeekAPIKey
			}
			if cfg.DeepSeekAPIURL != "" {
				apiURL = cfg.DeepSeekAPIURL
			}
			if cfg.DeepSeekModel != "" {
				modelName = cfg.DeepSeekModel
			}
			if cfg.DeepSeekMaxTokens > 0 {
				maxTokens = cfg.DeepSeekMaxTokens
			}
			if cfg.DeepSeekReasoningEffort != "" {
				reasoningEffort = cfg.DeepSeekReasoningEffort
			}
		}
	}

	switch provider {
	case "doubao":
		if apiURL == "" {
			apiURL = "https://ark.cn-beijing.volces.com/api/v3"
		}
		if modelName == "" {
			modelName = "doubao-seed-1-6-250715"
		}
	default: // deepseek
		if apiURL == "" {
			apiURL = "https://api.deepseek.com/v1"
		}
		if modelName == "" {
			modelName = "deepseek-chat"
		}
	}

	// reasoning_effort 只在 deepseek provider 时保留
	if provider != "deepseek" {
		reasoningEffort = ""
	}

	// thinking 参数：doubao 默认关闭深度思考
	thinking := ""
	if provider == "doubao" {
		thinking = `{"type":"disabled"}`
	}

	// lite_tools：doubao 用精简工具摘要替代全量 schema
	liteTools := false
	if provider == "doubao" {
		liteTools = true
	}

	log.Printf("[Gateway] provider=%s model=%s apiURL=%s maxTokens=%d reasoningEffort=%s liteTools=%v", provider, modelName, apiURL, maxTokens, reasoningEffort, liteTools)

	model := &foundation.Model{
		Name: modelName,
		Options: map[string]interface{}{
			"api_key":          apiKey,
			"api_url":          apiURL,
			"max_tokens":       maxTokens,
			"reasoning_effort": reasoningEffort,
			"thinking":         thinking,
			"lite_tools":       liteTools,
		},
	}
	dbPath := os.Getenv("SESSION_DB")
	if dbPath == "" {
		_ = os.MkdirAll("data", 0o755)
		dbPath = filepath.Join("data", "sessions.db")
	}

	repo, err := session.NewSQLiteRepository(dbPath)
	if err != nil {
		log.Fatalf("[Gateway] failed to open session DB %q: %v", dbPath, err)
	}
	defer repo.Close()
	log.Printf("[Gateway] session DB: %s", dbPath)

	mgr := session.NewManager(repo,
		session.WithTTL(24*time.Hour),
	)
	defer mgr.Close()

	// -------- 3) Memory 链路 --------
	memDir := os.Getenv("MEMORY_DIR")
	if memDir == "" {
		memDir = filepath.Join("data", "memories")
	}
	memProvider, err := memory.NewMarkdownProvider(memDir)
	if err != nil {
		log.Fatalf("[Gateway] failed to init memory: %v", err)
	}
	memSvc := services.NewMemoryService(memProvider)

	// 注入 remember_fact 工具的回调
	tools.SetWriteMemoryFunc(memSvc.Remember)

	// -------- 4) Cost repository --------
	costDBPath := os.Getenv("COST_DB")
	if costDBPath == "" {
		costDBPath = filepath.Join("data", "cost.db")
	}
	costRepo, err := repository.NewCostRepository(costDBPath)
	if err != nil {
		log.Fatalf("[Gateway] failed to open cost DB %q: %v", costDBPath, err)
	}
	defer costRepo.Close()
	log.Printf("[Gateway] cost DB: %s", costDBPath)

	// -------- 5) SessionService → AgentService → Handler --------
	a := agent.NewAgent()
	defer a.Close()

	sessSvc := services.NewSessionService(mgr)
	svc := services.NewAgentService(a, model, sessSvc, memSvc).WithCostRepo(costRepo)
	ah := handlers.NewAgentHandler(svc, sessSvc, costRepo)

	// -------- 6) 路由注册 --------
	http.HandleFunc("/api/chat", ah.HandleChat)
	http.HandleFunc("/api/sessions", ah.HandleSessions)
	http.HandleFunc("/api/sessions/messages", ah.HandleGetSessionMessages)
	http.HandleFunc("/api/costs", ah.HandleCosts)
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("[Gateway] starting HTTP gateway (SSE) on :%s ...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Gateway failed to start: %v", err)
	}
}
