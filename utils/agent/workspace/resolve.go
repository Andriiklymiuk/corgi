package workspace

import (
	"path/filepath"
	"sort"
	"strings"
)

type MatchKind string

const (
	MatchID      MatchKind = "id"
	MatchAlias   MatchKind = "alias"
	MatchDir     MatchKind = "directory"
	MatchRepo    MatchKind = "repo"
	MatchService MatchKind = "service"
)

type Candidate struct {
	Workspace Workspace `json:"workspace"`
	Kind      MatchKind `json:"matchedOn"`
	Matched   string    `json:"matchedValue"`
}

type Resolution struct {
	Workspace  *Workspace  `json:"workspace,omitempty"`
	MatchedOn  MatchKind   `json:"matchedOn,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Reason     string      `json:"reason"`
}

func (r Resolution) Resolved() bool { return r.Workspace != nil }

func Resolve(r *Registry, query string) Resolution {
	normalizedQuery := normalize(query)
	if normalizedQuery == "" {
		return Resolution{
			Candidates: allCandidates(r),
			Reason:     "no workspace named — say which one",
		}
	}

	if exact := exactMatches(r, normalizedQuery); len(exact) == 1 {
		w := exact[0].Workspace
		return Resolution{
			Workspace: &w,
			MatchedOn: exact[0].Kind,
			Reason:    describe(w, exact[0]),
		}
	} else if len(exact) > 1 {
		return Resolution{
			Candidates: exact,
			Reason:     "more than one workspace uses that name — pick one",
		}
	}

	fuzzy := fuzzyMatches(r, normalizedQuery)
	switch len(fuzzy) {
	case 0:
		return Resolution{
			Candidates: allCandidates(r),
			Reason:     "no workspace matched " + strings.TrimSpace(query),
		}
	case 1:
		w := fuzzy[0].Workspace
		return Resolution{
			Workspace: &w,
			MatchedOn: fuzzy[0].Kind,
			Reason:    describe(w, fuzzy[0]),
		}
	default:
		return Resolution{
			Candidates: fuzzy,
			Reason:     strings.TrimSpace(query) + " matched several workspaces — pick one",
		}
	}
}

func describe(w Workspace, c Candidate) string {
	parts := []string{w.ID + " (" + w.AbsPath + ")"}
	if len(w.Services) > 0 {
		parts = append(parts, strings.Join(w.Services, " + "))
	}
	if c.Kind != MatchID {
		parts = append(parts, "matched on "+string(c.Kind)+" "+c.Matched)
	}
	return strings.Join(parts, ", ")
}

func exactMatches(r *Registry, query string) []Candidate {
	var out []Candidate
	for _, w := range r.Workspaces {
		if normalize(w.ID) == query {
			out = append(out, Candidate{Workspace: w, Kind: MatchID, Matched: w.ID})
			continue
		}
		for _, alias := range w.Aliases {
			if normalize(alias) == query {
				out = append(out, Candidate{Workspace: w, Kind: MatchAlias, Matched: alias})
				break
			}
		}
	}
	return out
}

func fuzzyMatches(r *Registry, query string) []Candidate {
	var out []Candidate
	for _, w := range r.Workspaces {
		if c, ok := bestFuzzyMatch(w, query); ok {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Workspace.ID < out[j].Workspace.ID })
	return out
}

func bestFuzzyMatch(w Workspace, query string) (Candidate, bool) {
	if related(normalize(w.ID), query) {
		return Candidate{Workspace: w, Kind: MatchID, Matched: w.ID}, true
	}
	for _, alias := range w.Aliases {
		if related(normalize(alias), query) {
			return Candidate{Workspace: w, Kind: MatchAlias, Matched: alias}, true
		}
	}
	if base := filepath.Base(w.AbsPath); base != "" && base != "." && base != string(filepath.Separator) {
		if related(normalize(base), query) {
			return Candidate{Workspace: w, Kind: MatchDir, Matched: base}, true
		}
	}
	for _, repo := range w.Repos {
		if related(normalize(repo), query) {
			return Candidate{Workspace: w, Kind: MatchRepo, Matched: repo}, true
		}
	}
	for _, svc := range w.Services {
		if related(normalize(svc), query) {
			return Candidate{Workspace: w, Kind: MatchService, Matched: svc}, true
		}
	}
	return Candidate{}, false
}

func related(value, query string) bool {
	if value == "" || query == "" {
		return false
	}
	valueWords := strings.Fields(value)
	queryWords := strings.Fields(query)

	if containsAllWords(valueWords, queryWords) || containsAllWords(queryWords, valueWords) {
		return true
	}
	for _, v := range valueWords {
		if len(v) < 3 {
			continue
		}
		for _, q := range queryWords {
			if v == q {
				return true
			}
		}
	}
	return false
}

func containsAllWords(have, want []string) bool {
	if len(want) == 0 {
		return false
	}
	set := make(map[string]bool, len(have))
	for _, w := range have {
		set[w] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func allCandidates(r *Registry) []Candidate {
	out := make([]Candidate, 0, len(r.Workspaces))
	for _, w := range r.Sorted() {
		out = append(out, Candidate{Workspace: w, Kind: MatchID, Matched: w.ID})
	}
	return out
}

func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
