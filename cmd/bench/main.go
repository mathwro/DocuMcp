package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type task struct {
	ID                  string   `json:"id"`
	Prompt              string   `json:"prompt"`
	Source              string   `json:"source,omitempty"`
	ExpectedContains    []string `json:"expected_contains,omitempty"`
	ExpectedURLContains []string `json:"expected_url_contains,omitempty"`
}

type eventMetrics struct {
	EventCount        int     `json:"event_count"`
	InputTokens       int64   `json:"input_tokens,omitempty"`
	CachedInputTokens int64   `json:"cached_input_tokens,omitempty"`
	OutputTokens      int64   `json:"output_tokens,omitempty"`
	TotalTokens       int64   `json:"total_tokens,omitempty"`
	CostUSD           float64 `json:"cost_usd,omitempty"`
	WebSearchCalls    int     `json:"web_search_calls,omitempty"`
	MCPToolCalls      int     `json:"mcp_tool_calls,omitempty"`
	ToolCalls         int     `json:"tool_calls,omitempty"`
}

type result struct {
	TaskID        string       `json:"task_id"`
	Mode          string       `json:"mode"`
	Run           int          `json:"run"`
	Model         string       `json:"model,omitempty"`
	StartedAt     time.Time    `json:"started_at"`
	DurationMS    int64        `json:"duration_ms"`
	ExitCode      int          `json:"exit_code"`
	FinalAnswer   string       `json:"final_answer,omitempty"`
	RawEventsPath string       `json:"raw_events_path,omitempty"`
	StdoutBytes   int          `json:"stdout_bytes"`
	Stderr        string       `json:"stderr,omitempty"`
	Metrics       eventMetrics `json:"metrics"`
	CorrectHints  hintResult   `json:"correct_hints"`
}

type hintResult struct {
	AnswerContainsMatched bool     `json:"answer_contains_matched"`
	MissingAnswerContains []string `json:"missing_answer_contains,omitempty"`
	URLContainsMatched    bool     `json:"url_contains_matched"`
	MissingURLContains    []string `json:"missing_url_contains,omitempty"`
}

type report struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Results     []result     `json:"results"`
	Summary     []summary    `json:"summary"`
	Comparison  []comparison `json:"comparison,omitempty"`
}

type summary struct {
	Mode              string  `json:"mode"`
	Runs              int     `json:"runs"`
	MedianDurationMS  int64   `json:"median_duration_ms"`
	MedianInputTokens int64   `json:"median_input_tokens,omitempty"`
	MedianTotalTokens int64   `json:"median_total_tokens,omitempty"`
	MedianToolCalls   int64   `json:"median_tool_calls,omitempty"`
	Successes         int     `json:"successes"`
	SuccessRate       float64 `json:"success_rate"`
}

type comparison struct {
	TaskID            string  `json:"task_id"`
	NativeDurationMS  int64   `json:"native_duration_ms"`
	DocuMCPDurationMS int64   `json:"documcp_duration_ms"`
	DurationRatio     float64 `json:"duration_ratio"`
	NativeTokens      int64   `json:"native_tokens"`
	DocuMCPTokens     int64   `json:"documcp_tokens"`
	TokenRatio        float64 `json:"token_ratio"`
}

type runConfig struct {
	CodexCommand string
	TasksPath    string
	Mode         string
	Model        string
	Workdir      string
	OutputPath   string
	RawDir       string
	DocuMCPURL   string
	Runs         int
	Timeout      time.Duration
	DryRun       bool
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := run(context.Background(), cfg, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseFlags(args []string) (runConfig, error) {
	var cfg runConfig
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.StringVar(&cfg.CodexCommand, "codex", "codex", "codex executable")
	fs.StringVar(&cfg.TasksPath, "tasks", "", "JSONL task file")
	fs.StringVar(&cfg.Mode, "mode", "both", "benchmark mode: native, documcp, mcp_noop, both, or all")
	fs.StringVar(&cfg.Model, "model", "", "optional Codex model override")
	fs.StringVar(&cfg.Workdir, "workdir", ".", "working directory passed to codex exec")
	fs.StringVar(&cfg.OutputPath, "out", "bench-results.json", "JSON output path")
	fs.StringVar(&cfg.RawDir, "raw-dir", "bench-events", "directory for raw Codex JSONL event files")
	fs.StringVar(&cfg.DocuMCPURL, "documcp-url", "http://localhost:8080/mcp/http", "DocuMcp streamable HTTP endpoint")
	fs.IntVar(&cfg.Runs, "runs", 1, "runs per task per mode")
	fs.DurationVar(&cfg.Timeout, "timeout", 10*time.Minute, "timeout per Codex run")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "print planned commands without running Codex")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.TasksPath == "" {
		return cfg, errors.New("-tasks is required")
	}
	if cfg.Runs < 1 {
		return cfg, errors.New("-runs must be at least 1")
	}
	if len(modesFor(cfg.Mode)) == 0 {
		return cfg, fmt.Errorf("unsupported -mode %q", cfg.Mode)
	}
	return cfg, nil
}

func run(ctx context.Context, cfg runConfig, out io.Writer) error {
	tasks, err := loadTasks(cfg.TasksPath)
	if err != nil {
		return err
	}
	modes := modesFor(cfg.Mode)

	var results []result
	for _, t := range tasks {
		for _, mode := range modes {
			for i := 1; i <= cfg.Runs; i++ {
				res, err := runOne(ctx, cfg, t, mode, i)
				if err != nil {
					return err
				}
				results = append(results, res)
				fmt.Fprintf(out, "%s %s run %d: exit=%d duration=%dms tokens=%d tools=%d\n",
					t.ID, mode, i, res.ExitCode, res.DurationMS, res.Metrics.TotalTokens, res.Metrics.ToolCalls)
			}
		}
	}

	rep := report{
		GeneratedAt: time.Now().UTC(),
		Results:     results,
		Summary:     summarize(results),
		Comparison:  comparePairs(results),
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(cfg.OutputPath, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	fmt.Fprintf(out, "wrote %s\n", cfg.OutputPath)
	return nil
}

func runOne(parent context.Context, cfg runConfig, t task, mode string, runNumber int) (result, error) {
	tempDir, err := os.MkdirTemp("", "documcp-bench-*")
	if err != nil {
		return result{}, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	finalPath := filepath.Join(tempDir, "final.txt")
	args := buildCodexArgs(cfg, mode, finalPath, promptFor(t, mode))
	started := time.Now().UTC()
	res := result{
		TaskID:    t.ID,
		Mode:      mode,
		Run:       runNumber,
		Model:     cfg.Model,
		StartedAt: started,
	}
	if cfg.RawDir != "" {
		res.RawEventsPath = rawEventsPath(cfg.RawDir, res)
	}
	if cfg.DryRun {
		res.FinalAnswer = strings.Join(append([]string{cfg.CodexCommand}, args...), " ")
		res.DurationMS = int64(time.Since(started) / time.Millisecond)
		return res, nil
	}

	ctx, cancel := context.WithTimeout(parent, cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.CodexCommand, args...)
	cmd.Dir = cfg.Workdir
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	res.DurationMS = int64(time.Since(started) / time.Millisecond)
	res.StdoutBytes = stdout.Len()
	if cfg.RawDir != "" {
		if err := writeRawEvents(res.RawEventsPath, stdout.Bytes()); err != nil {
			return res, err
		}
	}
	res.Stderr = strings.TrimSpace(stderr.String())
	res.Metrics = analyzeCodexEvents(stdout.Bytes())
	final, readErr := os.ReadFile(finalPath)
	if readErr == nil {
		res.FinalAnswer = strings.TrimSpace(string(final))
	}
	res.CorrectHints = evaluateHints(t, res.FinalAnswer, stdout.String())

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		if ctx.Err() != nil {
			res.ExitCode = -1
			res.Stderr = strings.TrimSpace(res.Stderr + "\n" + ctx.Err().Error())
			return res, nil
		}
		return res, fmt.Errorf("run codex: %w", err)
	}
	return res, nil
}

func loadTasks(path string) ([]task, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open tasks: %w", err)
	}
	defer f.Close()

	var tasks []task
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var t task
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			return nil, fmt.Errorf("parse tasks line %d: %w", lineNo, err)
		}
		if t.ID == "" {
			return nil, fmt.Errorf("parse tasks line %d: id is required", lineNo)
		}
		if t.Prompt == "" {
			return nil, fmt.Errorf("parse tasks line %d: prompt is required", lineNo)
		}
		tasks = append(tasks, t)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read tasks: %w", err)
	}
	if len(tasks) == 0 {
		return nil, errors.New("no tasks loaded")
	}
	return tasks, nil
}

func buildCodexArgs(cfg runConfig, mode, finalPath, prompt string) []string {
	args := []string{}
	if mode == "native" {
		args = append(args, "--search")
	}
	args = append(args, "exec", "--ignore-user-config", "--json", "-o", finalPath, "-C", cfg.Workdir)
	if cfg.Model != "" {
		args = append(args, "-m", cfg.Model)
	}
	if mode == "documcp" || mode == "mcp_noop" {
		args = append(args, "-c", fmt.Sprintf("mcp_servers.documcp.url=%q", cfg.DocuMCPURL))
	}
	args = append(args, prompt)
	return args
}

func promptFor(t task, mode string) string {
	if mode == "mcp_noop" {
		return "Reply exactly: OK\n\nDo not call any tools."
	}
	var b strings.Builder
	b.WriteString("Answer the documentation question below. Keep the answer concise and cite the source URL you used.\n\n")
	if mode == "documcp" {
		b.WriteString("Use the DocuMcp MCP server for documentation lookup. Do not call list_sources. Call search_docs exactly once")
		if t.Source != "" {
			fmt.Fprintf(&b, " and pass source=%q", t.Source)
		}
		b.WriteString(". Answer from the search_docs snippets when they contain the answer. At most one get_page_excerpt call is allowed, and only if the snippet is insufficient. Do not call get_page unless the excerpt is also insufficient. Do not call browse_source.\n\n")
	} else {
		b.WriteString("Use native web search/documentation lookup tools. Prefer official documentation pages.\n\n")
	}
	b.WriteString(t.Prompt)
	return b.String()
}

func modesFor(mode string) []string {
	switch mode {
	case "native":
		return []string{"native"}
	case "documcp":
		return []string{"documcp"}
	case "mcp_noop":
		return []string{"mcp_noop"}
	case "both":
		return []string{"native", "documcp"}
	case "all":
		return []string{"native", "documcp", "mcp_noop"}
	default:
		return nil
	}
}

func rawEventsPath(rawDir string, res result) string {
	return filepath.Join(rawDir, fmt.Sprintf("%s__%s__run-%d.jsonl", sanitizeFilename(res.TaskID), sanitizeFilename(res.Mode), res.Run))
}

func sanitizeFilename(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "run"
	}
	return out
}

func writeRawEvents(path string, data []byte) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create raw events dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write raw events: %w", err)
	}
	return nil
}

func analyzeCodexEvents(jsonl []byte) eventMetrics {
	var metrics eventMetrics
	scanner := bufio.NewScanner(bytes.NewReader(jsonl))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		metrics.EventCount++
		flattenEvent(event, &metrics)
		kinds := toolKinds(event)
		if kinds.webSearch {
			metrics.WebSearchCalls++
		}
		if kinds.mcpTool {
			metrics.MCPToolCalls++
		}
		metrics.ToolCalls = metrics.WebSearchCalls + metrics.MCPToolCalls
	}
	if metrics.TotalTokens == 0 && (metrics.InputTokens != 0 || metrics.OutputTokens != 0) {
		metrics.TotalTokens = metrics.InputTokens + metrics.OutputTokens
	}
	return metrics
}

func flattenEvent(v any, metrics *eventMetrics) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			key := strings.ToLower(k)
			switch key {
			case "input_tokens":
				metrics.InputTokens = maxInt64(metrics.InputTokens, numberToInt64(val))
			case "cached_input_tokens":
				metrics.CachedInputTokens = maxInt64(metrics.CachedInputTokens, numberToInt64(val))
			case "output_tokens":
				metrics.OutputTokens = maxInt64(metrics.OutputTokens, numberToInt64(val))
			case "total_tokens":
				metrics.TotalTokens = maxInt64(metrics.TotalTokens, numberToInt64(val))
			case "cost_usd":
				metrics.CostUSD = maxFloat(metrics.CostUSD, numberToFloat64(val))
			}
			flattenEvent(val, metrics)
		}
	case []any:
		for _, item := range x {
			flattenEvent(item, metrics)
		}
	}
}

type toolKindFlags struct {
	webSearch bool
	mcpTool   bool
}

func toolKinds(v any) toolKindFlags {
	var flags toolKindFlags
	findToolKinds(v, &flags)
	return flags
}

func findToolKinds(v any, flags *toolKindFlags) {
	switch x := v.(type) {
	case map[string]any:
		for _, val := range x {
			findToolKinds(val, flags)
		}
	case []any:
		for _, item := range x {
			findToolKinds(item, flags)
		}
	case string:
		countToolString(x, flags)
	}
}

func countToolString(s string, flags *toolKindFlags) {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "web_search"):
		flags.webSearch = true
	case strings.Contains(s, "mcp") && strings.Contains(s, "tool"):
		flags.mcpTool = true
	case strings.Contains(s, "search_docs"), strings.Contains(s, "browse_source"), strings.Contains(s, "get_page"), strings.Contains(s, "list_sources"):
		flags.mcpTool = true
	}
}

func numberToInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	default:
		return 0
	}
}

func numberToFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	default:
		return 0
	}
}

func evaluateHints(t task, finalAnswer, rawEvents string) hintResult {
	h := hintResult{
		AnswerContainsMatched: len(t.ExpectedContains) == 0,
		URLContainsMatched:    len(t.ExpectedURLContains) == 0,
	}
	answerLower := strings.ToLower(finalAnswer)
	for _, needle := range t.ExpectedContains {
		if !strings.Contains(answerLower, strings.ToLower(needle)) {
			h.MissingAnswerContains = append(h.MissingAnswerContains, needle)
		}
	}
	if len(h.MissingAnswerContains) == 0 {
		h.AnswerContainsMatched = true
	}
	combinedLower := strings.ToLower(finalAnswer + "\n" + rawEvents)
	for _, needle := range t.ExpectedURLContains {
		if !strings.Contains(combinedLower, strings.ToLower(needle)) {
			h.MissingURLContains = append(h.MissingURLContains, needle)
		}
	}
	if len(h.MissingURLContains) == 0 {
		h.URLContainsMatched = true
	}
	return h
}

func summarize(results []result) []summary {
	byMode := map[string][]result{}
	for _, r := range results {
		byMode[r.Mode] = append(byMode[r.Mode], r)
	}
	out := make([]summary, 0, len(byMode))
	for mode, rs := range byMode {
		var durations, inputs, totals, tools []int64
		successes := 0
		for _, r := range rs {
			durations = append(durations, r.DurationMS)
			inputs = append(inputs, r.Metrics.InputTokens)
			totals = append(totals, r.Metrics.TotalTokens)
			tools = append(tools, int64(r.Metrics.ToolCalls))
			if isSuccessful(r) {
				successes++
			}
		}
		out = append(out, summary{
			Mode:              mode,
			Runs:              len(rs),
			MedianDurationMS:  medianInt64(durations),
			MedianInputTokens: medianInt64(inputs),
			MedianTotalTokens: medianInt64(totals),
			MedianToolCalls:   medianInt64(tools),
			Successes:         successes,
			SuccessRate:       float64(successes) / float64(len(rs)),
		})
	}
	return out
}

func isSuccessful(r result) bool {
	if r.ExitCode != 0 {
		return false
	}
	if r.Mode == "mcp_noop" {
		return strings.TrimSpace(r.FinalAnswer) == "OK" && r.Metrics.ToolCalls == 0
	}
	return r.CorrectHints.AnswerContainsMatched && r.CorrectHints.URLContainsMatched
}

func comparePairs(results []result) []comparison {
	type pair struct {
		native  *result
		documcp *result
	}
	pairs := map[string]*pair{}
	for i := range results {
		r := &results[i]
		p := pairs[r.TaskID]
		if p == nil {
			p = &pair{}
			pairs[r.TaskID] = p
		}
		switch r.Mode {
		case "native":
			p.native = r
		case "documcp":
			p.documcp = r
		}
	}
	out := []comparison{}
	for taskID, p := range pairs {
		if p.native == nil || p.documcp == nil {
			continue
		}
		cmp := comparison{
			TaskID:            taskID,
			NativeDurationMS:  p.native.DurationMS,
			DocuMCPDurationMS: p.documcp.DurationMS,
			NativeTokens:      p.native.Metrics.TotalTokens,
			DocuMCPTokens:     p.documcp.Metrics.TotalTokens,
		}
		if cmp.NativeDurationMS > 0 {
			cmp.DurationRatio = float64(cmp.DocuMCPDurationMS) / float64(cmp.NativeDurationMS)
		}
		if cmp.NativeTokens > 0 {
			cmp.TokenRatio = float64(cmp.DocuMCPTokens) / float64(cmp.NativeTokens)
		}
		out = append(out, cmp)
	}
	return out
}

func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]int64(nil), values...)
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j-1] > cp[j]; j-- {
			cp[j-1], cp[j] = cp[j], cp[j-1]
		}
	}
	return cp[len(cp)/2]
}

func maxInt64(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}

func maxFloat(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}
