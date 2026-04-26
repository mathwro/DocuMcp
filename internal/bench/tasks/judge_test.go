// internal/bench/tasks/judge_test.go
package tasks

import (
	"context"
	"errors"
	"testing"
)

type judgeAPI struct {
	resp apiResponse
	err  error
}

func (j *judgeAPI) Send(_ context.Context, _ []map[string]any, _ []map[string]any) (apiResponse, error) {
	return j.resp, j.err
}

func TestJudge_ParsesCorrectTrue(t *testing.T) {
	j := &judgeAPI{resp: apiResponse{
		StopReason:   "end_turn",
		InputTokens:  30,
		OutputTokens: 10,
		FinalText:    `{"correct": true, "reason": "matches reference"}`,
	}}
	res, judgeIn, judgeOut, err := Judge(context.Background(), j, JudgeInput{
		Question:         "q",
		Answer:           "a",
		ReferenceExcerpt: "r",
		FetchedSources:   "src",
	})
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if !res.Correct || res.Reason != "matches reference" {
		t.Errorf("unexpected: %+v", res)
	}
	if judgeIn != 30 || judgeOut != 10 {
		t.Errorf("token accounting: in=%d out=%d", judgeIn, judgeOut)
	}
}

func TestJudge_HandlesMalformedJSON(t *testing.T) {
	j := &judgeAPI{resp: apiResponse{
		StopReason:   "end_turn",
		InputTokens:  5,
		OutputTokens: 5,
		FinalText:    "not json at all",
	}}
	_, _, _, err := Judge(context.Background(), j, JudgeInput{})
	if err == nil {
		t.Error("expected error on malformed judge response")
	}
}

func TestJudge_PropagatesAPIError(t *testing.T) {
	j := &judgeAPI{err: errors.New("boom")}
	_, _, _, err := Judge(context.Background(), j, JudgeInput{})
	if err == nil {
		t.Error("expected error propagation")
	}
}
