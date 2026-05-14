package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"bufio"
	"container/heap"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var logsServiceFlag string
var logsPruneFlag bool
var logsAllFlag bool
var logsIdleFlag time.Duration

var logsCmd = &cobra.Command{
	Use:     "logs",
	Short:   "Browse and follow persisted service logs",
	Aliases: []string{"log"},
	Long: `Browse logs captured by corgi run --logs.

Without flags: interactive picker — choose a service then a run, and
the log is streamed to stdout (follows new writes like tail -f).

Examples:
  corgi logs                      # interactive picker
  corgi logs --service api        # jump straight to run picker for "api"
  corgi logs --all                # merge the newest run of every service into one stream
  corgi logs --idle 0             # tail forever (until Ctrl-C)
  corgi logs --prune              # delete all .logs/ directories`,
	Run: runLogs,
}

func init() {
	rootCmd.AddCommand(logsCmd)
	logsCmd.Flags().StringVar(&logsServiceFlag, "service", "", "Service name to show logs for (skips service picker)")
	logsCmd.Flags().BoolVar(&logsPruneFlag, "prune", false, "Delete all captured log files (corgi_services/.logs/)")
	logsCmd.Flags().BoolVar(&logsAllFlag, "all", false, "Merge the newest run of every service into one timestamp-sorted stream")
	logsCmd.Flags().DurationVar(&logsIdleFlag, "idle", 30*time.Second, "Exit after this much dead-air on the file (set 0 to tail forever)")
}

func runLogs(cmd *cobra.Command, _ []string) {
	base := logsBase()

	if logsPruneFlag {
		pruneAllLogs(base)
		return
	}

	if logsAllFlag {
		if err := followAllLogs(base); err != nil {
			fmt.Println(err)
		}
		return
	}

	serviceName := logsServiceFlag
	if serviceName == "" {
		var err error
		serviceName, err = pickLogService(base)
		if err != nil {
			fmt.Println(err)
			return
		}
	}

	runFile, err := pickLogRun(base, serviceName)
	if err != nil {
		fmt.Println(err)
		return
	}

	followLog(runFile)
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

func pickLogService(base string) (string, error) {
	services, err := utils.ListLoggedServices(base)
	if err != nil || len(services) == 0 {
		return "", fmt.Errorf("no log directories found under %s/.logs/\nRe-run with: corgi run --logs", base)
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

// followLog tails the file like `tail -f`. Exits after `--idle` seconds
// of no new writes (or never, when --idle=0).
func followLog(path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("cannot open %s: %v\n", path, err)
		return
	}
	defer f.Close()

	fmt.Printf("%s📄 %s (Ctrl-C to exit)%s\n\n", art.CyanColor, path, art.WhiteColor)

	reader := bufio.NewReader(f)
	var idleSince time.Time
	stripPrefix := looksLikeStampedLog(path)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if stripPrefix && len(line) >= utils.LogTimestampLen {
				fmt.Print(line[utils.LogTimestampLen:])
			} else {
				fmt.Print(line)
			}
			idleSince = time.Time{}
		}
		if err == nil {
			continue
		}
		if err != io.EOF {
			fmt.Printf("read error: %v\n", err)
			return
		}
		if idleSince.IsZero() {
			idleSince = time.Now()
		}
		idleExceeded := logsIdleFlag > 0 && time.Since(idleSince) > logsIdleFlag
		if !isLogFileActive(path) || idleExceeded {
			fmt.Printf("\n%s— end of log —%s\n", art.YellowColor, art.WhiteColor)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
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

// hasTimestampShape checks `YYYY-MM-DDTHH:MM:SS.sssZ ` (25 bytes).
func hasTimestampShape(b []byte) bool {
	if len(b) < utils.LogTimestampLen {
		return false
	}
	isDigit := func(c byte) bool { return c >= '0' && c <= '9' }
	if !isDigit(b[0]) || !isDigit(b[1]) || !isDigit(b[2]) || !isDigit(b[3]) {
		return false
	}
	if b[4] != '-' || b[7] != '-' || b[10] != 'T' {
		return false
	}
	if !isDigit(b[5]) || !isDigit(b[6]) || !isDigit(b[8]) || !isDigit(b[9]) {
		return false
	}
	if !isDigit(b[11]) || !isDigit(b[12]) || b[13] != ':' || b[16] != ':' {
		return false
	}
	if !isDigit(b[14]) || !isDigit(b[15]) || !isDigit(b[17]) || !isDigit(b[18]) {
		return false
	}
	if b[19] != '.' || !isDigit(b[20]) || !isDigit(b[21]) || !isDigit(b[22]) {
		return false
	}
	return b[23] == 'Z' && b[24] == ' '
}

// mergeStream is one input to the k-way merge — a service's log file
// scanned line by line, with the current head buffered for heap compare.
type mergeStream struct {
	service string
	scanner *bufio.Scanner
	f       *os.File
	headTS  string
	headBuf string
	eof     bool
}

func (s *mergeStream) advance() bool {
	if s.eof {
		return false
	}
	if !s.scanner.Scan() {
		s.eof = true
		s.f.Close()
		return false
	}
	line := s.scanner.Text()
	if hasTimestampShape([]byte(line)) {
		s.headTS = line[:utils.LogTimestampLen-1]
		s.headBuf = line[utils.LogTimestampLen:]
	} else {
		s.headTS = ""
		s.headBuf = line
	}
	return true
}

type mergeHeap []*mergeStream

func (h mergeHeap) Len() int { return len(h) }
func (h mergeHeap) Less(i, j int) bool {
	if h[i].headTS == h[j].headTS {
		return h[i].service < h[j].service
	}
	return h[i].headTS < h[j].headTS
}
func (h mergeHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *mergeHeap) Push(x interface{}) { *h = append(*h, x.(*mergeStream)) }
func (h *mergeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// followAllLogs merges the newest run of every logged service into one
// timestamp-sorted stream. K-way merge → memory is O(num services), not
// O(total log bytes), so big projects don't OOM the CLI.
func followAllLogs(base string) error {
	services, err := utils.ListLoggedServices(base)
	if err != nil || len(services) == 0 {
		return fmt.Errorf("no log directories found under %s/.logs/\nRe-run with: corgi run --logs", base)
	}

	h := &mergeHeap{}
	heap.Init(h)
	for _, svc := range services {
		runs, err := utils.ListServiceRuns(base, svc)
		if err != nil || len(runs) == 0 {
			continue
		}
		f, err := os.Open(runs[0])
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		s := &mergeStream{service: svc, scanner: sc, f: f}
		if s.advance() {
			heap.Push(h, s)
		} else {
			f.Close()
		}
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for h.Len() > 0 {
		s := heap.Pop(h).(*mergeStream)
		if s.headTS != "" {
			fmt.Fprintf(out, "%s %s[%s]%s %s\n", s.headTS, art.CyanColor, s.service, art.WhiteColor, s.headBuf)
		} else {
			fmt.Fprintf(out, "%s[%s]%s %s\n", art.CyanColor, s.service, art.WhiteColor, s.headBuf)
		}
		if s.advance() {
			heap.Push(h, s)
		}
	}
	return nil
}

// isLogFileActive returns true when the log file was modified in the last 2s.
func isLogFileActive(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 2*time.Second
}
