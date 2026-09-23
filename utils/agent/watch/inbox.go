package watch

import "strings"

// InboxDone says why an inbox row no longer waits on anyone, or "" while it
// does. Every surface (the phone, the menu bar, the editor, watch status)
// asks this one question, so a row leaves all of them at once:
//   - the ticket moved to a finished state, or was picked up (Settled)
//   - every pull request the row is about is merged or closed
//   - I reviewed every pull request a review request is about, after it came
//   - a run already handled it: the review is posted, the comment answered
func InboxDone(e Event, current string, pulls *PullLog, fixes *FixLog) string {
	if why := Settled(e, current); why != "" {
		return why
	}
	if why := pullsFinished(e, pulls); why != "" {
		return why
	}
	if reviewedByMe(e, pulls) {
		return "reviewed"
	}
	if fixes != nil && handledByRun(e, fixes) {
		return "handled"
	}
	return ""
}

// PullRefsOf is every pull request a row points at: its ref when that is one,
// its URL, and the links a chat message carried.
func PullRefsOf(e Event) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ref string) {
		if ref != "" && !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	if strings.ContainsAny(e.Ref, "#!") && !strings.HasPrefix(e.Ref, "slack") {
		add(e.Ref)
	}
	add(PullRef(e.URL))
	for _, l := range e.Links {
		add(PullRef(l))
	}
	return out
}

func pullsFinished(e Event, pulls *PullLog) string {
	switch e.Kind {
	case KindReviewRequested, KindPRComment, KindPRReview, KindCIFailed:
	default:
		return ""
	}
	refs := PullRefsOf(e)
	if len(refs) == 0 || pulls == nil {
		return ""
	}
	word := ""
	for _, ref := range refs {
		st, ok := pulls.Get(ref)
		if !ok || (st.State != "merged" && st.State != "closed") {
			return ""
		}
		if word == "" || st.State == "merged" {
			word = st.State
		}
	}
	return word
}

// handledByRun: a run for this row finished without failing. A run for the
// same pull request that started after the row came in counts too — it read
// the thread, this comment with it.
func handledByRun(e Event, fixes *FixLog) bool {
	switch e.Kind {
	case KindReviewRequested, KindPRComment, KindPRReview, KindCIFailed, KindIssueComment:
	default:
		return false
	}
	fixes.mu.Lock()
	defer fixes.mu.Unlock()
	for _, r := range fixes.Started {
		if !r.Done() || r.Error != "" || r.Failure != "" || r.Workspace != e.Workspace {
			continue
		}
		if r.Key == e.Key {
			return true
		}
		if e.Kind != KindIssueComment && e.Ref != "" && r.Ref == e.Ref && !r.StartedAt.Before(e.At) {
			return true
		}
	}
	return false
}

func reviewedByMe(e Event, pulls *PullLog) bool {
	if e.Kind != KindReviewRequested || pulls == nil {
		return false
	}
	refs := PullRefsOf(e)
	if len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		st, ok := pulls.Get(ref)
		if !ok || st.MyReviewAt.IsZero() || st.MyReviewAt.Before(e.At) {
			return false
		}
	}
	return true
}
