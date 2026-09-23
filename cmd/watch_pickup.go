package cmd

import (
	"context"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

const pickupTimeout = 20 * time.Second

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
			continue
		}
		current := e.State
		if known, ok := watch.LoadStateLog(agentD).Get(e.Key); ok && known.Status != "" {
			current = known.Status
		}
		if over := watch.FinishedState(current); over != "" {
			utils.Infof("corgi: %s is %s - not moving it to %s\n", ref, over, status)
			continue
		}
		if err := w.Move(ctx, ref, status); err != nil {
			utils.Infof("corgi: %s stayed where it was: %v\n", ref, err)
			continue
		}
		_ = watch.LoadStateLog(agentD).SetFrom(e.Key, status, e.State, time.Now())
		utils.Infof("corgi: %s → %s\n", ref, status)
	}
}

func pickupStatusFor(agentD, workspaceID string) string {
	resolved, err := resolveWorkspaceConfig(agentD, workspaceID)
	if err != nil || resolved.Watch == nil {
		return ""
	}
	return strings.TrimSpace(resolved.Watch.PickupStatus)
}

func claimTicket(agentD, workspaceID string, e watch.Event) (bool, string, error) {
	ref := strings.TrimSpace(e.Ref)
	if ref == "" {
		return true, "", nil
	}
	w, _, err := watchWriter(agentD, workspaceID)
	if err != nil {
		return false, "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), pickupTimeout)
	defer cancel()
	return watch.Claim(ctx, w, ref, watch.MachineName(), time.Now())
}

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
	_ = watch.UpsertWorkpad(ctx, w, e.Ref, "Pull requests", strings.Join(prs, "\n"))
	utils.Infof("corgi: %s → %s\n", e.Ref, status)
}

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
