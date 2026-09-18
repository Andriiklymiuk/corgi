package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var protectedFolders = []string{"Desktop", "Documents", "Downloads", filepath.Join("Library", "Mobile Documents")}

func protectedHomeFolder(path string) string {
	if runtime.GOOS != "darwin" || path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	for _, folder := range protectedFolders {
		root := filepath.Join(home, folder)
		if abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			if folder == filepath.Join("Library", "Mobile Documents") {
				return "iCloud Drive"
			}
			return folder
		}
	}
	return ""
}

var codesignTeamProbe = func(binary string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "codesign", "-dv", binary).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func corgiIsAdhocSigned() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	binary, err := os.Executable()
	if err != nil {
		return false
	}
	report := codesignTeamProbe(binary)
	if report == "" {
		return false
	}
	return strings.Contains(report, "TeamIdentifier=not set") || strings.Contains(report, "Signature=adhoc")
}

func protectedWorkspaceNote(path string) string {
	folder := protectedHomeFolder(path)
	if folder == "" {
		return ""
	}
	note := "macOS guards ~/" + folder + ", so it will ask to let corgi read it"
	if corgiIsAdhocSigned() {
		note += " — and asks again after every corgi upgrade, because corgi is not Developer-ID signed"
	}
	return note + ".\n  Keep workspaces outside Desktop/Documents/Downloads/iCloud Drive and macOS never asks."
}

func checkMacOSFileAccess() (agentCheck, bool) {
	if runtime.GOOS != "darwin" {
		return agentCheck{}, false
	}
	registry, _, err := agentRegistry()
	if err != nil {
		return agentCheck{}, false
	}
	byFolder := map[string][]string{}
	var order []string
	for _, ws := range registry.Sorted() {
		folder := protectedHomeFolder(ws.AbsPath)
		if folder == "" {
			continue
		}
		if _, seen := byFolder[folder]; !seen {
			order = append(order, folder)
		}
		byFolder[folder] = append(byFolder[folder], ws.ID)
	}
	if len(order) == 0 {
		return agentCheck{}, false
	}
	var gated []string
	for _, folder := range order {
		gated = append(gated, "~/"+folder+" ("+strings.Join(byFolder[folder], ", ")+")")
	}
	c := agentCheck{
		Name:   "macOS file access",
		OK:     true,
		Detail: "macOS asks before corgi reads " + strings.Join(gated, "; "),
		Fix:    "move those workspaces outside Desktop/Documents/Downloads/iCloud Drive and it stops asking",
	}
	if corgiIsAdhocSigned() {
		c.Detail += " — and asks again after every corgi upgrade (corgi is not Developer-ID signed, so each build is a new identity to macOS)"
	} else {
		c.Detail += " — once: this corgi is Developer-ID signed, so the answer survives upgrades"
	}
	return c, true
}
