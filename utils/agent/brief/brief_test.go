package brief

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func sampleRepos() []RepoState {
	return []RepoState{
		{Service: "web", Dir: "/s/web", Branch: "feature/referral", Dirty: true, Worktree: true},
		{Service: "api", Dir: "/s/api", Branch: "feature/referral", Worktree: true},
	}
}

func TestCaptureSortsReposForAStableRender(t *testing.T) {
	// Map iteration produces these in a random order, and a note that reshuffles
	// itself between reads is hard to trust.
	b := Capture(Params{WorkspaceID: "acme"}, sampleRepos())

	if len(b.Repos) != 2 || b.Repos[0].Service != "api" {
		t.Fatalf("repos = %+v, want them sorted by service", b.Repos)
	}
}

func TestCaptureDefaultsTheTimestamp(t *testing.T) {
	b := Capture(Params{WorkspaceID: "acme"}, nil)

	if b.EndedAt.IsZero() {
		t.Error("a brief with no end time cannot be ordered against any other")
	}
	if b.EndedAt.Location() != time.UTC {
		t.Error("stored time must be UTC — a brief outlives the timezone the daemon started in")
	}
}

func TestEmptyIsTrueWhenThereIsNothingToSay(t *testing.T) {
	// "It restarted" is already in the notification. A brief that adds nothing
	// must say so, or every restart gains a second empty line of noise.
	tests := []struct {
		name  string
		repos []RepoState
		want  bool
	}{
		{"no repos", nil, true},
		{"repos with no branch or changes", []RepoState{{Service: "api", Dir: "/s/api"}}, true},
		{"a branch", []RepoState{{Service: "api", Branch: "main"}}, false},
		{"uncommitted work", []RepoState{{Service: "api", Dirty: true}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Capture(Params{WorkspaceID: "acme"}, tt.repos).Empty(); got != tt.want {
				t.Errorf("Empty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSummaryNamesTheBranchAndCountsTheRest(t *testing.T) {
	b := Capture(Params{WorkspaceID: "acme"}, sampleRepos())

	got := b.Summary()
	if !strings.Contains(got, "feature/referral") {
		t.Errorf("summary %q must name the branch — that is the whole point of it", got)
	}
	if !strings.Contains(got, "1 repo has uncommitted changes") {
		t.Errorf("summary %q must count uncommitted work", got)
	}
	// One branch shared by two repos is one fact, not two.
	if strings.Count(got, "feature/referral") != 1 {
		t.Errorf("summary %q repeats a shared branch", got)
	}
}

func TestSummaryListsEveryDistinctBranch(t *testing.T) {
	// A stack half-materialized onto a branch is exactly the state worth
	// reporting, so both names have to appear.
	b := Capture(Params{WorkspaceID: "acme"}, []RepoState{
		{Service: "api", Branch: "feature/referral"},
		{Service: "web", Branch: "main"},
	})

	got := b.Summary()
	for _, want := range []string{"feature/referral", "main"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q missing branch %q", got, want)
		}
	}
}

func TestSummaryPluralizesTheDirtyCount(t *testing.T) {
	b := Capture(Params{WorkspaceID: "acme"}, []RepoState{
		{Service: "api", Branch: "main", Dirty: true},
		{Service: "web", Branch: "main", Dirty: true},
	})

	if got := b.Summary(); !strings.Contains(got, "2 repos have uncommitted changes") {
		t.Errorf("summary = %q, want a plural count", got)
	}
}

func TestSummaryIsEmptyWhenTheBriefIs(t *testing.T) {
	if got := Capture(Params{WorkspaceID: "acme"}, nil).Summary(); got != "" {
		t.Errorf("Summary() = %q, want empty so the notification stays one line", got)
	}
}

func TestWriteThenRead(t *testing.T) {
	dir := t.TempDir()
	want := Capture(Params{
		WorkspaceID: "acme",
		Dir:         "/dev/acme",
		Cause:       "network-timeout",
		Reason:      "remote control restarted",
		Restarts:    3,
	}, sampleRepos())

	if err := Write(dir, want); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	got, err := Read(dir, "acme")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got == nil {
		t.Fatal("Read() = nil after a successful write")
	}
	if got.Cause != want.Cause || got.Restarts != 3 || len(got.Repos) != 2 {
		t.Errorf("Read() = %+v, want the written brief back", got)
	}
}

func TestReadMissingIsNotAnError(t *testing.T) {
	// The ordinary case is a session that has never restarted. Treating that as
	// an error would make every first call look like a fault.
	got, err := Read(t.TempDir(), "acme")
	if err != nil {
		t.Fatalf("Read() error = %v, want nil for a workspace with no brief", err)
	}
	if got != nil {
		t.Errorf("Read() = %+v, want nil", got)
	}
}

func TestWriteReplacesTheEarlierBrief(t *testing.T) {
	// Only the latest is a handover note; an older one is a different session's
	// state wearing the same name.
	dir := t.TempDir()
	first := Capture(Params{WorkspaceID: "acme", Cause: "crash"}, sampleRepos())
	if err := Write(dir, first); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	second := Capture(Params{WorkspaceID: "acme", Cause: "network-timeout"}, sampleRepos())
	if err := Write(dir, second); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(dir, "acme")
	if err != nil || got == nil {
		t.Fatalf("Read() = %v, %v", got, err)
	}
	if got.Cause != "network-timeout" {
		t.Errorf("cause = %q, want the most recent one", got.Cause)
	}
}

func TestWriteLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, Capture(Params{WorkspaceID: "acme"}, sampleRepos())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dir, dirName))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("left %s behind; List would skip it but it accumulates", e.Name())
		}
	}
}

func TestWriteIsNotReadableByOtherUsers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Go reports 0666 for every file on Windows")
	}
	dir := t.TempDir()
	if err := Write(dir, Capture(Params{WorkspaceID: "acme"}, sampleRepos())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	info, err := os.Stat(Path(dir, "acme"))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	// A brief names repository paths and branch names — more than a passer-by
	// on a shared machine should be handed.
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("brief mode = %04o, want no group or world access", mode)
	}
}

func TestWriteRequiresAWorkspaceID(t *testing.T) {
	if err := Write(t.TempDir(), Brief{}); err == nil {
		t.Fatal("a brief with no workspace has no filename and must fail")
	}
}

func TestListIsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	older := Capture(Params{WorkspaceID: "old", EndedAt: time.Now().Add(-time.Hour)}, sampleRepos())
	newer := Capture(Params{WorkspaceID: "new", EndedAt: time.Now()}, sampleRepos())
	for _, b := range []Brief{older, newer} {
		if err := Write(dir, b); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	got, err := List(dir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 || got[0].WorkspaceID != "new" {
		t.Errorf("List() = %+v, want the most recent restart first", got)
	}
}

func TestListOnAMissingDirectoryIsEmpty(t *testing.T) {
	got, err := List(t.TempDir())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List() = %+v, want empty", got)
	}
}

func TestListSkipsUnreadableEntriesRatherThanFailing(t *testing.T) {
	// One corrupt file must not hide every other workspace's note.
	dir := t.TempDir()
	if err := Write(dir, Capture(Params{WorkspaceID: "good"}, sampleRepos())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, dirName, "broken.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := List(dir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 || got[0].WorkspaceID != "good" {
		t.Errorf("List() = %+v, want the readable brief only", got)
	}
}

func TestClearRemovesTheNote(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, Capture(Params{WorkspaceID: "acme"}, sampleRepos())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := Clear(dir, "acme"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	got, err := Read(dir, "acme")
	if err != nil || got != nil {
		t.Errorf("Read() = %v, %v after Clear", got, err)
	}
	// Clearing something already gone is what `workspaces forget` does on a
	// workspace that never restarted.
	if err := Clear(dir, "acme"); err != nil {
		t.Errorf("Clear() on a missing brief = %v, want nil", err)
	}
}

func TestPathStaysInsideTheBriefsDirectory(t *testing.T) {
	// Ids come from a registry corgi writes, but this builds a filename from a
	// name, and that is worth closing wherever it appears.
	dir := t.TempDir()
	base := filepath.Join(dir, dirName)

	for _, id := range []string{"../escape", "..", "a/b", `..\escape`, "."} {
		got := Path(dir, id)
		if filepath.Dir(got) != base {
			t.Errorf("Path(%q) = %q, want it inside %q", id, got, base)
		}
	}
}

func TestWriteAndReadAgreeOnASanitizedID(t *testing.T) {
	// Both sides go through the same mapping, so an id needing sanitizing still
	// round-trips rather than writing one file and reading another.
	dir := t.TempDir()
	const id = "acme/stack"
	if err := Write(dir, Capture(Params{WorkspaceID: id}, sampleRepos())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	got, err := Read(dir, id)
	if err != nil || got == nil {
		t.Fatalf("Read() = %v, %v", got, err)
	}
	if got.WorkspaceID != id {
		t.Errorf("workspaceId = %q, want the original %q preserved in the payload", got.WorkspaceID, id)
	}
}

func TestDistinctIDsDoNotShareAFile(t *testing.T) {
	// Replacing unsafe runes alone maps both of these onto "acme-stack", so one
	// workspace's brief would overwrite the other's and then be served for it.
	dir := t.TempDir()
	if err := Write(dir, Capture(Params{WorkspaceID: "acme/stack", Cause: "slash"}, sampleRepos())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := Write(dir, Capture(Params{WorkspaceID: "acme-stack", Cause: "dash"}, sampleRepos())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	slash, err := Read(dir, "acme/stack")
	if err != nil || slash == nil {
		t.Fatalf("Read(acme/stack) = %v, %v", slash, err)
	}
	if slash.Cause != "slash" {
		t.Errorf("acme/stack got cause %q — another workspace's brief overwrote it", slash.Cause)
	}
	if Path(dir, "acme/stack") == Path(dir, "acme-stack") {
		t.Error("two distinct ids resolved to the same file")
	}
}

func TestIDsMatchCaseInsensitively(t *testing.T) {
	// The registry compares ids with EqualFold, so `workspaces forget ACME`
	// drops the row. If the brief keyed on case it would survive and resurface
	// against whatever stack next took that id.
	dir := t.TempDir()
	if err := Write(dir, Capture(Params{WorkspaceID: "acme"}, sampleRepos())); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if got, _ := Read(dir, "ACME"); got == nil {
		t.Error("Read is case-sensitive but the registry is not")
	}
	if err := Clear(dir, "ACME"); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if got, _ := Read(dir, "acme"); got != nil {
		t.Error("Clear with different casing left the brief behind")
	}
}
