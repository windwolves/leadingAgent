package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"leadingAgent/agent"
	"leadingAgent/services"
)

// ChatRequest 前端发送的对话请求。
type ChatRequest struct {
	Message   string `json:"message"`
	SessionId string `json:"sessionId,omitempty"`
	UserId    string `json:"userId,omitempty"`
}

// AgentHandler HTTP 层处理器，负责请求参数校验、SSE 流式响应编排和错误返回。
// 对话业务委托给 services.AgentService，会话管理委托给 services.SessionService。
type AgentHandler struct {
	svc     *services.AgentService
	sessSvc *services.SessionService
	logger  *log.Logger
}

// NewAgentHandler 创建 HTTP 层处理器。
func NewAgentHandler(svc *services.AgentService, sessSvc *services.SessionService) *AgentHandler {
	return &AgentHandler{
		svc:     svc,
		sessSvc: sessSvc,
		logger:  log.Default(),
	}
}

// HandleChat 处理 POST /api/chat，流式 SSE 响应。
func (h *AgentHandler) HandleChat(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "empty message", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.handleChatFallback(w, r, req)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	h.logger.Printf("[AgentHandler] /api/chat message=%q sessionId=%q userId=%q",
		truncate(req.Message, 80), req.SessionId, req.UserId)

	emit := func(evt agent.StreamEvent) error {
		data, _ := json.Marshal(evt)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := h.svc.StreamChat(ctx, req.SessionId, req.UserId, req.Message, emit); err != nil {
		h.logger.Printf("[AgentHandler] streaming error: %v", err)
		_ = emit(agent.StreamEvent{
			Type:    agent.StreamEventError,
			Content: err.Error(),
		})
	}
}

// HandleSessions 处理 /api/sessions（GET 列表 / DELETE 删除）。
func (h *AgentHandler) HandleSessions(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.handleListSessions(w, r)
	case http.MethodDelete:
		h.handleDeleteSession(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *AgentHandler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		http.Error(w, "missing userId", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	list, err := h.sessSvc.ListSessions(ctx, userID, 100)
	if err != nil {
		h.logger.Printf("[AgentHandler] list sessions error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

func (h *AgentHandler) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionId string `json:"sessionId"`
		UserId    string `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.SessionId == "" {
		http.Error(w, "missing sessionId", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.sessSvc.DeleteSession(ctx, body.SessionId, body.UserId); err != nil {
		h.logger.Printf("[AgentHandler] delete session error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetSessionMessages 处理 /api/sessions/messages（GET 列表 / DELETE 单条消息）。
func (h *AgentHandler) HandleGetSessionMessages(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.handleListMessages(w, r)
	case http.MethodDelete:
		h.handleDeleteMessage(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *AgentHandler) handleListMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	userID := r.URL.Query().Get("userId")
	if sessionID == "" {
		http.Error(w, "missing sessionId", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	msgs, err := h.sessSvc.GetMessages(ctx, sessionID, userID)
	if err != nil {
		h.logger.Printf("[AgentHandler] get session messages error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(msgs)
}

func (h *AgentHandler) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionId string `json:"sessionId"`
		UserId    string `json:"userId"`
		Indexes   []int  `json:"indexes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if body.SessionId == "" || len(body.Indexes) == 0 {
		http.Error(w, "missing sessionId or indexes", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.sessSvc.DeleteMessages(ctx, body.SessionId, body.UserId, body.Indexes); err != nil {
		h.logger.Printf("[AgentHandler] delete messages error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// setCORS 统一设置跨域响应头，并处理 OPTIONS 预检请求。
func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// handleChatFallback 非流式降级处理，返回普通 JSON。
func (h *AgentHandler) handleChatFallback(w http.ResponseWriter, r *http.Request, req ChatRequest) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	result, err := h.svc.Chat(ctx, req.SessionId, req.UserId, req.Message)
	if err != nil {
		h.logger.Printf("[AgentHandler] chat fallback error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"response":  result.Response,
		"sessionId": result.SessionID,
	})
}
