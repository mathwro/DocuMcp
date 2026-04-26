// internal/bench/tasks/runner.go
package tasks

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

// API is the minimum surface the runner needs. Production: anthropicAPI{}.
// Tests: scriptedAPI{}. system prompt is fixed by the runner.
type API interface {
	Send(ctx context.Context, tools []map[string]any, messages []map[string]any) (apiResponse, error)
}

type apiResponse struct {
	StopReason   string
	InputTokens  int
	OutputTokens int
	ToolCalls    []toolCall // populated if StopReason == "tool_use"
	FinalText    string     // populated for both end_turn and tool_use (the model's text preamble)
}

type toolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

// ToolHandler executes a function tool locally and returns the text result.
// Server tools (e.g. web_search) return ("", nil) — the API handles them.
type ToolHandler func(ctx context.Context, args map[string]any) (string, error)

type RunLimits struct {
	MaxRounds      int
	PerCallTimeout time.Duration
	OverallTimeout time.Duration
}

const systemPrompt = "You are answering a documentation question. Use the available tools to find the answer. Cite the URL of the page where you found the information. Keep your final answer concise — quote or paraphrase only what's needed to answer."

// urlRe finds bare URLs in the agent's final answer; we use them as cited URLs.
var urlRe = regexp.MustCompile(`https?://[^\s<>"')]+`)

// RunTrial executes one (question, config) trial. Returns a TrialResult with
// token totals and the final answer. Correctness is set by the judge later.
func RunTrial(ctx context.Context, api API, handlers map[string]ToolHandler, tools []map[string]any, question string, lim RunLimits) (TrialResult, error) {
	if lim.MaxRounds == 0 {
		lim.MaxRounds = 15
	}
	if lim.PerCallTimeout == 0 {
		lim.PerCallTimeout = 30 * time.Second
	}
	if lim.OverallTimeout == 0 {
		lim.OverallTimeout = 5 * time.Minute
	}
	overallCtx, cancel := context.WithTimeout(ctx, lim.OverallTimeout)
	defer cancel()

	messages := []map[string]any{
		{"role": "user", "content": question},
	}
	res := TrialResult{}
	for round := 0; round < lim.MaxRounds; round++ {
		resp, err := api.Send(overallCtx, tools, prependSystem(messages))
		if err != nil {
			return res, fmt.Errorf("api send: %w", err)
		}
		res.InputTokens += resp.InputTokens
		res.OutputTokens += resp.OutputTokens

		if resp.StopReason == "end_turn" {
			res.FinalAnswer = resp.FinalText
			res.CitedURLs = urlRe.FindAllString(resp.FinalText, -1)
			return res, nil
		}
		if resp.StopReason != "tool_use" {
			res.Aborted = true
			return res, nil
		}

		// Build the assistant message that produced these tool calls. We must echo
		// the tool_use blocks back so the API can match them to the tool_results.
		// Any text the model emitted alongside the tool calls is preserved.
		assistantContent := []map[string]any{}
		if resp.FinalText != "" {
			assistantContent = append(assistantContent, map[string]any{
				"type": "text", "text": resp.FinalText,
			})
		}
		for _, tc := range resp.ToolCalls {
			assistantContent = append(assistantContent, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Name,
				"input": tc.Input,
			})
		}

		// Execute each tool call, build the matching tool_result blocks.
		toolResults := []map[string]any{}
		for _, tc := range resp.ToolCalls {
			res.ToolCalls++
			h, ok := handlers[tc.Name]
			var (
				out  string
				ferr error
			)
			if ok {
				callCtx, cancelCall := context.WithTimeout(overallCtx, lim.PerCallTimeout)
				out, ferr = h(callCtx, tc.Input)
				cancelCall()
			}
			toolResults = append(toolResults, map[string]any{
				"type":        "tool_result",
				"tool_use_id": tc.ID,
				"content":     toolText(out, ferr),
				"is_error":    ferr != nil,
			})
		}
		messages = append(messages,
			map[string]any{"role": "assistant", "content": assistantContent},
			map[string]any{"role": "user", "content": toolResults},
		)
	}
	res.Aborted = true
	return res, nil
}

func toolText(out string, err error) string {
	if err != nil {
		return fmt.Sprintf("tool error: %v", err)
	}
	return out
}

func prependSystem(msgs []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(msgs)+1)
	out = append(out, map[string]any{"role": "system", "content": systemPrompt})
	return append(out, msgs...)
}
