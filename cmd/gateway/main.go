package main

import (
	"context"
	"encoding/json"
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
	"leadingAgent/session"
)

// 注意：handlers.ChatRequest 已被 AgentHandler 使用，
// 这里为了避免循环依赖/字段差异，本地直接按契约解析。
type chatReq struct {
	Message   string `json:"message"`
	SessionId string `json:"sessionId,omitempty"`
	UserId    string `json:"userId,omitempty"`
}

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

	// -------- 3) Agent / Handler --------
	a := agent.NewAgent()
	defer a.Close()

	ah := handlers.NewAgentHandler(a, model, mgr)

	// -------- 4) HTTP 路由 --------
	http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req chatReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		log.Printf("[Gateway] /api/chat message=%q sessionId=%q userId=%q",
			truncate(req.Message, 80), req.SessionId, req.UserId)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()

		// 每次写一个事件，SSE 格式： "data: <json>\n\n"
		emit := func(evt agent.StreamEvent) error {
			data, _ := json.Marshal(evt)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}

		// 把前端请求映射到 handlers 的 ChatRequest（同字段）。
		internal := &handlers.ChatRequest{
			Message:   req.Message,
			SessionId: req.SessionId,
			UserId:    req.UserId,
		}

		if err := ah.StreamChat(ctx, internal, emit); err != nil {
			log.Printf("[Gateway] streaming error: %v", err)
			_ = emit(agent.StreamEvent{
				Type:    agent.StreamEventError,
				Content: err.Error(),
			})
		}
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
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

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
