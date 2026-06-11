package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"leadingAgent/agent"
	"leadingAgent/agent/foundation"
	"leadingAgent/config"
	"leadingAgent/handlers"
	"leadingAgent/services"
	"leadingAgent/session"
)

func main() {
	// -------- 1) 加载模型配置 --------
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	apiURL := os.Getenv("DEEPSEEK_API_URL")
	modelName := os.Getenv("DEEPSEEK_MODEL")

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
			"api_key": apiKey,
			"api_url": apiURL,
		},
	}
	log.Printf("[Gateway] model=%s apiURL=%s", modelName, apiURL)

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

	// -------- 3) SessionService → AgentService → Handler --------
	a := agent.NewAgent()
	defer a.Close()

	sessSvc := services.NewSessionService(mgr)
	svc := services.NewAgentService(a, model, sessSvc)
	ah := handlers.NewAgentHandler(svc, sessSvc)

	// -------- 4) 路由注册 --------
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
