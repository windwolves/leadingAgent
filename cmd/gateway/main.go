package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"leadingAgent/agent"
	"leadingAgent/agent/foundation"
	"leadingAgent/config"
)

type ChatRequest struct {
	Message   string `json:"message"`
	SessionId string `json:"sessionId"`
}

func main() {
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

	log.Printf("Using model: %s, API URL: %s", modelName, apiURL)

	model := &foundation.Model{
		Name: modelName,
		Options: map[string]interface{}{
			"api_key": apiKey,
		},
	}

	a := agent.NewAgent()
	defer a.Close()

	http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		log.Printf("[Gateway] /api/chat (streaming): message=%q sessionId=%q", req.Message, req.SessionId)

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

		sendEvent := func(evt agent.StreamEvent) error {
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return nil
		}

		err := a.ExecuteStreaming(ctx, model, req.Message, sendEvent)
		if err != nil {
			log.Printf("[Gateway] streaming error: %v", err)
			errData, _ := json.Marshal(agent.StreamEvent{
				Type:    agent.StreamEventError,
				Content: err.Error(),
			})
			fmt.Fprintf(w, "data: %s\n\n", errData)
			flusher.Flush()
		}
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting HTTP gateway (SSE streaming) on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Gateway failed to start: %v", err)
	}
}
