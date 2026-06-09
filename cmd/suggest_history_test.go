package cmd

import (
	"encoding/json"
	"os"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func runSuggestHistory(t *testing.T, args ...string) {
	t.Helper()
	rootCmd.SetArgs(append([]string{"suggest-history"}, args...))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("suggest-history %v failed: %v", args, err)
	}
}

func TestSuggestHistoryListAbsentIsPureJSON(t *testing.T) {
	withTempHome(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })

	out := captureStdout(t, func() { runSuggestHistory(t, "list") })
	var h utils.SuggestHistory
	if err := json.Unmarshal([]byte(out), &h); err != nil {
		t.Fatalf("absent store must emit valid JSON, got %q (%v)", out, err)
	}
	if len(h.Entries) != 0 {
		t.Fatalf("absent store must list empty, got %d", len(h.Entries))
	}
}

func TestSuggestHistoryRecordThenCheckDedupes(t *testing.T) {
	withTempHome(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })

	captureStdout(t, func() {
		runSuggestHistory(t, "record", "--slug", "demo", "--status", "proposed", "--title", "Demo", "--lens", "eng")
	})

	out := captureStdout(t, func() { runSuggestHistory(t, "check", "--slug", "demo") })
	var res suggestCheckResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("check must emit valid JSON, got %q (%v)", out, err)
	}
	if !res.Skip || res.Reason != "proposed" {
		t.Fatalf("expected skip=true reason=proposed, got %+v", res)
	}

	out = captureStdout(t, func() { runSuggestHistory(t, "check", "--slug", "other") })
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("check must emit valid JSON, got %q (%v)", out, err)
	}
	if res.Skip {
		t.Fatalf("unknown slug must not skip, got %+v", res)
	}
}

func TestSuggestHistoryRateLimitBlocksSecondFile(t *testing.T) {
	withTempHome(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })

	captureStdout(t, func() {
		runSuggestHistory(t, "record", "--slug", "first", "--status", "filed", "--ticket", "ABC-1")
	})
	// Default cap is 1/week → a fresh candidate is rate-limited.
	out := captureStdout(t, func() { runSuggestHistory(t, "check", "--slug", "second") })
	var res suggestCheckResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("check must emit valid JSON, got %q (%v)", out, err)
	}
	if !res.Skip || res.Reason != "rate-limit" {
		t.Fatalf("expected skip=true reason=rate-limit, got %+v", res)
	}
}
