// cmd/bench/all_cmd.go
package main

func runAll(args []string) {
	dir := newOutputDir()
	runPageDiffInto(dir, args)
	runTasksInto(dir, args)
}
