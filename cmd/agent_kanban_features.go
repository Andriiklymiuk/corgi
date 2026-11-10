package cmd

import (
	"fmt"
	"sort"
	"strings"
)

// kanbanFeatures groups the cards that make one thing: a plan's tasks or
// a parent ticket's subtasks. A feature is done when every card is, blocked
// when one is, running when one is; otherwise it waits or is in review.
func kanbanFeatures(cards []KanbanCard) []KanbanFeature {
	byID := map[string]*KanbanFeature{}
	var order []string
	byRef := map[string]KanbanCard{}
	for _, c := range cards {
		byRef[c.Workspace+"/"+c.Ref] = c
	}
	for _, c := range cards {
		if c.Feature == nil || c.Feature.Ref == "" {
			continue
		}
		id := c.Workspace + "/" + c.Feature.Ref
		f, ok := byID[id]
		if !ok {
			f = &KanbanFeature{Ref: c.Feature.Ref, Title: c.Feature.Title, Workspace: c.Workspace}
			if parent, has := byRef[id]; has {
				f.URL, f.Title = parent.URL, firstNonEmpty(f.Title, parent.Title)
			}
			byID[id] = f
			order = append(order, id)
		}
		f.add(c)
	}
	out := make([]KanbanFeature, 0, len(order))
	for _, id := range order {
		f := byID[id]
		f.Waves = featureWaves(f.Cards, byRef, f.Workspace)
		f.State, f.Why = featureState(*f)
		out = append(out, *f)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (f *KanbanFeature) add(c KanbanCard) {
	f.Title = firstNonEmpty(f.Title, c.Feature.Title)
	f.Cards = append(f.Cards, c.Ref)
	f.Total++
	switch c.Column {
	case ColDone:
		f.Done++
	case ColRunning:
		f.Running++
	case ColReview:
		f.Review++
	case ColBlocked:
		f.Blocked++
	}
	if c.Cost != nil {
		f.addCost(*c.Cost)
	}
	if c.UpdatedAt.After(f.UpdatedAt) {
		f.UpdatedAt = c.UpdatedAt
	}
}

func (f *KanbanFeature) addCost(c CardCost) {
	if f.Cost == nil {
		f.Cost = &CardCost{}
	}
	f.Cost.USD += c.USD
	f.Cost.Tokens += c.Tokens
	f.Cost.Runs += c.Runs
	f.Cost.Sessions += c.Sessions
	for _, m := range c.Models {
		f.Cost.Models = appendFold(f.Cost.Models, m)
	}
	for _, a := range c.Agents {
		f.Cost.Agents = appendFold(f.Cost.Agents, a)
	}
}

func cardReady(card KanbanCard, refs []string, placed map[string]bool) bool {
	for _, dep := range card.After {
		if containsFold(refs, dep) && !placed[strings.ToLower(dep)] {
			return false
		}
	}
	return true
}

func featureWaves(refs []string, byRef map[string]KanbanCard, ws string) [][]string {
	placed := map[string]bool{}
	var waves [][]string
	remaining := append([]string(nil), refs...)
	for len(remaining) > 0 {
		var wave, rest []string
		for _, ref := range remaining {
			if cardReady(byRef[ws+"/"+ref], refs, placed) {
				wave = append(wave, ref)
			} else {
				rest = append(rest, ref)
			}
		}
		if len(wave) == 0 {
			wave, rest = rest, nil
		}
		for _, ref := range wave {
			placed[strings.ToLower(ref)] = true
		}
		waves = append(waves, wave)
		remaining = rest
	}
	return waves
}

func featureState(f KanbanFeature) (string, string) {
	left := f.Total - f.Done
	switch {
	case f.Blocked > 0:
		return "blocked", plural(f.Blocked, "card is blocked", "cards are blocked")
	case f.Total > 0 && f.Done == f.Total:
		return "done", "every card is done"
	case f.Running > 1:
		return "running", fmt.Sprintf("%d cards run in parallel · %s", f.Running, plural(left, "left", "left"))
	case f.Running == 1:
		return "running", "one card runs · " + plural(left, "left", "left")
	case f.Review > 0 && f.Review == left:
		return "review", plural(f.Review, "card in review", "cards in review") + ", nothing else left"
	case f.Review > 0:
		return "review", plural(f.Review, "card in review", "cards in review") + " · " + plural(left-f.Review, "still to do", "still to do")
	}
	return "waiting", plural(left, "card to do", "cards to do") + " · " + plural(f.Done, "done", "done")
}

func featureLine(f KanbanFeature) string {
	line := f.Ref
	if f.Title != "" {
		line += "  " + clipTitle(f.Title, 48)
	}
	line += " · " + f.State + " · " + f.Why
	if len(f.Waves) > 1 {
		var waves []string
		for _, w := range f.Waves {
			waves = append(waves, strings.Join(w, " + "))
		}
		line += " · " + strings.Join(waves, " → ")
	}
	if f.Cost != nil && f.Cost.Tokens > 0 {
		line += " · " + costLine(*f.Cost)
	}
	return line
}
