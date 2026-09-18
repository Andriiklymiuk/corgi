package sessions

import (
	"strconv"
	"strings"
)

type Standing struct {
	Word string `json:"word"`
	Why  string `json:"why,omitempty"`
}

func (s Standing) Line() string {
	if s.Why == "" {
		return s.Word
	}
	if s.Word == "" {
		return s.Why
	}
	return s.Word + " · " + s.Why
}

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

type PullFacts struct {
	State  string
	Checks string
	Review string
}

func PullReady(p PullFacts) bool {
	return p.State == "open" && (p.Checks == "passing" || p.Checks == "none" || p.Checks == "") && p.Review == "approved"
}

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

type Facts struct {
	Status  Status
	Pending *Pending
	Stuck   bool
	Limit   LimitKind
	Gate    *GateRun
	Tests   *TestRun
	Behind  *Behind
	Pull    *PullFacts
	PR      string
	Blocked string
	Handoff string
}

func (s Session) Facts(pull *PullFacts) Facts {
	return Facts{Status: s.Status, Pending: s.Pending, Stuck: s.Stuck, Limit: s.Limit,
		Gate: s.Gate, Tests: s.Tests, Behind: s.Behind, Pull: pull, PR: s.PR}
}

const gateStrikes = 3

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
