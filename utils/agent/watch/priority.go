package watch

import "strings"

// Priority is how urgent a ticket says it is, from its labels: the words
// trackers and teams actually use. 0 is urgent, 1 high, 2 everything else.
// Used to order the inbox and to pick which deferred fix runs first.
func Priority(e Event) int {
	for _, l := range e.Labels {
		switch strings.ToLower(strings.TrimSpace(l)) {
		case "urgent", "p0", "p1", "critical", "blocker", "highest", "sev1", "sev-1", "priority: urgent", "priority: highest":
			return 0
		case "high", "p2", "important", "priority: high", "sev2", "sev-2":
			return 1
		}
	}
	return 2
}

// KindRank orders kinds by who is waiting: a review someone asked for
// outranks a red build outranks a reply on your own PR outranks a fresh
// ticket, which blocks nobody yet.
func KindRank(kind Kind) int {
	switch kind {
	case KindReviewRequested:
		return 0
	case KindCIFailed:
		return 1
	case KindPRReview, KindPRComment:
		return 2
	case KindIssueComment:
		return 3
	default:
		return 4
	}
}

// Less orders two events for a queue: priority, then kind, then age.
func Less(a, b Event) bool {
	if pa, pb := Priority(a), Priority(b); pa != pb {
		return pa < pb
	}
	if ra, rb := KindRank(a.Kind), KindRank(b.Kind); ra != rb {
		return ra < rb
	}
	return a.At.Before(b.At)
}
