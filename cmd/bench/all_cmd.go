// cmd/bench/all_cmd.go
package main

import "flag"

func runAll(args []string) {
	fs := flag.NewFlagSet("all", flag.ExitOnError)
	urlsPath := fs.String("urls", "internal/bench/corpus/page-urls.txt", "path to URL list")
	corpusPath := fs.String("questions", "internal/bench/corpus/questions.json", "path to questions.json")
	trials := fs.Int("trials", 3, "trials per (question, config)")
	_ = fs.Parse(args)

	dir := newOutputDir()
	runPageDiffInto(dir, pageDiffOpts{URLsPath: *urlsPath})
	runTasksInto(dir, tasksOpts{CorpusPath: *corpusPath, Trials: *trials})
}
