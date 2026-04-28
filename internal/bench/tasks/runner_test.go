// internal/bench/tasks/runner_test.go
package tasks

import (
	"context"
	"errors"
	"testing"
)

// scriptedAPI returns scripted responses in order. Each call consumes one entry.
type scriptedAPI struct {
	responses []apiResponse
	calls     int
}

func (s *scriptedAPI) Send(_ context.Context, _ []map[string]any, _ []map[string]any) (apiResponse, error) {
	if s.calls >= len(s.responses) {
		return apiResponse{}, errors.New("script exhausted")
	}
	r := s.responses[s.calls]
	s.calls++
	return r, nil
}

func TestRunner_StopsOnEndTurn(t *testing.T) {
	api := &scriptedAPI{
		responses: []apiResponse{
			{
				StopReason:   "end_turn",
				InputTokens:  100,
				OutputTokens: 20,
				FinalText:    "the answer is 42",
			},
		},
	}
	tools := map[string]ToolHandler{}
	res, err := RunTrial(context.Background(), api, tools, ConfigATools(), "what's the answer?", RunLimits{MaxRounds: 5})
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if res.Aborted {
		t.Errorf("should not have aborted")
	}
	if res.InputTokens != 100 || res.OutputTokens != 20 {
		t.Errorf("token totals: got input=%d output=%d", res.InputTokens, res.OutputTokens)
	}
	if res.FinalAnswer != "the answer is 42" {
		t.Errorf("final answer: %q", res.FinalAnswer)
	}
}

func TestRunner_ExecutesToolThenAnswers(t *testing.T) {
	api := &scriptedAPI{
		responses: []apiResponse{
			{
				StopReason:   "tool_use",
				InputTokens:  50,
				OutputTokens: 10,
				ToolCalls: []toolCall{
					{ID: "t1", Name: "fetch_url", Input: map[string]any{"url": "https://docs.example.com/x"}},
				},
			},
			{
				StopReason:   "end_turn",
				InputTokens:  60,
				OutputTokens: 5,
				FinalText:    "Source: https://docs.example.com/x — answer.",
			},
		},
	}
	tools := map[string]ToolHandler{
		"fetch_url": func(_ context.Context, _ map[string]any) (string, error) { return "page text", nil },
	}
	res, err := RunTrial(context.Background(), api, tools, ConfigATools(), "q?", RunLimits{MaxRounds: 5})
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if res.ToolCalls != 1 {
		t.Errorf("tool calls: got %d", res.ToolCalls)
	}
	if res.InputTokens != 110 || res.OutputTokens != 15 {
		t.Errorf("token totals: got input=%d output=%d", res.InputTokens, res.OutputTokens)
	}
	if len(res.CitedURLs) != 1 || res.CitedURLs[0] != "https://docs.example.com/x" {
		t.Errorf("expected one cited url, got %v", res.CitedURLs)
	}
}

func TestRunner_TracksToolUseURLs(t *testing.T) {
	api := &scriptedAPI{
		responses: []apiResponse{
			{
				StopReason:   "tool_use",
				InputTokens:  50,
				OutputTokens: 10,
				ToolCalls: []toolCall{
					{ID: "t1", Name: "fetch_url", Input: map[string]any{"url": "https://docs.example.com/fetched"}},
					{ID: "t2", Name: "get_page", Input: map[string]any{"url": "https://docs.example.com/page"}},
				},
			},
			{
				StopReason:   "end_turn",
				InputTokens:  20,
				OutputTokens: 5,
				FinalText:    "The answer is 42.", // no URL in answer text
			},
		},
	}
	tools := map[string]ToolHandler{
		"fetch_url": func(_ context.Context, _ map[string]any) (string, error) { return "x", nil },
		"get_page":  func(_ context.Context, _ map[string]any) (string, error) { return "y", nil },
	}
	res, err := RunTrial(context.Background(), api, tools, ConfigATools(), "q?", RunLimits{MaxRounds: 5})
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if len(res.CitedURLs) != 2 {
		t.Fatalf("want 2 cited URLs, got %v", res.CitedURLs)
	}
	want := map[string]bool{
		"https://docs.example.com/fetched": true,
		"https://docs.example.com/page":    true,
	}
	for _, u := range res.CitedURLs {
		if !want[u] {
			t.Errorf("unexpected URL %q in CitedURLs", u)
		}
	}
}

func TestRunner_DedupsCitedURLs(t *testing.T) {
	url := "https://docs.example.com/x"
	api := &scriptedAPI{
		responses: []apiResponse{
			{
				StopReason: "tool_use", InputTokens: 1, OutputTokens: 1,
				ToolCalls: []toolCall{{ID: "t1", Name: "fetch_url", Input: map[string]any{"url": url}}},
			},
			{
				StopReason: "end_turn", InputTokens: 1, OutputTokens: 1,
				FinalText: "Source: " + url + " — answer.",
			},
		},
	}
	tools := map[string]ToolHandler{"fetch_url": func(_ context.Context, _ map[string]any) (string, error) { return "x", nil }}
	res, err := RunTrial(context.Background(), api, tools, ConfigATools(), "q?", RunLimits{MaxRounds: 5})
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if len(res.CitedURLs) != 1 || res.CitedURLs[0] != url {
		t.Fatalf("want exactly [%s], got %v", url, res.CitedURLs)
	}
}

func TestRunner_AbortsOnMaxRounds(t *testing.T) {
	loopResp := apiResponse{
		StopReason:   "tool_use",
		InputTokens:  10,
		OutputTokens: 1,
		ToolCalls:    []toolCall{{ID: "t", Name: "fetch_url", Input: map[string]any{"url": "https://x"}}},
	}
	api := &scriptedAPI{responses: []apiResponse{loopResp, loopResp, loopResp, loopResp}}
	tools := map[string]ToolHandler{
		"fetch_url": func(_ context.Context, _ map[string]any) (string, error) { return "x", nil },
	}
	res, err := RunTrial(context.Background(), api, tools, ConfigATools(), "q?", RunLimits{MaxRounds: 2})
	if err != nil {
		t.Fatalf("RunTrial: %v", err)
	}
	if !res.Aborted {
		t.Error("expected aborted=true after MaxRounds")
	}
}
