package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/resource/httpadapter"
)

// ChatMessage represents a single message in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents an incoming chat analysis request.
type ChatRequest struct {
	Mode     string          `json:"mode"`
	Prompt   string          `json:"prompt"`
	Context  json.RawMessage `json:"context"`
	Messages []ChatMessage   `json:"messages,omitempty"`

	// authHeaders are injected by the handler, not from JSON.
	authHeaders map[string]string `json:"-"`
}

// ChatResponse represents the chat completion response.
type ChatResponse struct {
	Content       string        `json:"content"`
	Usage         *Usage        `json:"usage,omitempty"`
	Done          bool          `json:"done"`
	ToolCall      *ToolCallInfo `json:"toolCall,omitempty"`
	ContextTokens int           `json:"contextTokens,omitempty"`
	MaxTokens     int           `json:"maxTokens,omitempty"`
}

// ToolCallInfo describes a tool invocation sent to the frontend for display.
type ToolCallInfo struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Usage holds token usage information.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

var validModes = map[string]bool{
	"chat":                true,
	"explain_panel":       true,
	"summarize_dashboard": true,
	"analyze_logs":        true,
	"analyze_metrics":     true,
}

func (a *App) registerRoutes() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("POST /chat", a.handleChat)
	mux.HandleFunc("/", a.handleNotFound)

	a.httpHandler = httpadapter.New(mux)
}

// CallResource routes requests, handling streaming endpoints directly.
func (a *App) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if req.Path == "chat/stream" && req.Method == http.MethodPost {
		return a.handleStreamResource(ctx, req, sender)
	}
	return a.httpHandler.CallResource(ctx, req, sender)
}

func (a *App) handleStreamResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	user := extractUser(req.Headers)
	if !a.getLimiter(user).Allow() {
		return sendErrorResponse(sender, http.StatusTooManyRequests, "rate limit exceeded")
	}

	var chatReq ChatRequest
	if err := json.Unmarshal(req.Body, &chatReq); err != nil {
		return sendErrorResponse(sender, http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	chatReq.Prompt = sanitizePrompt(chatReq.Prompt)

	if chatReq.Prompt == "" {
		return sendErrorResponse(sender, http.StatusBadRequest, "prompt is required")
	}

	if !validModes[chatReq.Mode] {
		return sendErrorResponse(sender, http.StatusBadRequest, "invalid mode: "+chatReq.Mode)
	}

	if err := sanitizeContextSize(chatReq.Context, maxContextBytes); err != nil {
		return sendErrorResponse(sender, http.StatusBadRequest, err.Error())
	}

	// Forward auth headers so the tool executor can query datasources
	authHeaders := extractAuthHeaders(req.Headers)

	return a.streamChatCompletion(ctx, chatReq, sender, authHeaders)
}

// extractAuthHeaders pulls authentication-related headers from the request.
func extractAuthHeaders(headers map[string][]string) map[string]string {
	result := make(map[string]string)
	for _, key := range []string{"Cookie", "Authorization", "X-Grafana-Org-Id"} {
		if vals, ok := headers[key]; ok && len(vals) > 0 {
			result[key] = vals[0]
		}
		// Also check lowercase
		lower := strings.ToLower(key)
		if vals, ok := headers[lower]; ok && len(vals) > 0 {
			result[key] = vals[0]
		}
	}
	return result
}

func sendErrorResponse(sender backend.CallResourceResponseSender, status int, message string) error {
	body, _ := json.Marshal(map[string]string{"error": message})
	return sender.Send(&backend.CallResourceResponse{
		Status: status,
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
		Body: body,
	})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	result, err := a.CheckHealth(r.Context(), &backend.CheckHealthRequest{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	status := http.StatusOK
	statusStr := "ok"

	if result.Status != backend.HealthStatusOk {
		status = http.StatusBadGateway
		statusStr = "error"
	}

	writeJSON(w, status, map[string]string{
		"status":  statusStr,
		"message": result.Message,
		"model":   a.settings.Model,
	})
}

func (a *App) handleChat(w http.ResponseWriter, r *http.Request) {
	user := r.Header.Get("X-Grafana-User")
	if user == "" {
		user = "anonymous"
	}
	if !a.getLimiter(user).Allow() {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "rate limit exceeded",
		})
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid request body: " + err.Error(),
		})
		return
	}

	req.Prompt = sanitizePrompt(req.Prompt)
	req.authHeaders = extractAuthHeaders(r.Header)

	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "prompt is required",
		})
		return
	}

	if !validModes[req.Mode] {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid mode: " + req.Mode,
		})
		return
	}

	if err := sanitizeContextSize(req.Context, maxContextBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
		return
	}

	start := time.Now()
	content, usage, err := a.chatCompletion(r.Context(), req)
	duration := time.Since(start).Seconds()

	if err != nil {
		a.logger.Error("chat completion failed", "error", err, "mode", req.Mode, "duration_s", duration)
		a.metrics.recordRequest(a.settings.Model, "error", duration, 0, 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "LLM request failed: " + err.Error(),
		})
		return
	}

	promptTokens, completionTokens := 0, 0
	if usage != nil {
		promptTokens = usage.PromptTokens
		completionTokens = usage.CompletionTokens
	}

	a.logger.Info("chat completion succeeded", "mode", req.Mode, "model", a.settings.Model,
		"duration_s", duration, "prompt_tokens", promptTokens, "completion_tokens", completionTokens)
	a.metrics.recordRequest(a.settings.Model, "success", duration, promptTokens, completionTokens)

	writeJSON(w, http.StatusOK, ChatResponse{
		Content: content,
		Usage:   usage,
		Done:    true,
	})
}

func (a *App) handleNotFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{
		"error": "not found",
	})
}

// extractUser returns the Grafana user from request headers, defaulting to "anonymous".
func extractUser(headers map[string][]string) string {
	for _, key := range []string{"X-Grafana-User", "x-grafana-user"} {
		if vals, ok := headers[key]; ok && len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
	}
	return "anonymous"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
