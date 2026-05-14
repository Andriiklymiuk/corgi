package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	logsDirName    = ".logs"
	maxLogFileSize = 50 * 1024 * 1024 // 50 MB
	defaultKeepN   = 10
	logTimeFormat  = "2006-01-02T15:04:05.000Z07:00"
)

// LogStatus is how a service exited; used to rename the log file with a
// meaningful suffix on close.
type LogStatus int

const (
	LogStatusUnknown LogStatus = iota
	LogStatusOK
	LogStatusCrashed
)

// logWriter writes a service's stdout+stderr to one file, stamping each
// line with an RFC3339 UTC timestamp and enforcing a per-file size cap.
// Partial lines (no trailing newline) are buffered until the newline
// arrives so a stamp is emitted once per logical line, not per chunk.
type logWriter struct {
	mu          sync.Mutex
	f           *os.File
	path        string
	status      LogStatus
	written     int64
	closed      bool
	capWarned   bool
	pending     []byte // bytes received without a trailing newline yet
	pendingTime time.Time
}

// Path returns the underlying file path. Used by tests and rename-on-close.
func (lw *logWriter) Path() string { return lw.path }

// SetStatus records the exit status. The file is renamed with a matching
// suffix on Close.
func (lw *logWriter) SetStatus(s LogStatus) {
	lw.mu.Lock()
	lw.status = s
	lw.mu.Unlock()
}

// CurrentStatus returns the last status set on this writer.
func (lw *logWriter) CurrentStatus() LogStatus {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.status
}

func nowTimestamp() string { return time.Now().UTC().Format(logTimeFormat) }

func (lw *logWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.closed {
		return len(p), nil
	}
	if lw.written >= maxLogFileSize {
		if !lw.capWarned {
			lw.capWarned = true
			_, _ = lw.f.WriteString(nowTimestamp() + " [corgi] log file reached " + fmt.Sprintf("%d", maxLogFileSize) + " bytes, further output dropped\n")
		}
		return len(p), nil
	}

	consumed := len(p)
	rest := p
	for len(rest) > 0 {
		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			if len(lw.pending) == 0 {
				lw.pendingTime = time.Now().UTC()
			}
			lw.pending = append(lw.pending, rest...)
			break
		}
		line := rest[:nl+1]
		rest = rest[nl+1:]

		ts := lw.pendingTime
		if len(lw.pending) == 0 {
			ts = time.Now().UTC()
		}
		stamp := ts.Format(logTimeFormat) + " "

		n, err := lw.f.WriteString(stamp)
		lw.written += int64(n)
		if err != nil {
			return consumed, err
		}
		if len(lw.pending) > 0 {
			n, err = lw.f.Write(normalizeWindowsLineEnding(lw.pending))
			lw.written += int64(n)
			lw.pending = lw.pending[:0]
			lw.pendingTime = time.Time{}
			if err != nil {
				return consumed, err
			}
		}
		n, err = lw.f.Write(normalizeWindowsLineEnding(line))
		lw.written += int64(n)
		if err != nil {
			return consumed, err
		}
	}
	return consumed, nil
}

// normalizeWindowsLineEnding turns a trailing Windows newline ("\r\n")
// into a Unix newline ("\n") so log files have one consistent terminator.
func normalizeWindowsLineEnding(buf []byte) []byte {
	n := len(buf)
	if n >= 2 && buf[n-2] == '\r' && buf[n-1] == '\n' {
		out := make([]byte, n-1)
		copy(out, buf[:n-2])
		out[n-2] = '\n'
		return out
	}
	return buf
}

// flushPendingLocked writes any unterminated buffered bytes with a final
// newline. Caller must hold lw.mu.
func (lw *logWriter) flushPendingLocked() {
	if len(lw.pending) == 0 {
		return
	}
	ts := lw.pendingTime
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	stamp := ts.Format(logTimeFormat) + " "
	_, _ = lw.f.WriteString(stamp)
	_, _ = lw.f.Write(lw.pending)
	_, _ = lw.f.Write([]byte{'\n'})
	lw.pending = lw.pending[:0]
	lw.pendingTime = time.Time{}
}

func (lw *logWriter) Close() error {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.closed {
		return nil
	}
	lw.flushPendingLocked()
	lw.closed = true
	err := lw.f.Close()
	lw.renameByStatusLocked()
	return err
}

// renameByStatusLocked tags the file with .ok or .crashed for the picker.
// Caller must hold lw.mu. Rename errors leave the file as-is.
func (lw *logWriter) renameByStatusLocked() {
	suffix := ""
	switch lw.status {
	case LogStatusOK:
		suffix = ".ok"
	case LogStatusCrashed:
		suffix = ".crashed"
	default:
		return
	}
	ext := filepath.Ext(lw.path)
	base := strings.TrimSuffix(lw.path, ext)
	newPath := base + suffix + ext
	if err := os.Rename(lw.path, newPath); err == nil {
		lw.path = newPath
	}
}

// LogTimestampLen is the timestamp + space prefix length (UTC `Z` form).
// `corgi logs` strips this off for single-file display.
const LogTimestampLen = len("2006-01-02T15:04:05.000Z") + 1

// OpenLogWriter creates corgi_services/.logs/<service>/<timestamp>.log.
// Returns (nil, nil) on empty service name so callers don't need to guard.
func OpenLogWriter(corgiServicesPath, serviceName string) (io.WriteCloser, error) {
	if serviceName == "" {
		return nil, nil
	}
	dir := filepath.Join(corgiServicesPath, logsDirName, sanitizeName(serviceName))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("logs: mkdir %s: %w", dir, err)
	}
	ts := time.Now().Format("2006-01-02T15-04-05")
	path := filepath.Join(dir, ts+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("logs: create %s: %w", path, err)
	}
	return &logWriter{f: f, path: path}, nil
}

// PruneLogs deletes the oldest log files for serviceName, keeping at most
// keepN. If keepN <= 0, defaultKeepN is used.
func PruneLogs(corgiServicesPath, serviceName string, keepN int) {
	if keepN <= 0 {
		keepN = defaultKeepN
	}
	dir := filepath.Join(corgiServicesPath, logsDirName, sanitizeName(serviceName))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	// Sort oldest first (lexicographic on ISO timestamp filenames = chronological).
	sort.Strings(files)
	if len(files) <= keepN {
		return
	}
	for _, f := range files[:len(files)-keepN] {
		os.Remove(f)
	}
}

// EnsureLogsGitignore adds the .logs/ entry to corgi_services/.gitignore,
// creating the file if it does not exist. Idempotent.
func EnsureLogsGitignore(corgiServicesPath string) {
	path := filepath.Join(corgiServicesPath, ".gitignore")
	const entry = ".logs/"

	data, _ := os.ReadFile(path)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == entry {
			return // already present
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		fmt.Fprintln(f)
	}
	fmt.Fprintln(f, entry)
}

// ListLoggedServices returns service names that have log directories under
// corgi_services/.logs/.
func ListLoggedServices(corgiServicesPath string) ([]string, error) {
	dir := filepath.Join(corgiServicesPath, logsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// ListServiceRuns returns log file paths for the given service, sorted
// newest-first by the embedded ISO timestamp prefix (ignoring any
// .ok / .crashed status suffix so a renamed older run does not sort
// above an in-progress newer run with the same prefix).
func ListServiceRuns(corgiServicesPath, serviceName string) ([]string, error) {
	dir := filepath.Join(corgiServicesPath, logsDirName, sanitizeName(serviceName))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return runSortKey(files[i]) > runSortKey(files[j])
	})
	return files, nil
}

// runSortKey extracts the timestamp portion of a log filename for ordering,
// stripping the .ok / .crashed suffix and .log extension. Example:
//
//	2024-01-01T10-00-00.crashed.log → 2024-01-01T10-00-00
func runSortKey(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".log")
	name = strings.TrimSuffix(name, ".crashed")
	name = strings.TrimSuffix(name, ".ok")
	return name
}

// sanitizeName makes a service name safe to use as a directory component.
func sanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, name)
}
