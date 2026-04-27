// cmd/bench/tasks_cmd.go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mathwro/DocuMcp/internal/bench/report"
	"github.com/mathwro/DocuMcp/internal/bench/tasks"
)

func runTasks(args []string) {
	fs := flag.NewFlagSet("tasks", flag.ExitOnError)
	corpusPath := fs.String("questions", "internal/bench/corpus/questions.json", "path to questions.json")
	trials := fs.Int("trials", 3, "trials per (question, config)")
	_ = fs.Parse(args)

	apiKey := mustEnv("ANTHROPIC_API_KEY")
	docURL := envOr("DOCUMCP_BENCH_URL", "http://127.0.0.1:8080")
	bearer := os.Getenv("DOCUMCP_API_KEY")
	ctx := context.Background()

	known, err := fetchSourceSet(docURL, bearer)
	if err != nil {
		fatal("fetch sources: %v", err)
	}
	corpus, err := tasks.LoadCorpus(*corpusPath, known)
	if err != nil {
		fatal("corpus: %v", err)
	}
	if len(corpus) == 0 {
		fatal("corpus is empty")
	}

	api := tasks.NewAnthropicAPI(apiKey, "claude-sonnet-4-6")
	mcp := tasks.NewMCPClient(docURL+"/mcp", bearer)

	allTrials := make([]tasks.TrialResult, 0, len(corpus)*2*(*trials))
	tiers := make(map[string]int, len(corpus))
	var judgeAcc tasks.JudgeAccounting

	for _, q := range corpus {
		tiers[q.ID] = q.Tier
		for _, cfg := range []string{"A", "B"} {
			for i := 1; i <= *trials; i++ {
				res := runOneTrial(ctx, api, mcp, q, cfg, i)
				ji, jo := judgeOne(ctx, api, mcp, q, &res)
				judgeAcc.InputTokens += ji
				judgeAcc.OutputTokens += jo
				allTrials = append(allTrials, res)
				fmt.Printf("%s [%s/%d] tokens=%d correct=%v aborted=%v\n",
					q.ID, cfg, i, res.TotalTokens(), res.Correct, res.Aborted)
			}
		}
	}

	dir := newOutputDir()
	rep := report.Report{
		Metadata: report.Metadata{
			Model:      "claude-sonnet-4-6",
			GitSHA:     gitSHA(),
			CorpusHash: hashFile(*corpusPath),
			Timestamp:  time.Now().UTC(),
		},
		Trials: allTrials,
		Tiers:  tiers,
		Judge:  judgeAcc,
	}
	if err := report.WriteJSON(filepath.Join(dir, "results.json"), rep); err != nil {
		fatal("write json: %v", err)
	}
	if err := report.WriteMarkdown(filepath.Join(dir, "summary.md"), rep); err != nil {
		fatal("write md: %v", err)
	}
	fmt.Printf("tasks complete: %d trials — output: %s\n", len(allTrials), dir)
}

func runOneTrial(ctx context.Context, api tasks.API, mcp *tasks.MCPClient, q tasks.Question, cfg string, trial int) tasks.TrialResult {
	tools, handlers := buildConfig(cfg, mcp)
	res, err := tasks.RunTrial(ctx, api, handlers, tools, q.Question, tasks.RunLimits{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "trial %s/%s/%d: %v\n", q.ID, cfg, trial, err)
		res.Aborted = true
	}
	res.QuestionID = q.ID
	res.Config = cfg
	res.Trial = trial
	return res
}

func buildConfig(cfg string, mcp *tasks.MCPClient) ([]map[string]any, map[string]tasks.ToolHandler) {
	switch cfg {
	case "A":
		return tasks.ConfigATools(), map[string]tasks.ToolHandler{
			"fetch_url": func(ctx context.Context, args map[string]any) (string, error) {
				url, _ := args["url"].(string)
				return tasks.FetchURL(ctx, http.DefaultClient, url, 50_000)
			},
		}
	case "B":
		mcpHandler := func(name string) tasks.ToolHandler {
			return func(ctx context.Context, args map[string]any) (string, error) {
				return mcp.CallTool(ctx, name, args)
			}
		}
		return tasks.ConfigBTools(), map[string]tasks.ToolHandler{
			"list_sources":  mcpHandler("list_sources"),
			"search_docs":   mcpHandler("search_docs"),
			"browse_source": mcpHandler("browse_source"),
			"get_page":      mcpHandler("get_page"),
		}
	default:
		fatal("unknown config: %s", cfg)
		return nil, nil
	}
}

func judgeOne(ctx context.Context, api tasks.API, mcp *tasks.MCPClient, q tasks.Question, res *tasks.TrialResult) (int, int) {
	// Verify URL pattern matches at least one cited URL — otherwise mark incorrect
	// without spending judge tokens.
	hasMatch := false
	for _, u := range res.CitedURLs {
		if q.URLRegex().MatchString(u) {
			hasMatch = true
			break
		}
	}
	if !hasMatch && len(res.CitedURLs) > 0 {
		res.Correct = false
		res.JudgeReason = "cited URL did not match expected_url_pattern"
		return 0, 0
	}

	fetched := fetchCitedContent(ctx, mcp, res.CitedURLs)
	jr, jin, jout, err := tasks.Judge(ctx, api, tasks.JudgeInput{
		Question:         q.Question,
		Answer:           res.FinalAnswer,
		ReferenceExcerpt: q.ReferenceExcerpt,
		FetchedSources:   fetched,
	})
	if err != nil {
		res.JudgeReason = "judge error: " + err.Error()
		return jin, jout
	}
	res.Correct = jr.Correct
	res.JudgeReason = jr.Reason
	return jin, jout
}

func fetchCitedContent(ctx context.Context, mcp *tasks.MCPClient, urls []string) string {
	if len(urls) == 0 {
		return "(no URLs cited)"
	}
	var b strings.Builder
	for _, u := range urls {
		body, err := mcp.CallTool(ctx, "get_page", map[string]any{"url": u})
		if err != nil || body == "" {
			body, err = tasks.FetchURL(ctx, http.DefaultClient, u, 50_000)
			if err != nil {
				b.WriteString(fmt.Sprintf("[%s — fetch error: %v]\n", u, err))
				continue
			}
		}
		fmt.Fprintf(&b, "--- %s ---\n%s\n\n", u, body)
	}
	return b.String()
}

func fetchSourceSet(baseURL, bearer string) (map[string]bool, error) {
	names, err := fetchSources(baseURL, bearer)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out, nil
}

func gitSHA() string {
	out, err := os.ReadFile(".git/HEAD")
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if strings.HasPrefix(s, "ref: ") {
		ref := strings.TrimPrefix(s, "ref: ")
		body, err := os.ReadFile(filepath.Join(".git", ref))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(body))
	}
	return s
}

func hashFile(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
