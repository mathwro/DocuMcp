// internal/bench/tasks/types.go
package tasks

import "regexp"

// Question is a single benchmark prompt + ground truth.
type Question struct {
	ID                 string         `json:"id"`
	Tier               int            `json:"tier"`
	Question           string         `json:"question"`
	ExpectedSource     string         `json:"expected_source"`
	ExpectedURLPattern string         `json:"expected_url_pattern"`
	ReferenceExcerpt   string         `json:"reference_excerpt"`
	Notes              string         `json:"notes,omitempty"`
	urlRegex           *regexp.Regexp // populated by LoadCorpus
}

// URLRegex returns the compiled expected_url_pattern. Always non-nil after LoadCorpus.
func (q *Question) URLRegex() *regexp.Regexp { return q.urlRegex }

// TrialResult is one (question, config, trial) outcome.
type TrialResult struct {
	QuestionID   string   `json:"question_id"`
	Config       string   `json:"config"` // "A" or "B"
	Trial        int      `json:"trial"`
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	ToolCalls    int      `json:"tool_calls"`
	Aborted      bool     `json:"aborted"`
	Correct      bool     `json:"correct"`
	JudgeReason  string   `json:"judge_reason"`
	FinalAnswer  string   `json:"final_answer"`
	CitedURLs    []string `json:"cited_urls"`
}

// TotalTokens is input + output (excludes judge tokens, which are tracked separately).
func (t TrialResult) TotalTokens() int { return t.InputTokens + t.OutputTokens }

// JudgeAccounting tracks judge-only token spend across the run.
type JudgeAccounting struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
