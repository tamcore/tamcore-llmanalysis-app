package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func newTestApp(t *testing.T, endpointURL, apiKey string) *App {
	t.Helper()

	jsonData, err := json.Marshal(map[string]any{
		"endpointURL":    endpointURL,
		"model":          "test-model",
		"timeoutSeconds": 10,
		"maxTokens":      100,
	})
	if err != nil {
		t.Fatalf("marshal jsonData: %v", err)
	}

	inst, err := NewApp(context.Background(), backend.AppInstanceSettings{
		JSONData:                jsonData,
		DecryptedSecureJSONData: map[string]string{"apiKey": apiKey},
	})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	return inst.(*App)
}

func TestHealthCheck_Success(t *testing.T) {
	t.Parallel()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer test-key")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
	}))
	defer llmServer.Close()

	app := newTestApp(t, llmServer.URL+"/v1", "test-key")

	result, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
	})
	if err != nil {
		t.Fatalf("CheckHealth returned error: %v", err)
	}

	if result.Status != backend.HealthStatusOk {
		t.Errorf("Status = %v, want %v", result.Status, backend.HealthStatusOk)
	}
}

func TestHealthCheck_Failure(t *testing.T) {
	t.Parallel()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_api_key"}`))
	}))
	defer llmServer.Close()

	app := newTestApp(t, llmServer.URL+"/v1", "bad-key")

	result, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
	})
	if err != nil {
		t.Fatalf("CheckHealth returned error: %v", err)
	}

	if result.Status != backend.HealthStatusError {
		t.Errorf("Status = %v, want %v", result.Status, backend.HealthStatusError)
	}
}

func TestHealthCheck_ConnectionRefused(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, "http://127.0.0.1:1/v1", "key")

	result, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
	})
	if err != nil {
		t.Fatalf("CheckHealth returned error: %v", err)
	}

	if result.Status != backend.HealthStatusError {
		t.Errorf("Status = %v, want %v", result.Status, backend.HealthStatusError)
	}
}
