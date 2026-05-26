package cmd

import (
	"fmt"
	"strings"

	"andriiklymiuk/corgi/utils"
)

// Conflict message per declared port already in use. isBusy/owner injected for tests.
func checkPortConflicts(ports []portOwnerInfo, isBusy func(int) bool, owner func(int) string) []string {
	var conflicts []string
	for _, p := range ports {
		if !isBusy(p.Port) {
			continue
		}
		who := owner(p.Port)
		if who == "" {
			who = "unknown (lsof unavailable)"
		}
		conflicts = append(conflicts, fmt.Sprintf("port %d busy (%s) — held by %s", p.Port, p.Desc, who))
	}
	return conflicts
}

// Service ports only (not db_services: corgi reuses already-running db
// containers, so their ports being held is expected, not a conflict).
func collectServicePorts(corgi *utils.CorgiCompose) []portOwnerInfo {
	var ports []portOwnerInfo
	for _, svc := range corgi.Services {
		if svc.Port == 0 || svc.ManualRun {
			continue
		}
		ports = append(ports, portOwnerInfo{
			Port: svc.Port,
			Desc: fmt.Sprintf("services.%s", svc.ServiceName),
		})
	}
	return ports
}

// portPreflight aborts the run if a declared service port is taken. With
// killPort it first frees occupied ports, then re-checks.
func portPreflight(corgi *utils.CorgiCompose, killPort bool) error {
	ports := collectServicePorts(corgi)
	if killPort {
		for _, p := range ports {
			if utils.IsPortListening(p.Port) {
				if err := utils.KillPortOwner(p.Port); err != nil {
					utils.Infof("⚠️  could not free port %d: %v\n", p.Port, err)
				}
			}
		}
	}
	conflicts := checkPortConflicts(ports, utils.IsPortListening, utils.PortOwner)
	if len(conflicts) == 0 {
		return nil
	}
	hint := " (use --kill-port to reclaim)"
	if killPort {
		hint = ""
	}
	return fmt.Errorf("port conflict%s:\n  %s", hint, strings.Join(conflicts, "\n  "))
}
