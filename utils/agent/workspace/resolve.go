package workspace

import (
	"path/filepath"
	"sort"
	"strings"
)

// MatchKind records why a workspace matched, so the answer can explain itself
// rather than looking like magic.
type MatchKind string

const (
	MatchID      MatchKind = "id"
	MatchAlias   MatchKind = "alias"
	MatchDir     MatchKind = "directory"
	MatchRepo    MatchKind = "repo"
	MatchService MatchKind = "service"
)

// Candidate is one possible answer to a query.
type Candidate struct {
	Workspace Workspace `json:"workspace"`
	Kind      MatchKind `json:"matchedOn"`
	Matched   string    `json:"matchedValue"`
}

// Resolution is the outcome of resolving a name. Exactly one of Workspace or
// Candidates is meaningful: a resolved workspace, or the choices to offer.
type Resolution struct {
	Workspace  *Workspace  `json:"workspace,omitempty"`
	MatchedOn  MatchKind   `json:"matchedOn,omitempty"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Reason     string      `json:"reason"`
}

// Resolved reports whether the query produced exactly one workspace.
func (r Resolution) Resolved() bool { return r.Workspace != nil }

// Resolve turns a human phrase like "the recipe app" into a workspace.
//
// It never guesses. An ambiguous query returns candidates and resolves nothing,
// because a wrong resolution means an agent editing the wrong repository, and
// one extra tap is far cheaper than that.
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

// describe is what the caller echoes back before doing any work, so a
// mis-resolution is caught before code is written rather than after.
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

// bestFuzzyMatch checks the fields in decreasing order of how strongly they
// identify a workspace, so the explanation names the most meaningful hit.
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
	// Services come last: "fix the api" should find the stack that has a
	// service called api, but a service name is the weakest identifier.
	for _, svc := range w.Services {
		if related(normalize(svc), query) {
			return Candidate{Workspace: w, Kind: MatchService, Matched: svc}, true
		}
	}
	return Candidate{}, false
}

// related reports whether two normalized strings refer to the same thing.
//
// Matching is on whole words only. A raw substring test made "api" match
// "rapid-prototype", and when that was the sole hit the resolver answered with
// it confidently — exactly the wrong-repository outcome this package promises
// never to produce.
func related(value, query string) bool {
	if value == "" || query == "" {
		return false
	}
	valueWords := strings.Fields(value)
	queryWords := strings.Fields(query)

	// One phrase containing the other, word for word: "recipe app" reached by
	// "the recipe app", and vice versa.
	if containsAllWords(valueWords, queryWords) || containsAllWords(queryWords, valueWords) {
		return true
	}
	// Otherwise a shared whole word, long enough to identify something.
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

// containsAllWords reports whether every word of want appears in have.
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

// normalize lowercases and reduces punctuation to spaces so "recipe-app",
// "recipe_app", and "Recipe App" all compare equal.
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
