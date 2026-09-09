package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

// usageHome points HOME and the agent dir at temp dirs and hands back both.
func usageHome(t *testing.T) (home, agentD string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	agentD, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agentD, 0o700); err != nil {
		t.Fatal(err)
	}
	return home, agentD
}

func writeStatsCacheFor(t *testing.T, configDir string, now time.Time) {
	t.Helper()
	body := `{"dailyActivity":[{"date":"` + usage.Today(now) + `","messageCount":12,"sessionCount":2,"toolCallCount":7}],` +
		`"dailyModelTokens":[{"date":"` + usage.Today(now) + `","tokensByModel":{"claude-opus-4-7-20260101":2500000,"claude-sonnet-5":1500}}]}`
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "stats-cache.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeBoardAccounts(t *testing.T, agentD string, accounts []sessions.Account) {
	t.Helper()
	data, err := json.Marshal(sessions.State{Size: 6, Accounts: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemon.SessionsPath(agentD), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func recordWaits(t *testing.T, agentD string, now time.Time) {
	t.Helper()
	for _, w := range []usage.Wait{
		{At: now.Add(-2 * time.Hour), Kind: "wait", Label: "acme", Seconds: 30},
		{At: now.Add(-time.Hour), Kind: "wait", Label: "web", Seconds: 300},
		{At: now.Add(-30 * time.Minute), Kind: "limited", Label: "acme", Seconds: 600},
		{At: now.Add(-48 * time.Hour), Kind: "wait", Label: "yesterday", Seconds: 9999},
	} {
		if err := usage.RecordWait(agentD, w); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartOfDay(t *testing.T) {
	now := time.Date(2026, 9, 8, 17, 45, 12, 99, time.Local)
	if got := startOfDay(now); !got.Equal(time.Date(2026, 9, 8, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("got %v", got)
	}
}

func TestBuildUsageReportPrefersTheBoard(t *testing.T) {
	home, agentD := usageHome(t)
	now := time.Now()
	work := filepath.Join(home, ".claude-work")
	writeStatsCacheFor(t, work, now)
	writeBoardAccounts(t, agentD, []sessions.Account{
		{Profile: "work", ConfigDir: work, Limits: &usage.Limits{FetchedAt: now, FiveHour: usage.Window{Percent: 40}}, Sessions: 2},
		{Profile: "default"},
	})
	recordWaits(t, agentD, now)

	rep := buildUsageReport(agentD, now)
	if !rep.At.Equal(now) || len(rep.Accounts) != 2 || rep.Accounts[0].Profile != "work" || rep.Accounts[0].Sessions != 2 {
		t.Fatalf("accounts: %+v", rep.Accounts)
	}
	if len(rep.Today) != 1 || rep.Today[0].Profile != "work" || rep.Today[0].Messages != 12 || len(rep.Today[0].Models) != 2 {
		t.Fatalf("today: %+v", rep.Today)
	}
	if rep.Waits.Count != 2 || rep.Waits.Longest != 300 || rep.Waits.LongestLabel != "web" || rep.Waits.Total != 330 {
		t.Fatalf("waits: %+v", rep.Waits)
	}
	if rep.Limited.Count != 1 || rep.Limited.Total != 600 {
		t.Fatalf("limited: %+v", rep.Limited)
	}
}

func TestBuildUsageReportFallsBackToTheCaches(t *testing.T) {
	home, agentD := usageHome(t)
	now := time.Now()
	fetched := now.Add(-time.Minute).UnixMilli()
	cached := `{"cachedUsageUtilization":{"fetchedAtMs":` + itoa64(fetched) + `,"utilization":{"five_hour":{"utilization":55,"resets_at":"` +
		now.Add(2*time.Hour).UTC().Format(time.RFC3339Nano) + `"},"seven_day":{"utilization":10.4,"resets_at":"` +
		now.Add(72*time.Hour).UTC().Format(time.RFC3339Nano) + `"}}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(cached), 0o600); err != nil {
		t.Fatal(err)
	}
	writeStatsCacheFor(t, filepath.Join(home, ".claude"), now)
	// Four readings 10 minutes apart give the forecast a slope to project.
	for i := 0; i <= 3; i++ {
		at := now.Add(-time.Duration(30-10*i) * time.Minute)
		l := usage.Limits{FetchedAt: at, FiveHour: usage.Window{Percent: 10 + 10*i}, SevenDay: usage.Window{Percent: 10}}
		if _, err := usage.RecordSample(agentD, "default", l, at); err != nil {
			t.Fatal(err)
		}
	}

	rep := buildUsageReport(agentD, now)
	if len(rep.Accounts) != 1 || rep.Accounts[0].Profile != "default" || rep.Accounts[0].ConfigDir != "" {
		t.Fatalf("accounts: %+v", rep.Accounts)
	}
	a := rep.Accounts[0]
	if a.Limits == nil || a.Limits.FiveHour.Percent != 55 || a.Limits.SevenDay.Percent != 10 {
		t.Fatalf("limits: %+v", a.Limits)
	}
	if a.Forecast == nil || a.Forecast.FiveHour == nil || a.Forecast.FiveHour.PercentPerHour != 60 {
		t.Fatalf("forecast: %+v", a.Forecast)
	}
	if len(rep.Today) != 1 || rep.Today[0].Profile != "default" || rep.Today[0].Sessions != 2 {
		t.Fatalf("today: %+v", rep.Today)
	}
	if rep.Waits.Count != 0 || rep.Limited.Count != 0 {
		t.Fatalf("no waits recorded: %+v %+v", rep.Waits, rep.Limited)
	}
}

func itoa64(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestLimitBar(t *testing.T) {
	reset := time.Now().Add(time.Hour)
	for _, tc := range []struct {
		w    usage.Window
		want string
	}{
		{usage.Window{Percent: 62}, " 62% ▓▓▓▓▓▓░░░░"},
		{usage.Window{Percent: 0}, "  0% ░░░░░░░░░░"},
		{usage.Window{Percent: 100}, "100% ▓▓▓▓▓▓▓▓▓▓"},
		{usage.Window{Percent: 150}, "150% ▓▓▓▓▓▓▓▓▓▓"},
		{usage.Window{Percent: 5, ResetsAt: reset}, "  5% ░░░░░░░░░░ resets " + reset.Local().Format("3:04pm")},
	} {
		if got := limitBar(tc.w); got != tc.want {
			t.Errorf("limitBar(%+v) = %q, want %q", tc.w, got, tc.want)
		}
	}
}

func TestResetTextSaysTheDayOnlyWhenItIsFar(t *testing.T) {
	soon := time.Now().Add(3 * time.Hour)
	far := time.Now().Add(49 * time.Hour)
	if got := resetText(soon); got != soon.Local().Format("3:04pm") {
		t.Errorf("soon: %q", got)
	}
	if got := resetText(far); got != far.Local().Format("Mon 3:04pm") {
		t.Errorf("far: %q", got)
	}
}

func TestForecastLine(t *testing.T) {
	now := time.Now()
	exhaust := now.Add(time.Hour)
	reset := now.Add(2 * time.Hour)
	weekExhaust := now.Add(30 * time.Hour)
	l := &usage.Limits{FiveHour: usage.Window{Percent: 40, ResetsAt: reset}}
	for name, tc := range map[string]struct {
		f    *usage.Forecast
		want string
	}{
		"no forecast": {nil, ""},
		"flat":        {&usage.Forecast{FiveHour: &usage.WindowForecast{}}, "5h: pace flat"},
		"safe":        {&usage.Forecast{FiveHour: &usage.WindowForecast{PercentPerHour: 12.4, ExhaustAt: exhaust, Safe: true}}, "5h: 12%/h, lasts until the reset"},
		"runs out": {&usage.Forecast{FiveHour: &usage.WindowForecast{PercentPerHour: 60, ExhaustAt: exhaust}},
			"5h: 60%/h, RUNS OUT " + resetText(exhaust) + " (before the " + resetText(reset) + " reset)"},
		"week flat is silent": {&usage.Forecast{SevenDay: &usage.WindowForecast{Safe: true}}, ""},
		"week fine":           {&usage.Forecast{SevenDay: &usage.WindowForecast{ExhaustAt: weekExhaust, Safe: true}}, "week: fine"},
		"week runs out":       {&usage.Forecast{SevenDay: &usage.WindowForecast{ExhaustAt: weekExhaust}}, "week: RUNS OUT " + resetText(weekExhaust)},
		"both": {&usage.Forecast{FiveHour: &usage.WindowForecast{}, SevenDay: &usage.WindowForecast{ExhaustAt: weekExhaust, Safe: true}},
			"5h: pace flat · week: fine"},
	} {
		if got := forecastLine(l, tc.f); got != tc.want {
			t.Errorf("%s: got %q want %q", name, got, tc.want)
		}
	}
}

func TestForecastSuffixOnlyWarns(t *testing.T) {
	exhaust := time.Now().Add(time.Hour)
	for name, tc := range map[string]struct {
		f    *usage.Forecast
		want string
	}{
		"nil":       {nil, ""},
		"no window": {&usage.Forecast{}, ""},
		"flat":      {&usage.Forecast{FiveHour: &usage.WindowForecast{}}, ""},
		"safe":      {&usage.Forecast{FiveHour: &usage.WindowForecast{ExhaustAt: exhaust, Safe: true}}, ""},
		"runs out":  {&usage.Forecast{FiveHour: &usage.WindowForecast{ExhaustAt: exhaust}}, " · runs out " + resetText(exhaust)},
	} {
		if got := forecastSuffix(tc.f); got != tc.want {
			t.Errorf("%s: got %q want %q", name, got, tc.want)
		}
	}
}

func TestShortModel(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-4-7-20260101":  "opus-4-7",
		"claude-sonnet-5":           "sonnet-5",
		"claude-haiku-4-5-20251001": "haiku-4-5",
		"gpt-5":                     "gpt-5",
		"":                          "",
	} {
		if got := shortModel(in); got != want {
			t.Errorf("shortModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrintUsageReport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Now()
	rep := usageReport{
		At: now,
		Accounts: []sessions.Account{
			{Profile: "default"},
			{Profile: "work", ConfigDir: filepath.Join(home, ".claude-work"), Sessions: 3,
				Limits:   &usage.Limits{FetchedAt: now.Add(-5 * time.Minute), FiveHour: usage.Window{Percent: 62, ResetsAt: now.Add(time.Hour)}, SevenDay: usage.Window{Percent: 10}},
				Forecast: &usage.Forecast{FiveHour: &usage.WindowForecast{}}},
		},
		Today: []accountDayJSON{{Profile: "work", DayStats: usage.DayStats{Sessions: 2, Messages: 12, ToolCalls: 7,
			Models: []usage.ModelTokens{{Model: "claude-opus-4-7-20260101", Tokens: 2500000}, {Model: "claude-sonnet-5", Tokens: 1500}}}}},
		Waits:   usage.WaitSummary{Count: 2, Median: 30, Longest: 300, LongestLabel: "web", Total: 330},
		Limited: usage.WaitSummary{Count: 1, Total: 600},
	}
	out := captureStdout(t, func() { printUsageReport(rep) })
	for _, want := range []string{
		"Accounts",
		"default                      no usage snapshot yet",
		"work (~/.claude-work)",
		"5h  62% ▓▓▓▓▓▓░░░░ resets",
		"week  10% ▓░░░░░░░░░",
		"3 session(s) · as of 5m ago",
		"5h: pace flat",
		"Today (Claude Code's own stats cache; lags by hours)",
		"work                         2 sessions · 12 messages · 7 tool calls · opus-4-7 2.5M, sonnet-5 1k",
		"Waiting on you today",
		"2 waits · median 30s · longest 5m (web) · total 5m30s",
		"limits cost 10m across 1 session(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	out = captureStdout(t, func() { printUsageReport(usageReport{At: now}) })
	if !strings.Contains(out, "nothing waited yet") || strings.Contains(out, "Today (") || strings.Contains(out, "limits cost") {
		t.Errorf("empty report:\n%s", out)
	}
}

func usageCmd(t *testing.T) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "usage"}
	c.Flags().Bool("watch", false, "")
	return c
}

func TestRunAgentUsagePrintsOnceWithoutWatch(t *testing.T) {
	_, agentD := usageHome(t)
	now := time.Now()
	writeBoardAccounts(t, agentD, []sessions.Account{{Profile: "work", Limits: &usage.Limits{FetchedAt: now}}})
	recordWaits(t, agentD, now)

	orig := utils.JSONOutput
	t.Cleanup(func() { utils.JSONOutput = orig })

	utils.JSONOutput = false
	out := captureStdout(t, func() { runAgentUsage(usageCmd(t), nil) })
	if !strings.Contains(out, "work") || !strings.Contains(out, "2 waits") {
		t.Fatalf("human:\n%s", out)
	}

	utils.JSONOutput = true
	out = captureStdout(t, func() { runAgentUsage(usageCmd(t), nil) })
	var rep usageReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(rep.Accounts) != 1 || rep.Accounts[0].Profile != "work" || rep.Waits.Count != 2 || rep.Limited.Count != 1 || rep.Waits.LongestLabel != "web" {
		t.Fatalf("json report: %+v", rep)
	}
	if !strings.Contains(out, `"waitsToday"`) || !strings.Contains(out, `"limitedToday"`) || !strings.Contains(out, `"at"`) {
		t.Fatalf("json keys:\n%s", out)
	}
	// --watch with --json still prints once and returns.
	c := usageCmd(t)
	if err := c.Flags().Set("watch", "true"); err != nil {
		t.Fatal(err)
	}
	if out := captureStdout(t, func() { runAgentUsage(c, nil) }); !strings.Contains(out, `"accounts"`) {
		t.Fatalf("watch+json:\n%s", out)
	}
}

func TestDigestText(t *testing.T) {
	home, agentD := usageHome(t)
	now := time.Now()
	if got := digestText(agentD, now); got != "" {
		t.Fatalf("nothing on record is an empty digest, got %q", got)
	}

	work := filepath.Join(home, ".claude-work")
	idle := filepath.Join(home, ".claude-idle")
	writeStatsCacheFor(t, work, now)
	if err := os.MkdirAll(idle, 0o700); err != nil {
		t.Fatal(err)
	}
	// A day with tokens but no sessions or messages is not worth a line.
	if err := os.WriteFile(filepath.Join(idle, "stats-cache.json"), []byte(`{"dailyModelTokens":[{"date":"`+usage.Today(now)+`","tokensByModel":{"m":1}}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeBoardAccounts(t, agentD, []sessions.Account{
		{Profile: "work", ConfigDir: work, Limits: &usage.Limits{FetchedAt: now, FiveHour: usage.Window{Percent: 62}, SevenDay: usage.Window{Percent: 10}}},
		{Profile: "idle", ConfigDir: idle},
	})
	recordWaits(t, agentD, now)

	got := digestText(agentD, now)
	want := strings.Join([]string{
		"work: 2 sessions, 12 messages, 7 tool calls",
		"waited on you 2× — median 30s, longest 5m (web)",
		"limits cost 10m",
		"work: 5h 62%, week 10%",
	}, "\n")
	if got != want {
		t.Fatalf("digest:\n%s\nwant:\n%s", got, want)
	}
}

func digestCmd(t *testing.T, send bool) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "digest"}
	c.Flags().Bool("send", false, "")
	if send {
		if err := c.Flags().Set("send", "true"); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestAgentDigestCommand(t *testing.T) {
	_, agentD := usageHome(t)
	orig := utils.JSONOutput
	t.Cleanup(func() { utils.JSONOutput = orig })
	// Notifications read ~/.corgi/config.yml once; with HOME pointed at an
	// empty dir --send has nowhere to go, which is the point.
	utils.ResetNotifyCache()
	t.Cleanup(utils.ResetNotifyCache)

	utils.JSONOutput = false
	out := captureStdout(t, func() { agentDigestCmd.Run(digestCmd(t, false), nil) })
	if strings.TrimSpace(out) != "nothing on record today" {
		t.Fatalf("empty digest: %q", out)
	}

	now := time.Now()
	writeBoardAccounts(t, agentD, []sessions.Account{{Profile: "work", Limits: &usage.Limits{FetchedAt: now, FiveHour: usage.Window{Percent: 7}}}})
	out = captureStdout(t, func() { agentDigestCmd.Run(digestCmd(t, true), nil) })
	if strings.TrimSpace(out) != "work: 5h 7%, week 0%" {
		t.Fatalf("digest with --send: %q", out)
	}

	utils.JSONOutput = true
	out = captureStdout(t, func() { agentDigestCmd.Run(digestCmd(t, true), nil) })
	var got struct {
		Text string `json:"text"`
		Sent bool   `json:"sent"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Text != "work: 5h 7%, week 0%" || !got.Sent {
		t.Fatalf("json digest: %+v %v\n%s", got, err, out)
	}
}
