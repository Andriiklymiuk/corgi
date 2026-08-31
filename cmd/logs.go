package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var logsServiceFlag string
var logsPruneFlag bool
var logsAllFlag bool
var logsIdleFlag time.Duration
var logsDumpFlag string
var logsSessionFlag bool
var logsWaitForFlag string
var logsGrepFlag string
var logsSinceFlag string
var logsTimeoutFlag time.Duration

var logsCmd = &cobra.Command{
	Use:     "logs",
	Short:   "Browse and follow persisted service logs",
	Aliases: []string{"log"},
	Long: `Browse logs captured by corgi run (capture is on unless --logs=false).

Without flags: interactive picker — choose a service then a run, and
the log is streamed to stdout (follows new writes like tail -f).

Examples:
  corgi logs                      # interactive picker
  corgi logs --service api        # jump straight to run picker for "api"
  corgi logs --all                # merge the newest run of every service into one stream
  corgi logs --session            # corgi's own output for the last run
  corgi logs --idle 0             # tail forever (until Ctrl-C)
  corgi logs --prune              # delete all .logs/ directories

Waiting, for scripts and agents:
  corgi logs --service api --wait-for "Listening on" --timeout 90s
  corgi logs --service api --since 5m --grep "ERROR" --json

--wait-for blocks until a line matches, then exits 0. It exits 1 on timeout, so
a shell step can gate on it without parsing output.`,
	Run: runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().StringVar(&logsServiceFlag, "service", "", "Service name to show logs for (skips service picker)")
	logsCmd.Flags().BoolVar(&logsPruneFlag, "prune", false, "Delete all captured log files (corgi_services/.logs/)")
	logsCmd.Flags().BoolVar(&logsAllFlag, "all", false, "Merge the newest run of every service into one timestamp-sorted stream")
	logsCmd.Flags().DurationVar(&logsIdleFlag, "idle", 30*time.Second, "Exit after this much dead-air on the file (set 0 to tail forever)")
	logsCmd.Flags().StringVar(&logsDumpFlag, "dump", "", "Copy the newest run of every service into this directory and exit (for CI artifacts)")
	logsCmd.Flags().BoolVar(&logsSessionFlag, "session", false, "Show corgi's own run output (validation errors, port conflicts, resolved branches) instead of a service's")
	logsCmd.Flags().StringVar(&logsWaitForFlag, "wait-for", "", "Block until a line matches this regexp, then exit 0 (exit 1 on timeout)")
	logsCmd.Flags().StringVar(&logsGrepFlag, "grep", "", "Only show lines matching this regexp")
	logsCmd.Flags().StringVar(&logsSinceFlag, "since", "", "Only show lines newer than this (duration like 5m, or an RFC3339 timestamp)")
	logsCmd.Flags().DurationVar(&logsTimeoutFlag, "timeout", 60*time.Second, "How long --wait-for waits before giving up")
}

type logStreamFilter struct {
	since    time.Time
	grep     *regexp.Regexp
	waitFor  *regexp.Regexp
	deadline time.Time
}

var activeLogFilter logStreamFilter

func buildLogStreamFilter() (logStreamFilter, error) {
	var filter logStreamFilter
	if logsSinceFlag != "" {
		since, err := parseSince(logsSinceFlag)
		if err != nil {
			return filter, err
		}
		filter.since = since
	}
	if logsGrepFlag != "" {
		re, err := regexp.Compile(logsGrepFlag)
		if err != nil {
			return filter, fmt.Errorf("--grep is not a valid regexp: %v", err)
		}
		filter.grep = re
	}
	if logsWaitForFlag != "" {
		re, err := regexp.Compile(logsWaitForFlag)
		if err != nil {
			return filter, fmt.Errorf("--wait-for is not a valid regexp: %v", err)
		}
		filter.waitFor = re
		filter.deadline = time.Now().Add(logsTimeoutFlag)
	}
	return filter, nil
}

func parseSince(raw string) (time.Time, error) {
	if d, err := time.ParseDuration(raw); err == nil {
		return time.Now().Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("--since %q is neither a duration (5m) nor an RFC3339 timestamp", raw)
}

func (f logStreamFilter) waiting() bool { return f.waitFor != nil }

func (f logStreamFilter) allows(ts, content string) bool {
	if !f.since.IsZero() {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(ts))
		if err != nil || at.Before(f.since) {
			return false
		}
	}
	if f.grep != nil && !f.grep.MatchString(content) {
		return false
	}
	return true
}

func logJSONLine(service, ts, level, line string) string {
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}
	b, _ := json.Marshal(struct {
		Service string `json:"service"`
		TS      string `json:"ts"`
		Level   string `json:"level"`
		Line    string `json:"line"`
	}{service, ts, level, line})
	return string(b)
}

// detectLevel is a best-effort log-level guess from a line's content.
func detectLevel(line string) string {
	low := strings.ToLower(line)
	switch {
	case strings.Contains(low, "error") || strings.Contains(low, "panic") || strings.Contains(low, "fatal"):
		return "error"
	case strings.Contains(low, "warn"):
		return "warn"
	default:
		return "info"
	}
}

func runLogs(cmd *cobra.Command, _ []string) {
	base := logsBase()

	if logsPruneFlag {
		pruneAllLogs(base)
		return
	}

	if logsDumpFlag != "" {
		if err := dumpNewestLogs(base, logsDumpFlag); err != nil {
			fmt.Fprintln(os.Stderr, err)
			exitProcess(1)
		}
		return
	}

	filter, err := buildLogStreamFilter()
	if err != nil {
		exitWithError(utils.ErrUsage, err, 2)
		return
	}
	activeLogFilter = filter

	if logsAllFlag {
		if filter.waiting() {
			exitWithError(utils.ErrUsage,
				fmt.Errorf("--wait-for needs one service; pass --service <name> instead of --all"), 2)
			return
		}
		if err := followAllLogs(base); err != nil {
			utils.Info(err)
		}
		return
	}

	serviceName := logsServiceFlag
	if logsSessionFlag {
		serviceName = utils.SessionLogDir
	}
	if serviceName == "" {
		var err error
		serviceName, err = chooseLogService(base)
		if err != nil {
			utils.Info(err)
			return
		}
	}

	if activeLogFilter.waiting() {
		runWaitForLog(base, serviceName)
		return
	}

	runFile, err := pickLogRun(base, serviceName)
	if err != nil {
		utils.Info(err)
		return
	}

	followLog(runFile)
}

func runWaitForLog(base, serviceName string) {
	runFile, err := awaitNewestRun(base, serviceName, activeLogFilter.deadline)
	if err != nil {
		if !utils.JSONOutput {
			utils.Info(err)
		}
		exitProcess(1)
		return
	}
	if !followLog(runFile) {
		if !utils.JSONOutput {
			utils.Infof("timed out after %s waiting for %q in %s\n", logsTimeoutFlag, logsWaitForFlag, serviceName)
		}
		exitProcess(1)
	}
}

func awaitNewestRun(base, serviceName string, deadline time.Time) (string, error) {
	for {
		runs, err := utils.ListServiceRuns(base, serviceName)
		if err == nil && len(runs) > 0 {
			return runs[0], nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("no log file appeared for %s within %s", serviceName, logsTimeoutFlag)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func chooseLogService(base string) (string, error) {
	if utils.NonInteractive {
		available, _ := utils.ListLoggedServices(base)
		if err := requireServiceForLogs("", true, available); err != nil {
			exitWithError(utils.ErrInteractiveReq, err, 2)
		}
	}
	return pickLogService(base)
}

func logsBase() string {
	corgiDir := utils.CorgiComposePathDir
	if corgiDir == "" {
		corgiDir = "."
	}
	return filepath.Join(corgiDir, "corgi_services")
}

func pruneAllLogs(base string) {
	logsDir := filepath.Join(base, ".logs")
	if err := os.RemoveAll(logsDir); err != nil {
		fmt.Printf("prune failed: %v\n", err)
		return
	}
	fmt.Println(art.GreenColor, "✅ All log files removed.", art.WhiteColor)
}

// dumpNewestLogs copies each service's newest run into dir as <service>.log.
func dumpNewestLogs(base, dir string) error {
	services, err := utils.ListLoggedStreams(base)
	if err != nil || len(services) == 0 {
		return fmt.Errorf("no log directories found under %s/.logs/", base)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var copied int
	for _, svc := range services {
		runs, runErr := utils.ListServiceRuns(base, svc)
		if runErr != nil || len(runs) == 0 {
			continue
		}
		if err := copyFile(runs[0], filepath.Join(dir, sanitizeLogName(svc)+".log")); err != nil {
			return fmt.Errorf("dump %s: %v", svc, err)
		}
		copied++
	}
	if copied == 0 {
		return fmt.Errorf("no log files found under %s/.logs/", base)
	}
	fmt.Printf("dumped %d service logs to %s\n", copied, dir)
	return nil
}

func sanitizeLogName(name string) string {
	return strings.NewReplacer("/", "-", string(filepath.Separator), "-").Replace(name)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func requireServiceForLogs(service string, nonInteractive bool, available []string) error {
	if service != "" || !nonInteractive {
		return nil
	}
	return fmt.Errorf("no terminal for the service picker; pass --service <name> (available: %s)",
		strings.Join(available, ", "))
}

func pickLogService(base string) (string, error) {
	services, err := utils.ListLoggedServices(base)
	if err != nil || len(services) == 0 {
		return "", fmt.Errorf("no log directories found under %s/.logs/\nRun the stack first: corgi run (capture is on unless --logs=false)", base)
	}
	return utils.PickItemFromListPrompt("Select service", services, "⬅️  cancel")
}

func pickLogRun(base, serviceName string) (string, error) {
	runs, err := utils.ListServiceRuns(base, serviceName)
	if err != nil || len(runs) == 0 {
		return "", fmt.Errorf("no log files found for %s", serviceName)
	}

	labels := make([]string, len(runs))
	labelToPath := make(map[string]string, len(runs))
	for i, r := range runs {
		labels[i] = labelForRun(r)
		labelToPath[labels[i]] = r
	}

	chosen, err := utils.PickItemFromListPrompt(
		fmt.Sprintf("Select run for %s (newest first)", serviceName),
		labels,
		"⬅️  back",
	)
	if err != nil {
		return "", err
	}
	if p, ok := labelToPath[chosen]; ok {
		return p, nil
	}
	return "", fmt.Errorf("run not found: %s", chosen)
}

// labelForRun makes a picker label that surfaces .ok / .crashed status.
func labelForRun(path string) string {
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(base, ".crashed.log"):
		return strings.TrimSuffix(base, ".crashed.log") + ".log  ❌ crashed"
	case strings.HasSuffix(base, ".ok.log"):
		return strings.TrimSuffix(base, ".ok.log") + ".log  ✅ ok"
	default:
		return base + "  ⏳ in-progress"
	}
}

func followLog(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		utils.Infof("cannot open %s: %v\n", path, err)
		return false
	}
	defer f.Close()

	if !utils.JSONOutput && !activeLogFilter.waiting() {
		fmt.Printf("%s📄 %s (Ctrl-C to exit)%s\n\n", art.CyanColor, path, art.WhiteColor)
	}
	return streamLogLines(f, path)
}

func streamLogLines(f *os.File, path string) bool {
	service := filepath.Base(filepath.Dir(path))
	reader := bufio.NewReader(f)
	stripPrefix := looksLikeStampedLog(path)
	var idleSince time.Time
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if matched := handleFollowedLine(service, line, stripPrefix); matched {
				return true
			}
			idleSince = time.Time{}
		}
		if err == nil {
			continue
		}
		if activeLogFilter.waiting() {
			if time.Now().After(activeLogFilter.deadline) {
				return false
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if followShouldStop(err, path, &idleSince) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func handleFollowedLine(service, line string, stripPrefix bool) bool {
	ts, content := splitLogLine(line, stripPrefix)
	if !activeLogFilter.allows(ts, content) {
		return false
	}
	printFollowedLine(service, line, stripPrefix)
	return activeLogFilter.waitFor != nil && activeLogFilter.waitFor.MatchString(content)
}

func splitLogLine(line string, stripPrefix bool) (ts, content string) {
	content = strings.TrimRight(line, "\n")
	if stripPrefix && len(line) >= utils.LogTimestampLen {
		ts = strings.TrimSpace(line[:utils.LogTimestampLen-1])
		content = strings.TrimRight(line[utils.LogTimestampLen:], "\n")
	}
	return ts, content
}

// followShouldStop reports whether the follow loop should end: a non-EOF read
// error, or EOF past the idle/inactivity threshold. Prints the matching
// human notice (suppressed in JSON mode).
func followShouldStop(err error, path string, idleSince *time.Time) bool {
	if err != io.EOF {
		if !utils.JSONOutput {
			fmt.Printf("read error: %v\n", err)
		}
		return true
	}
	if shouldExitFollow(path, idleSince) {
		if !utils.JSONOutput {
			fmt.Printf("\n%s— end of log —%s\n", art.YellowColor, art.WhiteColor)
		}
		return true
	}
	return false
}

func printFollowedLine(service, line string, stripPrefix bool) {
	ts, content := splitLogLine(line, stripPrefix)
	if utils.JSONOutput {
		fmt.Println(logJSONLine(service, ts, detectLevel(content), content))
		return
	}
	if ts != "" {
		fmt.Println(content)
		return
	}
	fmt.Print(line)
}

func shouldExitFollow(path string, idleSince *time.Time) bool {
	if idleSince.IsZero() {
		*idleSince = time.Now()
	}
	idleExceeded := logsIdleFlag > 0 && time.Since(*idleSince) > logsIdleFlag
	return !isLogFileActive(path) || idleExceeded
}

// looksLikeStampedLog returns true when the file's first 25 bytes match
// the `YYYY-MM-DDTHH:MM:SS.sssZ ` shape written by the logger. Logs from
// older corgi versions (no prefix) are read as-is.
func looksLikeStampedLog(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, utils.LogTimestampLen)
	n, _ := io.ReadFull(f, buf)
	if n < utils.LogTimestampLen {
		return false
	}
	return hasTimestampShape(buf)
}

// timestampShapePattern: 'd' = any digit, every other byte literal (25 bytes).
const timestampShapePattern = "dddd-dd-ddTdd:dd:dd.dddZ "

// hasTimestampShape checks `YYYY-MM-DDTHH:MM:SS.sssZ `.
func hasTimestampShape(b []byte) bool {
	if len(b) < len(timestampShapePattern) {
		return false
	}
	for i := 0; i < len(timestampShapePattern); i++ {
		switch p := timestampShapePattern[i]; {
		case p == 'd':
			if b[i] < '0' || b[i] > '9' {
				return false
			}
		case b[i] != p:
			return false
		}
	}
	return true
}

// mergeStream is one input to the k-way merge — a service's log file read line
// by line, head buffered for heap compare. A Reader rather than a Scanner
// because a Scanner stops for good at EOF and a live log hits EOF constantly.
// A trailing line without a newline is held back until the rest arrives.
type mergeStream struct {
	service string
	reader  *bufio.Reader
	f       *os.File
	headTS  string
	headBuf string
	partial string
}

// readLine returns the next complete line. An incomplete trailing line is kept
// until its newline shows up, so a half-written line is never printed twice.
func (s *mergeStream) readLine() (string, bool) {
	chunk, err := s.reader.ReadString('\n')
	if err != nil {
		s.partial += chunk
		return "", false
	}
	line := s.partial + strings.TrimRight(chunk, "\n")
	s.partial = ""
	return line, true
}

func (s *mergeStream) close() {
	if s.f != nil {
		s.f.Close()
		s.f = nil
	}
}

// followAllLogs merges the newest run of every logged service into one
// timestamp-sorted stream. K-way merge → memory is O(num services), not
// O(total log bytes), so big projects don't OOM the CLI.
func followAllLogs(base string) error {
	services, err := utils.ListLoggedStreams(base)
	if err != nil || len(services) == 0 {
		return fmt.Errorf("no log directories found under %s/.logs/\nRun the stack first: corgi run (capture is on unless --logs=false)", base)
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	streams := map[string]*mergeStream{}
	defer func() {
		for _, st := range streams {
			st.close()
		}
	}()

	var idleSince time.Time
	for {
		adoptNewServices(base, streams)
		wrote := drainStreams(out, streams)
		out.Flush()

		if wrote > 0 {
			idleSince = time.Time{}
		}
		if !keepFollowing(&idleSince) {
			return nil
		}
		time.Sleep(followPollInterval)
	}
}

// followPollInterval is how often the tail checks for new content. Short
// enough to feel live, long enough not to spin.
const followPollInterval = 250 * time.Millisecond

// keepFollowing stops after --idle of dead air. --idle 0 tails forever, which
// is what a CI job wants alongside a booting stack.
func keepFollowing(idleSince *time.Time) bool {
	if idleSince.IsZero() {
		*idleSince = time.Now()
	}
	if logsIdleFlag <= 0 {
		return true
	}
	return time.Since(*idleSince) <= logsIdleFlag
}

// adoptNewServices picks up services that started logging after the follow
// began — a slow one has no log file when the first pass runs.
func adoptNewServices(base string, streams map[string]*mergeStream) {
	services, err := utils.ListLoggedStreams(base)
	if err != nil {
		return
	}
	for _, svc := range services {
		if _, known := streams[svc]; known {
			continue
		}
		if st := openMergeStream(base, svc); st != nil {
			streams[svc] = st
		}
	}
}

// drainStreams writes every line currently available, oldest first, and
// reports how many. Each file is already in order, so sorting one tick's batch
// is enough to interleave services correctly.
func drainStreams(out *bufio.Writer, streams map[string]*mergeStream) int {
	type entry struct {
		ts, line, service string
	}
	var batch []entry
	for _, st := range streams {
		for st.advanceTail() {
			batch = append(batch, entry{st.headTS, st.headBuf, st.service})
		}
	}
	sort.SliceStable(batch, func(i, j int) bool { return batch[i].ts < batch[j].ts })
	for _, e := range batch {
		writeMergedLine(out, e.service, e.ts, e.line)
	}
	return len(batch)
}

// advanceTail is advance for a file still being written: EOF means "nothing
// more yet", not "finished".
func (s *mergeStream) advanceTail() bool {
	line, ok := s.readLine()
	if !ok {
		return false
	}
	s.setHead(line)
	return true
}

func (s *mergeStream) setHead(line string) {
	if hasTimestampShape([]byte(line)) {
		s.headTS = line[:utils.LogTimestampLen-1]
		s.headBuf = line[utils.LogTimestampLen:]
		return
	}
	s.headTS = ""
	s.headBuf = line
}

func openMergeStream(base, svc string) *mergeStream {
	runs, err := utils.ListServiceRuns(base, svc)
	if err != nil || len(runs) == 0 {
		return nil
	}
	f, err := os.Open(runs[0])
	if err != nil {
		return nil
	}
	return &mergeStream{service: svc, reader: bufio.NewReader(f), f: f}
}

func writeMergedLine(out *bufio.Writer, service, ts, line string) {
	if utils.JSONOutput {
		fmt.Fprintln(out, logJSONLine(service, ts, detectLevel(line), line))
		return
	}
	if ts != "" {
		fmt.Fprintf(out, "%s %s[%s]%s %s\n", ts, art.CyanColor, service, art.WhiteColor, line)
		return
	}
	fmt.Fprintf(out, "%s[%s]%s %s\n", art.CyanColor, service, art.WhiteColor, line)
}

// isLogFileActive returns true when the log file was modified in the last 2s.
func isLogFileActive(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 2*time.Second
}
