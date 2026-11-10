package utils

import "strings"

// Nearest returns the candidate closest to name by edit distance, ignoring case
// and the - _ separators, or "" when none is close enough to be a plausible typo.
func Nearest(name string, candidates []string) string {
	key := foldName(name)
	if key == "" {
		return ""
	}
	limit := 2
	if len(key) > 8 {
		limit = 3
	}
	best, bestD := "", limit+1
	for _, c := range candidates {
		ck := foldName(c)
		if ck == key {
			return c
		}
		d := editDistance(key, ck)
		if len(key) >= 4 && len(ck) >= 4 && (strings.HasPrefix(ck, key) || strings.HasPrefix(key, ck)) {
			d = min(d, limit)
		}
		if d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

func foldName(s string) string {
	s = strings.ToLower(strings.TrimLeft(strings.TrimSpace(s), "-"))
	return strings.NewReplacer("_", "", "-", "").Replace(s)
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
