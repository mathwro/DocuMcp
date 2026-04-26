// internal/bench/tasks/api_test.go
package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicAPI_ParsesEndTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":  150,
				"output_tokens": 25,
			},
			"content": []map[string]any{
				{"type": "text", "text": "Done. See https://x.example.com/p."},
			},
		})
	}))
	defer srv.Close()

	a := NewAnthropicAPI("k", "claude-sonnet-4-6", WithAPIBaseURL(srv.URL))
	resp, err := a.Send(context.Background(), nil, []map[string]any{
		{"role": "user", "content": "hi"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.StopReason != "end_turn" || resp.InputTokens != 150 || resp.OutputTokens != 25 {
		t.Errorf("unexpected response: %+v", resp)
	}
	if resp.FinalText == "" {
		t.Errorf("expected final text")
	}
}

func TestAnthropicAPI_ParsesToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stop_reason": "tool_use",
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 2,
			},
			"content": []map[string]any{
				{"type": "tool_use", "id": "id1", "name": "fetch_url", "input": map[string]any{"url": "u"}},
			},
		})
	}))
	defer srv.Close()

	a := NewAnthropicAPI("k", "claude-sonnet-4-6", WithAPIBaseURL(srv.URL))
	resp, err := a.Send(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "fetch_url" {
		t.Errorf("unexpected tool calls: %+v", resp.ToolCalls)
	}
}
