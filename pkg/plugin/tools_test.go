package plugin

import (
	"encoding/json"
	"testing"
)

func TestLLMTools_ReturnsExpectedTools(t *testing.T) {
	t.Parallel()

	tools := llmTools()
	if len(tools) != 7 {
		t.Fatalf("expected 7 tools, got %d", len(tools))
	}

	expected := map[string]bool{
		"query_prometheus": false,
		"query_loki":       false,
		"list_datasources": false,
		"list_dashboards":  false,
		"get_dashboard":    false,
		"list_alerts":      false,
		"list_alert_rules": false,
	}

	for _, tool := range tools {
		if tool.Function == nil {
			t.Error("tool has nil function definition")
			continue
		}
		name := tool.Function.Name
		if _, ok := expected[name]; !ok {
			t.Errorf("unexpected tool: %s", name)
		}
		expected[name] = true

		if tool.Function.Description == "" {
			t.Errorf("tool %s has empty description", name)
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected tool %s not found", name)
		}
	}
}

func TestLLMTools_PrometheusSchemaValid(t *testing.T) {
	t.Parallel()

	tools := llmTools()
	var promTool *json.RawMessage
	for _, tool := range tools {
		if tool.Function != nil && tool.Function.Name == "query_prometheus" {
			raw, ok := tool.Function.Parameters.(json.RawMessage)
			if !ok {
				t.Fatal("expected json.RawMessage parameters")
			}
			promTool = &raw
			break
		}
	}

	if promTool == nil {
		t.Fatal("query_prometheus tool not found")
	}

	var schema map[string]any
	if err := json.Unmarshal(*promTool, &schema); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}

	if schema["type"] != "object" {
		t.Error("expected type=object")
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties object")
	}

	if _, ok := props["query"]; !ok {
		t.Error("expected 'query' property")
	}

	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("expected required array")
	}

	if len(required) != 1 || required[0] != "query" {
		t.Errorf("expected required=[query], got %v", required)
	}
}

func TestPrometheusQueryArgs_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"query":"rate(http_requests_total[5m])","start":"now-1h","end":"now","step":"30s"}`
	var args PrometheusQueryArgs
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if args.Query != "rate(http_requests_total[5m])" {
		t.Errorf("query = %q", args.Query)
	}
	if args.Start != "now-1h" {
		t.Errorf("start = %q", args.Start)
	}
	if args.Step != "30s" {
		t.Errorf("step = %q", args.Step)
	}
}

func TestLokiQueryArgs_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"query":"{app=\"nginx\"} |= \"error\"","limit":50}`
	var args LokiQueryArgs
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if args.Query != `{app="nginx"} |= "error"` {
		t.Errorf("query = %q", args.Query)
	}
	if args.Limit != 50 {
		t.Errorf("limit = %d", args.Limit)
	}
}

func TestListDashboardsArgs_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"query":"kubernetes"}`
	var args ListDashboardsArgs
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if args.Query != "kubernetes" {
		t.Errorf("query = %q", args.Query)
	}
}

func TestGetDashboardArgs_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"uid":"abc-123"}`
	var args GetDashboardArgs
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if args.UID != "abc-123" {
		t.Errorf("uid = %q", args.UID)
	}
}

func TestListAlertsArgs_Unmarshal(t *testing.T) {
	t.Parallel()

	input := `{"filter":"severity=critical","state":"firing"}`
	var args ListAlertsArgs
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if args.Filter != "severity=critical" {
		t.Errorf("filter = %q", args.Filter)
	}
	if args.State != "firing" {
		t.Errorf("state = %q", args.State)
	}
}

func TestListAlertsArgs_Unmarshal_Empty(t *testing.T) {
	t.Parallel()

	input := `{}`
	var args ListAlertsArgs
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if args.Filter != "" {
		t.Errorf("expected empty filter, got %q", args.Filter)
	}
	if args.State != "" {
		t.Errorf("expected empty state, got %q", args.State)
	}
}
