package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/example/deepseek-go/config"
	"github.com/example/deepseek-go/handlers"
	"github.com/example/deepseek-go/services"
)

type ChatRequest struct {
	Message string `json:"message"`
}

type ChatResponse struct {
	Response         string `json:"response"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

type StreamChatResponse struct {
	Response         string `json:"response"`
	IsLast           bool   `json:"is_last"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	handler, err := handlers.NewLdAgentHandler(cfg)
	if err != nil {
		log.Fatalf("Failed to create handler: %v", err)
	}

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

		requestID := time.Now().UnixNano()
		ctx := context.WithValue(r.Context(), "request_id", requestID)

		serviceReq := &services.ChatRequest{
			Message: req.Message,
		}

		resp, err := handler.Chat(ctx, serviceReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ChatResponse{
			Response:         resp.Response,
			PromptTokens:     resp.PromptTokens,
			CompletionTokens: resp.CompletionTokens,
			TotalTokens:      resp.TotalTokens,
		})
	})

	http.HandleFunc("/api/stream-chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		requestID := time.Now().UnixNano()
		ctx := context.WithValue(r.Context(), "request_id", requestID)

		serviceReq := &services.ChatRequest{
			Message: req.Message,
		}

		sender := func(resp *services.StreamChatResponse) error {
			response := StreamChatResponse{
				Response:         resp.Response,
				IsLast:           resp.IsLast,
				PromptTokens:     resp.PromptTokens,
				CompletionTokens: resp.CompletionTokens,
				TotalTokens:      resp.TotalTokens,
			}

			data, err := json.Marshal(response)
			if err != nil {
				return err
			}

			if _, err := w.Write(data); err != nil {
				return err
			}
			if _, err := w.Write([]byte("\n\n")); err != nil {
				return err
			}
			flusher.Flush()

			return nil
		}

		if err := handler.StreamChat(ctx, serviceReq, sender); err != nil {
			log.Printf("Stream chat error: %v", err)
		}
	})

	log.Println("Starting HTTP gateway on port 8080...")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Gateway failed to start: %v", err)
	}
}
