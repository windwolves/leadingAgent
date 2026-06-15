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
	"leadingAgent/services"
	"leadingAgent/session"
)

func main() {
	// -------- 1) 加载模型配置 --------
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

	if apiURL == "" {
		apiURL = "https://api.deepseek.com/v1"
	}
	if modelName == "" {
		modelName = "deepseek-chat"
	}

	model := &foundation.Model{
		Name: modelName,
		Options: map[string]interface{}{
			"api_key":           apiKey,
			"api_url":           apiURL,
			"max_tokens":        maxTokens,
			"reasoning_effort":  reasoningEffort,
		},
	}
	log.Printf("[Gateway] model=%s apiURL=%s maxTokens=%d reasoningEffort=%s", modelName, apiURL, maxTokens, reasoningEffort)

	// -------- 2) SQLite 仓库 + session.Manager --------
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

	// -------- 4) SessionService → AgentService → Handler --------
	a := agent.NewAgent()
	defer a.Close()

	sessSvc := services.NewSessionService(mgr)
	svc := services.NewAgentService(a, model, sessSvc, memSvc)
	ah := handlers.NewAgentHandler(svc, sessSvc)

	// -------- 5) 路由注册 --------
	http.HandleFunc("/api/chat", ah.HandleChat)
	http.HandleFunc("/api/sessions", ah.HandleSessions)
	http.HandleFunc("/api/sessions/messages", ah.HandleGetSessionMessages)
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
