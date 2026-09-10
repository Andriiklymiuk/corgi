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
