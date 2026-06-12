package cmd

import (
	"fmt"
	"strings"
	"time"

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
			who = fmt.Sprintf("an unidentified process (try: sudo lsof -nP -i:%d)", p.Port)
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
			if !utils.IsPortListening(p.Port) {
				continue
			}
			if err := utils.KillPortOwner(p.Port); err != nil {
				utils.Infof("⚠️  could not free port %d: %v\n", p.Port, err)
				continue
			}
			// wait for the socket to release before the re-check below.
			if !utils.WaitPortFree(p.Port, 3*time.Second) {
				utils.Infof("⚠️  port %d still busy after kill\n", p.Port)
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
