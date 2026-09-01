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

// macOS gates ~/Desktop, ~/Documents, ~/Downloads and iCloud Drive behind TCC:
// the first process to read one has to ask, and the answer is remembered
// against that exact binary. corgi ships ad-hoc signed (no Developer ID), so
// every upgrade is a brand-new identity as far as TCC is concerned and the
// "corgi would like to access files in your Documents folder" prompt comes
// back — after a reboot it looks like the answer never stuck at all.
//
// corgi cannot sign its way out of that from in here. It can say why, and it
// can point at the one fix that needs nobody's certificate: a workspace outside
// those four folders is never gated in the first place.

// protectedFolders are the home-relative directories macOS puts behind TCC.
// iCloud Drive lives under Library/Mobile Documents.
var protectedFolders = []string{"Desktop", "Documents", "Downloads", filepath.Join("Library", "Mobile Documents")}

// protectedHomeFolder names the TCC-gated folder path sits in, or "" when it
// sits outside all of them (and on every non-macOS platform).
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

// codesignTeamProbe is the `codesign` read, replaced in tests.
var codesignTeamProbe = func(binary string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// codesign prints its report on stderr.
	out, err := exec.CommandContext(ctx, "codesign", "-dv", binary).CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

// corgiIsAdhocSigned reports whether this binary carries no Developer ID, which
// is what makes a TCC answer last only until the next upgrade. Unknown counts
// as signed: a wrong warning is worse than a missing one.
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

// protectedWorkspaceNote is the one-line explanation shown when a workspace is
// registered inside a TCC-gated folder. Empty when there is nothing to say.
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

// checkMacOSFileAccess reports the registered workspaces macOS gates, so the
// repeating permission dialog has an explanation somewhere other than the
// user's memory. The second return is false when there is nothing to report.
func checkMacOSFileAccess() (agentCheck, bool) {
	if runtime.GOOS != "darwin" {
		return agentCheck{}, false
	}
	registry, _, err := agentRegistry()
	if err != nil {
		return agentCheck{}, false
	}
	// Grouped by folder: five workspaces in ~/Documents is one dialog to
	// explain, not five, and the line stays readable.
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
	}
	return c, true
}
