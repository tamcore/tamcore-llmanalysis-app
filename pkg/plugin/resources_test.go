package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestResourceHealth_Success(t *testing.T) {
	t.Parallel()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
	}))
	defer llmServer.Close()

	app := newTestApp(t, llmServer.URL+"/v1", "key")

	req := &backend.CallResourceRequest{
		Path:   "health",
		Method: http.MethodGet,
	}

	var statusCode int
	var body []byte

	sender := backend.CallResourceResponseSenderFunc(func(res *backend.CallResourceResponse) error {
		statusCode = res.Status
		body = res.Body
		return nil
	})

	err := app.CallResource(context.Background(), req, sender)
	if err != nil {
		t.Fatalf("CallResource returned error: %v", err)
	}

	if statusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", statusCode, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("status = %q, want %q", resp["status"], "ok")
	}

	// Verify provider URL is NOT leaked
	if _, exists := resp["provider"]; exists {
		t.Error("health response should not include provider URL")
	}
}

func TestResourceChat_MissingPrompt(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, "http://localhost:1/v1", "key")

	req := &backend.CallResourceRequest{
		Path:   "chat",
		Method: http.MethodPost,
		Body:   []byte(`{"mode":"explain_panel","context":{}}`),
	}

	var statusCode int

	sender := backend.CallResourceResponseSenderFunc(func(res *backend.CallResourceResponse) error {
		statusCode = res.Status
		return nil
	})

	err := app.CallResource(context.Background(), req, sender)
	if err != nil {
		t.Fatalf("CallResource returned error: %v", err)
	}

	if statusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", statusCode, http.StatusBadRequest)
	}
}

func TestResourceChat_InvalidMode(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, "http://localhost:1/v1", "key")

	req := &backend.CallResourceRequest{
		Path:   "chat",
		Method: http.MethodPost,
		Body:   []byte(`{"mode":"invalid_mode","prompt":"test","context":{}}`),
	}

	var statusCode int

	sender := backend.CallResourceResponseSenderFunc(func(res *backend.CallResourceResponse) error {
		statusCode = res.Status
		return nil
	})

	err := app.CallResource(context.Background(), req, sender)
	if err != nil {
		t.Fatalf("CallResource returned error: %v", err)
	}

	if statusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", statusCode, http.StatusBadRequest)
	}
}

func TestResourceChat_Success(t *testing.T) {
	t.Parallel()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}

		bodyBytes, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		_ = json.Unmarshal(bodyBytes, &reqBody)

		if reqBody["model"] != "test-model" {
			t.Errorf("model = %v, want test-model", reqBody["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"content": "This panel shows test data."}}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5}
		}`))
	}))
	defer llmServer.Close()

	app := newTestApp(t, llmServer.URL+"/v1", "key")

	chatReq := `{
		"mode": "explain_panel",
		"prompt": "What does this show?",
		"context": {"panel": {"title": "Test"}}
	}`

	req := &backend.CallResourceRequest{
		Path:   "chat",
		Method: http.MethodPost,
		Body:   []byte(chatReq),
	}

	var statusCode int
	var body []byte

	sender := backend.CallResourceResponseSenderFunc(func(res *backend.CallResourceResponse) error {
		statusCode = res.Status
		body = res.Body
		return nil
	})

	err := app.CallResource(context.Background(), req, sender)
	if err != nil {
		t.Fatalf("CallResource returned error: %v", err)
	}

	if statusCode != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", statusCode, http.StatusOK, string(body))
	}

	var resp ChatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !strings.Contains(resp.Content, "test data") {
		t.Errorf("content = %q, expected to contain 'test data'", resp.Content)
	}
}

func TestResourceUnknownPath(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, "http://localhost:1/v1", "key")

	req := &backend.CallResourceRequest{
		Path:   "unknown",
		Method: http.MethodGet,
	}

	var statusCode int

	sender := backend.CallResourceResponseSenderFunc(func(res *backend.CallResourceResponse) error {
		statusCode = res.Status
		return nil
	})

	err := app.CallResource(context.Background(), req, sender)
	if err != nil {
		t.Fatalf("CallResource returned error: %v", err)
	}

	if statusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", statusCode, http.StatusNotFound)
	}
}

func TestStreamResource_RateLimitExceeded(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, "http://localhost:1/v1", "key")

	// Exhaust the burst of 10
	for range 10 {
		req := &backend.CallResourceRequest{
			Path:    "chat/stream",
			Method:  http.MethodPost,
			Body:    []byte(`{"mode":"chat","prompt":"test","context":{}}`),
			Headers: map[string][]string{"X-Grafana-User": {"testuser"}},
		}
		sender := backend.CallResourceResponseSenderFunc(func(_ *backend.CallResourceResponse) error { return nil })
		_ = app.CallResource(context.Background(), req, sender)
	}

	// 11th request should be rate limited
	req := &backend.CallResourceRequest{
		Path:    "chat/stream",
		Method:  http.MethodPost,
		Body:    []byte(`{"mode":"chat","prompt":"test","context":{}}`),
		Headers: map[string][]string{"X-Grafana-User": {"testuser"}},
	}

	var statusCode int
	sender := backend.CallResourceResponseSenderFunc(func(res *backend.CallResourceResponse) error {
		statusCode = res.Status
		return nil
	})

	err := app.CallResource(context.Background(), req, sender)
	if err != nil {
		t.Fatalf("CallResource returned error: %v", err)
	}

	if statusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", statusCode, http.StatusTooManyRequests)
	}
}

func TestExtractUser_FromHeaders(t *testing.T) {
	t.Parallel()

	headers := map[string][]string{
		"X-Grafana-User": {"admin"},
	}
	if got := extractUser(headers); got != "admin" {
		t.Errorf("extractUser() = %q, want %q", got, "admin")
	}
}

func TestExtractUser_DefaultsToAnonymous(t *testing.T) {
	t.Parallel()

	if got := extractUser(nil); got != "anonymous" {
		t.Errorf("extractUser(nil) = %q, want %q", got, "anonymous")
	}
}
