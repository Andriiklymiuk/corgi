package cmd

import (
	"context"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// pickupTimeout keeps a slow tracker from holding up the thing someone
// actually asked for, which is the session.
const pickupTimeout = 20 * time.Second

// markPickedUp moves a ticket to the workspace's pickup column — "In
// Progress" — when someone starts working on it. Best effort by design: the
// session is the point, and a tracker that refuses the move must not stop it.
// Only issues move; a pull request review has no column.
func markPickedUp(agentD string, events []watch.Event) {
	if len(events) == 0 {
		return
	}
	workspaceID := events[0].Workspace
	if workspaceID == "" {
		return
	}
	status := pickupStatusFor(agentD, workspaceID)
	if status == "" {
		return
	}
	w, _, err := watchWriter(agentD, workspaceID)
	if err != nil {
		utils.Infof("corgi: not moving the ticket: %v\n", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), pickupTimeout)
	defer cancel()
	for _, e := range events {
		if e.Kind != watch.KindIssueNew && e.Kind != watch.KindIssueComment {
			continue
		}
		ref := strings.TrimSpace(e.Ref)
		if ref == "" || strings.EqualFold(strings.TrimSpace(e.State), status) {
			continue // already there: a needless write is still a write
		}
		if err := w.Move(ctx, ref, status); err != nil {
			utils.Infof("corgi: %s stayed where it was: %v\n", ref, err)
			continue
		}
		// Where it came from, so the move can be undone.
		_ = watch.LoadStateLog(agentD).SetFrom(e.Key, status, e.State, time.Now())
		utils.Infof("corgi: %s → %s\n", ref, status)
	}
}

// pickupStatusFor is the column this workspace moves a picked-up ticket to,
// or "" when it was never configured to move one.
func pickupStatusFor(agentD, workspaceID string) string {
	resolved, err := resolveWorkspaceConfig(agentD, workspaceID)
	if err != nil || resolved.Watch == nil {
		return ""
	}
	return strings.TrimSpace(resolved.Watch.PickupStatus)
}

// claimTicket takes a ticket on the tracker for this machine, so a second
// machine watching the same board leaves it alone. Wired from the daemon,
// which cannot write to a tracker itself.
func claimTicket(agentD, workspaceID string, e watch.Event) (bool, string, error) {
	ref := strings.TrimSpace(e.Ref)
	if ref == "" {
		return true, "", nil // nothing to claim on
	}
	w, _, err := watchWriter(agentD, workspaceID)
	if err != nil {
		return false, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pickupTimeout)
	defer cancel()
	return watch.Claim(ctx, w, ref, watch.MachineName(), time.Now())
}

// markDelivered moves a ticket on once a run opened a pull request for it.
// A different column from the pickup one: the work is finished and waiting
// on a person, which is not the same as being worked on.
func markDelivered(agentD, workspaceID string, e watch.Event, prs []string) {
	if len(prs) == 0 || strings.TrimSpace(e.Ref) == "" {
		return
	}
	resolved, err := resolveWorkspaceConfig(agentD, workspaceID)
	if err != nil || resolved.Watch == nil {
		return
	}
	status := strings.TrimSpace(resolved.Watch.ReviewStatus)
	if status == "" || strings.EqualFold(status, e.State) {
		return
	}
	w, _, err := watchWriter(agentD, workspaceID)
	if err != nil {
		utils.Infof("corgi: %s stayed where it was: %v\n", e.Ref, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), pickupTimeout)
	defer cancel()
	if err := w.Move(ctx, e.Ref, status); err != nil {
		utils.Infof("corgi: %s stayed where it was: %v\n", e.Ref, err)
		return
	}
	_ = watch.LoadStateLog(agentD).Set(e.Key, status, time.Now())
	// Say what it opened, on the ticket, so the board is not the only place
	// the link exists — in the one corgi comment, not a new one each time.
	_ = watch.UpsertWorkpad(ctx, w, e.Ref, "Pull requests", strings.Join(prs, "\n"))
	utils.Infof("corgi: %s → %s\n", e.Ref, status)
}

// writeWorkpad sets one section of a ticket's workpad comment from the
// daemon, for the runner to leave the handoff or a blocker on the ticket.
func writeWorkpad(agentD, workspaceID, ref, section, text string) {
	if strings.TrimSpace(ref) == "" {
		return
	}
	w, _, err := watchWriter(agentD, workspaceID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), pickupTimeout)
	defer cancel()
	if err := watch.UpsertWorkpad(ctx, w, ref, section, text); err != nil {
		utils.Infof("corgi: workpad on %s: %v\n", ref, err)
	}
}
