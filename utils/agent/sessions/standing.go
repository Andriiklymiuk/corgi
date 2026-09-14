package sessions

import (
	"strconv"
	"strings"
)

// Standing is where a thing stands — a session, a kanban card, an inbox
// row — in one word a surface prints as is, and the clause behind it.
// Worked out once, here, from the facts the daemon holds, so the phone,
// the menu bar, the editor and the deck never disagree about what "ready
// to merge" means.
type Standing struct {
	Word string `json:"word"`
	Why  string `json:"why,omitempty"`
}

// Line is the word and the clause on one row: "ready to merge · checks ✓ ·
// approved", "working".
func (s Standing) Line() string {
	if s.Why == "" {
		return s.Word
	}
	if s.Word == "" {
		return s.Why
	}
	return s.Word + " · " + s.Why
}

// The words, top rung first. A higher rung wins: merged is merged whatever
// the checks say, a permission prompt waits whatever the pull request says,
// and a pull request that exists is what the row is about, not the session
// behind it.
const (
	StandMerged    = "merged"
	StandClosed    = "closed"
	StandBlocked   = "blocked"
	StandNeedsYou  = "needs you"
	StandLimited   = "at a limit"
	StandChecksRed = "checks failing"
	StandChanges   = "changes requested"
	StandConflicts = "conflicts"
	StandTestsRed  = "tests failing"
	StandReady     = "ready to merge"
	StandApproved  = "approved"
	StandDraft     = "draft"
	StandReview    = "in review"
	StandPROpen    = "pull request open"
	StandStuck     = "stuck"
	StandWorking   = "working"
	StandDone      = "done"
	StandIdle      = "idle"
	StandGone      = "gone"
	StandHandoff   = "handoff waits"
	StandNew       = "new"
)

// PullFacts is what the forge said about a pull request, as words: State
// open / draft / merged / closed, Checks passing / failing / pending /
// none, Review approved / changes / pending / none.
type PullFacts struct {
	State  string
	Checks string
	Review string
}

// PullReady is "nothing stands between this and Merge": open, checks green
// or absent, approved.
func PullReady(p PullFacts) bool {
	return p.State == "open" && (p.Checks == "passing" || p.Checks == "none" || p.Checks == "") && p.Review == "approved"
}

// PullLine is the pull request's standing in a few words for a row:
// "ready to merge · checks ✓ · approved", "checks ✗ · changes requested".
func PullLine(p PullFacts) string {
	parts := pullParts(p)
	if PullReady(p) {
		return "ready to merge · " + parts
	}
	return parts
}

func pullParts(p PullFacts) string {
	var parts []string
	switch p.Checks {
	case "passing":
		parts = append(parts, "checks ✓")
	case "failing":
		parts = append(parts, "checks ✗")
	case "pending":
		parts = append(parts, "checks running")
	}
	switch p.Review {
	case "approved":
		parts = append(parts, "approved")
	case "changes":
		parts = append(parts, "changes requested")
	case "pending":
		parts = append(parts, "review pending")
	}
	return strings.Join(parts, " · ")
}

// Facts is everything the ladder looks at. Any field may be empty; the
// ladder takes the highest rung the facts reach.
type Facts struct {
	Status  Status
	Pending *Pending
	Stuck   bool
	Limit   LimitKind
	Gate    *GateRun
	Tests   *TestRun
	Behind  *Behind
	// Pull is the forge's word on the pull request; PR the link alone,
	// before the forge has been asked.
	Pull *PullFacts
	PR   string
	// Blocked is the wall an unattended run hit, from the fix log.
	Blocked string
	// Handoff is a packet waiting for a session to pick it up.
	Handoff string
}

// Facts gathers a session's own facts; pull is the forge's word on its
// pull request, when the daemon has one.
func (s Session) Facts(pull *PullFacts) Facts {
	return Facts{Status: s.Status, Pending: s.Pending, Stuck: s.Stuck, Limit: s.Limit,
		Gate: s.Gate, Tests: s.Tests, Behind: s.Behind, Pull: pull, PR: s.PR}
}

// gateStrikes is how many red gate runs in a row mean the session cannot
// get there on its own — the same count the daemon rings at.
const gateStrikes = 3

// StandingOf walks the ladder top down and stops at the first rung the
// facts reach.
func StandingOf(f Facts) Standing {
	if f.Pull != nil {
		switch f.Pull.State {
		case "merged":
			return Standing{Word: StandMerged}
		case "closed":
			return Standing{Word: StandClosed}
		}
	}
	if f.Blocked != "" {
		return Standing{Word: StandBlocked, Why: f.Blocked}
	}
	if f.Pending != nil {
		why := "allow " + strings.TrimSpace(f.Pending.Tool+" "+f.Pending.Subject) + "?"
		return Standing{Word: StandNeedsYou, Why: why}
	}
	if f.Status == StatusNeedsInput {
		return Standing{Word: StandNeedsYou, Why: "waiting for a word"}
	}
	if f.Gate != nil && !f.Gate.OK && f.Gate.Fails >= gateStrikes {
		return Standing{Word: StandNeedsYou, Why: "gate red " + strconv.Itoa(f.Gate.Fails) + "× · " + f.Gate.Cmd}
	}
	if f.Status == StatusLimited {
		return Standing{Word: StandLimited, Why: string(f.Limit)}
	}
	if f.Pull != nil {
		if f.Pull.Checks == "failing" {
			return Standing{Word: StandChecksRed, Why: pullParts(*f.Pull)}
		}
		if f.Pull.Review == "changes" {
			return Standing{Word: StandChanges, Why: pullParts(*f.Pull)}
		}
	}
	if f.Behind != nil && len(f.Behind.Conflicts) > 0 {
		return Standing{Word: StandConflicts, Why: BehindLine(f.Behind)}
	}
	if f.Gate != nil && !f.Gate.OK {
		return Standing{Word: StandTestsRed, Why: strings.TrimSpace("gate ✗ " + f.Gate.Cmd)}
	}
	if f.Tests != nil && !f.Tests.OK {
		return Standing{Word: StandTestsRed, Why: TestsLine(f.Tests)}
	}
	if f.Pull != nil {
		switch {
		case PullReady(*f.Pull):
			return Standing{Word: StandReady, Why: pullParts(*f.Pull)}
		case f.Pull.Review == "approved":
			return Standing{Word: StandApproved, Why: pullParts(*f.Pull)}
		case f.Pull.State == "draft":
			return Standing{Word: StandDraft, Why: pullParts(*f.Pull)}
		case f.Pull.State == "open":
			return Standing{Word: StandReview, Why: pullParts(*f.Pull)}
		}
	}
	if f.PR != "" {
		return Standing{Word: StandPROpen}
	}
	if f.Stuck {
		return Standing{Word: StandStuck, Why: "working, silent " + strconv.Itoa(int(StuckAfter.Minutes())) + " min"}
	}
	switch f.Status {
	case StatusWorking:
		return Standing{Word: StandWorking}
	case StatusDone:
		return Standing{Word: StandDone}
	case StatusStale:
		return Standing{Word: StandIdle}
	case StatusGone:
		return Standing{Word: StandGone}
	}
	if f.Handoff != "" {
		return Standing{Word: StandHandoff, Why: f.Handoff}
	}
	return Standing{Word: StandNew}
}
