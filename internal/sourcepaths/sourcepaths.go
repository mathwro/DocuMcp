package sourcepaths

import "strings"

// Normalize combines the legacy single path with the multi-path list.
// It trims blanks, drops empty entries, and deduplicates in first-seen order.
func Normalize(legacy string, paths []string) []string {
	out := make([]string, 0, len(paths)+1)
	seen := make(map[string]struct{}, len(paths)+1)
	for _, p := range append([]string{legacy}, paths...) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func First(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}
