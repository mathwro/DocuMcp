// internal/bench/report/markdown.go
package report

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mathwro/DocuMcp/internal/bench/tasks"
)

func WriteMarkdown(path string, rep Report) error {
	var b strings.Builder
	b.WriteString("# DocuMcp Token-Savings Benchmark\n\n")
	fmt.Fprintf(&b, "_Model: %s — Generated: %s_\n\n",
		rep.Metadata.Model, rep.Metadata.Timestamp.Format("2006-01-02 15:04:05 MST"))

	writeHeadline(&b, rep.Trials)
	writePerTier(&b, rep.Trials, rep.Tiers)
	writePageDiffTable(&b, rep)
	writeRates(&b, rep.Trials)
	writeJudgeCost(&b, rep.Judge)
	writeSkippedLog(&b, rep)

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeHeadline(b *strings.Builder, trials []tasks.TrialResult) {
	a := totalsForConfig(trials, "A")
	bb := totalsForConfig(trials, "B")
	b.WriteString("## Headline\n\n")
	b.WriteString("| Config | N (correct) | Mean tokens | 95% CI |\n")
	b.WriteString("|---|---|---|---|\n")
	fmt.Fprintf(b, "| Config A (no DocuMcp) | %d | %.0f | [%.0f, %.0f] |\n", a.n, a.mean, a.lo, a.hi)
	fmt.Fprintf(b, "| Config B (DocuMcp)    | %d | %.0f | [%.0f, %.0f] |\n", bb.n, bb.mean, bb.lo, bb.hi)
	if a.mean > 0 && bb.mean > 0 {
		fmt.Fprintf(b, "\n**DocuMcp savings: %.1f%%**\n\n", 100*(a.mean-bb.mean)/a.mean)
	}
}

func writePerTier(b *strings.Builder, trials []tasks.TrialResult, tiers map[string]int) {
	if len(tiers) == 0 {
		return
	}
	b.WriteString("## Per-Tier Breakdown\n\n")
	b.WriteString("| Tier | Config | N | Mean tokens | 95% CI |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for tier := 1; tier <= 3; tier++ {
		filtered := make([]tasks.TrialResult, 0)
		for _, t := range trials {
			if tiers[t.QuestionID] == tier {
				filtered = append(filtered, t)
			}
		}
		ta := totalsForConfig(filtered, "A")
		tb := totalsForConfig(filtered, "B")
		fmt.Fprintf(b, "| Tier %d | A | %d | %.0f | [%.0f, %.0f] |\n", tier, ta.n, ta.mean, ta.lo, ta.hi)
		fmt.Fprintf(b, "| Tier %d | B | %d | %.0f | [%.0f, %.0f] |\n", tier, tb.n, tb.mean, tb.lo, tb.hi)
	}
	b.WriteByte('\n')
}

func writePageDiffTable(b *strings.Builder, rep Report) {
	if rep.PageDiff == nil || len(rep.PageDiff.Rows) == 0 {
		return
	}
	rows := make([]int, len(rep.PageDiff.Rows))
	for i := range rows {
		rows[i] = i
	}
	sort.Slice(rows, func(i, j int) bool {
		return rep.PageDiff.Rows[rows[i]].RatioStrippedOverDoc > rep.PageDiff.Rows[rows[j]].RatioStrippedOverDoc
	})
	b.WriteString("## Per-Page Token Diff (top 10 by stripped/DocuMcp ratio)\n\n")
	b.WriteString("| URL | tokens_raw | tokens_stripped | tokens_documcp | stripped/doc | raw/doc |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	limit := 10
	if len(rows) < limit {
		limit = len(rows)
	}
	for _, idx := range rows[:limit] {
		r := rep.PageDiff.Rows[idx]
		fmt.Fprintf(b, "| `%s` | %d | %d | %d | %.1f× | %.1f× |\n", r.URL, r.TokensRaw, r.TokensStripped, r.TokensDocuMcp, r.RatioStrippedOverDoc, r.RatioRawOverDoc)
	}
	b.WriteByte('\n')
}

func writeRates(b *strings.Builder, trials []tasks.TrialResult) {
	a := ratesForConfig(trials, "A")
	bb := ratesForConfig(trials, "B")
	b.WriteString("## Correctness & Aborts\n\n")
	b.WriteString("| Config | Correct rate | Mean tool calls | Abort rate |\n")
	b.WriteString("|---|---|---|---|\n")
	fmt.Fprintf(b, "| A | %.0f%% | %.1f | %.0f%% |\n", 100*a.correct, a.meanTools, 100*a.aborts)
	fmt.Fprintf(b, "| B | %.0f%% | %.1f | %.0f%% |\n\n", 100*bb.correct, bb.meanTools, 100*bb.aborts)
}

func writeJudgeCost(b *strings.Builder, j tasks.JudgeAccounting) {
	b.WriteString("## Judge Token Cost\n\n")
	fmt.Fprintf(b, "Input: %d — Output: %d (excluded from per-config totals.)\n\n", j.InputTokens, j.OutputTokens)
}

func writeSkippedLog(b *strings.Builder, rep Report) {
	if rep.PageDiff != nil && len(rep.PageDiff.Errors) > 0 {
		b.WriteString("## Page-Diff Skipped URLs\n\n")
		for _, e := range rep.PageDiff.Errors {
			fmt.Fprintf(b, "- %s\n", e)
		}
		b.WriteByte('\n')
	}
	aborted := 0
	for _, t := range rep.Trials {
		if t.Aborted {
			aborted++
		}
	}
	if aborted > 0 {
		fmt.Fprintf(b, "## Aborted Trials\n\n%d trials hit a hard limit (excluded from headline mean).\n\n", aborted)
	}
}

type configTotals struct {
	n        int
	mean     float64
	lo, hi   float64
}

func totalsForConfig(trials []tasks.TrialResult, cfg string) configTotals {
	var xs []float64
	for _, t := range trials {
		if t.Config == cfg && t.Correct && !t.Aborted {
			xs = append(xs, float64(t.TotalTokens()))
		}
	}
	mean, lo, hi := BootstrapCI95(xs, 10000, 1)
	return configTotals{n: len(xs), mean: mean, lo: lo, hi: hi}
}

type configRates struct {
	correct, aborts, meanTools float64
}

func ratesForConfig(trials []tasks.TrialResult, cfg string) configRates {
	var (
		total, ok, aborts, tools int
	)
	for _, t := range trials {
		if t.Config != cfg {
			continue
		}
		total++
		if t.Correct {
			ok++
		}
		if t.Aborted {
			aborts++
		}
		tools += t.ToolCalls
	}
	if total == 0 {
		return configRates{}
	}
	return configRates{
		correct:   float64(ok) / float64(total),
		aborts:    float64(aborts) / float64(total),
		meanTools: float64(tools) / float64(total),
	}
}
