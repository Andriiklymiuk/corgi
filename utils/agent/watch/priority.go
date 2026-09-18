package watch

import "strings"

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

func Less(a, b Event) bool {
	if pa, pb := Priority(a), Priority(b); pa != pb {
		return pa < pb
	}
	if ra, rb := KindRank(a.Kind), KindRank(b.Kind); ra != rb {
		return ra < rb
	}
	return a.At.Before(b.At)
}
