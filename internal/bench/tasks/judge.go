// internal/bench/tasks/judge.go
package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
)

type JudgeInput struct {
	Question         string
	Answer           string
	ReferenceExcerpt string
	FetchedSources   string // text content of cited URLs (joined with separators)
}

type JudgeResult struct {
	Correct bool   `json:"correct"`
	Reason  string `json:"reason"`
}

const judgePrompt = `You are evaluating whether a documentation-lookup answer is correct.

QUESTION: %s

REFERENCE EXCERPT (ground truth): %s

CITED SOURCE CONTENT: %s

ANSWER UNDER EVALUATION: %s

Reply with a single JSON object on one line: {"correct": <bool>, "reason": "<short justification>"}. The answer is "correct" if it factually matches the reference, even if phrased differently. The answer is "incorrect" if it contradicts, omits the asked-for fact, or fabricates.`

// jsonObjRe is intentionally tolerant — the judge sometimes wraps its JSON in
// prose. We grab the first {...} block and parse it.
var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// Judge runs the judge call. Returns the result plus judge token counts (so the
// runner can keep them out of the per-config totals).
func Judge(ctx context.Context, api API, in JudgeInput) (JudgeResult, int, int, error) {
	prompt := fmt.Sprintf(judgePrompt, in.Question, in.ReferenceExcerpt, in.FetchedSources, in.Answer)
	resp, err := api.Send(ctx, nil, []map[string]any{
		{"role": "user", "content": prompt},
	})
	if err != nil {
		return JudgeResult{}, 0, 0, fmt.Errorf("judge api: %w", err)
	}
	match := jsonObjRe.FindString(resp.FinalText)
	if match == "" {
		return JudgeResult{}, resp.InputTokens, resp.OutputTokens, fmt.Errorf("no JSON object in judge output: %q", resp.FinalText)
	}
	var r JudgeResult
	if err := json.Unmarshal([]byte(match), &r); err != nil {
		return JudgeResult{}, resp.InputTokens, resp.OutputTokens, fmt.Errorf("parse judge json: %w (raw: %s)", err, match)
	}
	return r, resp.InputTokens, resp.OutputTokens, nil
}
