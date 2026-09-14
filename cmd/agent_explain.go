package cmd

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

// Explain this diff: the chief reads a session's branch — the files with
// their counts and the patch, bounded — and says in three plain lines what
// it does, then one word for the risk. For a phone on the bus: enough to
// merge or to ask, without reading four hundred lines. The same gate as
// the diff itself, and the same cheap model as the chief.

const explainSoul = `You explain a code change to its author, who is reading on a phone. You are given the ticket, the branch, the files with their line counts, and the patch. Answer in plain text only — no markdown, no asterisks, no headings, no code fences. Exactly this shape: at most three short lines saying what the change does and what it touches (name files as given), then one last line "risk: low|medium|high — <why in a clause>". Low is a comment, a rename, a test, a small local change; medium is logic or an interface others use; high is data, auth, deletion, migrations, money, or anything that runs against production. Say what the patch shows, never what it might have meant to do.`

// explainPatchMax bounds what the chief reads: past this the patch is cut
// and the prompt says so, so the answer says it read the first part.
const explainPatchMax = 60 << 10

// explainSession is what the chief says about the session's diff.
func explainSession(ctx context.Context, s sessions.Session, question string) (answer, risk string, files int, err error) {
	dir := diffDirFor(s)
	if dir == "" {
		return "", "", 0, fmt.Errorf("this session has no checkout to diff")
	}
	gitCtx, cancel := context.WithTimeout(ctx, diffTimeout)
	defer cancel()
	base := diffBase(gitCtx, dir)
	if base == "" {
		return "", "", 0, fmt.Errorf("no main branch to diff against")
	}
	list, err := diffFiles(gitCtx, dir, base)
	if err != nil {
		return "", "", 0, fmt.Errorf("git could not diff the branch")
	}
	if len(list) == 0 {
		return "", "", 0, fmt.Errorf("nothing changed on this branch yet")
	}
	patch, truncated, err := diffPatch(gitCtx, dir, base, "")
	if err != nil {
		return "", "", 0, fmt.Errorf("git could not diff the branch")
	}
	if len(patch) > explainPatchMax {
		patch, truncated = patch[:explainPatchMax], true
	}
	var b strings.Builder
	if s.Ticket != "" {
		b.WriteString("TICKET: " + s.Ticket + "\n")
	}
	b.WriteString("BRANCH: " + s.Branch + "\n")
	b.WriteString("FILES:\n")
	for _, f := range list {
		line := f.Path + " +" + strconv.Itoa(f.Added) + " -" + strconv.Itoa(f.Deleted)
		if f.Generated {
			line += " (generated)"
		}
		if f.Binary {
			line += " (binary)"
		}
		b.WriteString("  " + line + "\n")
	}
	if truncated {
		b.WriteString("PATCH (the first part; the rest was cut for size):\n")
	} else {
		b.WriteString("PATCH:\n")
	}
	b.WriteString(patch)
	if q := strings.TrimSpace(question); q != "" {
		if len(q) > 500 {
			q = q[:500]
		}
		b.WriteString("\n\nQUESTION: " + q)
	}
	askCtx, cancelAsk := context.WithTimeout(ctx, askTimeout)
	defer cancelAsk()
	answer, err = runClaudePrint(askCtx, askModel, explainSoul, b.String())
	if err != nil {
		return "", "", 0, err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", "", 0, fmt.Errorf("claude answered nothing")
	}
	return answer, riskWord(answer), len(list), nil
}

var riskLine = regexp.MustCompile(`(?im)^\s*risk:\s*(low|medium|high)\b`)

// riskWord is the one word off the answer's risk line, or "".
func riskWord(answer string) string {
	m := riskLine.FindAllStringSubmatch(answer, -1)
	if len(m) == 0 {
		return ""
	}
	return strings.ToLower(m[len(m)-1][1])
}
