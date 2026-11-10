package utils

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxWaitLineBytes = 1 << 20

type LogWait struct {
	Service string
	Pattern string
	Since   time.Time
	Timeout time.Duration
	Poll    time.Duration
}

type logFollower struct {
	logsDir     string
	wait        LogWait
	preexisting string
	skipHistory bool
	current     *os.File
	reader      *bufio.Reader
	openedPath  string
}

func (f *logFollower) close() {
	if f.current != nil {
		_ = f.current.Close()
	}
}

func (f *logFollower) follow() error {
	newest, found := newestRun(f.logsDir, f.wait.Service)
	if !found || newest == f.openedPath {
		return nil
	}
	f.close()
	f.current, f.reader = nil, nil
	file, err := os.Open(newest)
	if err != nil {
		return nil
	}
	if newest == f.preexisting && f.skipHistory && f.wait.Since.IsZero() {
		if _, err := file.Seek(0, io.SeekEnd); err != nil {
			_ = file.Close()
			return err
		}
	}
	f.current, f.reader, f.openedPath = file, bufio.NewReader(file), newest
	return nil
}

func (f *logFollower) step(re *regexp.Regexp) (content string, found, idle bool, err error) {
	if err := f.follow(); err != nil {
		return "", false, false, err
	}
	if f.reader == nil {
		return "", false, true, nil
	}
	line, readErr := readBoundedLine(f.reader)
	if line != "" {
		content = trimLogPrefix(strings.TrimRight(line, "\n"))
		return content, waitLineAllowed(content, line, f.wait.Since) && re.MatchString(content), false, nil
	}
	if readErr != nil && readErr != io.EOF {
		return "", false, false, readErr
	}
	return "", false, true, nil
}

func WaitForLogLine(logsDir string, wait LogWait) (string, bool, error) {
	re, err := regexp.Compile(wait.Pattern)
	if err != nil {
		return "", false, fmt.Errorf("pattern is not a valid regexp: %v", err)
	}
	poll := wait.Poll
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	deadline := time.Now().Add(wait.Timeout)
	preexisting, hadRun := newestRun(logsDir, wait.Service)
	f := &logFollower{logsDir: logsDir, wait: wait, preexisting: preexisting, skipHistory: hadRun && isFinishedRun(preexisting)}
	defer f.close()
	for !time.Now().After(deadline) {
		content, found, idle, err := f.step(re)
		if err != nil {
			return "", false, err
		}
		if found {
			return content, true, nil
		}
		if idle {
			time.Sleep(poll)
		}
	}
	return "", false, nil
}

func waitLineAllowed(content, raw string, since time.Time) bool {
	if since.IsZero() {
		return true
	}
	if len(raw) < LogTimestampLen {
		return false
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(raw[:LogTimestampLen-1]))
	return err == nil && !at.Before(since)
}

func readBoundedLine(reader *bufio.Reader) (string, error) {
	var builder strings.Builder
	for {
		chunk, err := reader.ReadString('\n')
		if builder.Len()+len(chunk) > maxWaitLineBytes {
			builder.WriteString(chunk[:maxWaitLineBytes-builder.Len()])
			return builder.String(), nil
		}
		builder.WriteString(chunk)
		if err != nil {
			if builder.Len() > 0 && err == io.EOF && strings.HasSuffix(builder.String(), "\n") {
				return builder.String(), nil
			}
			return "", err
		}
		return builder.String(), nil
	}
}

func isFinishedRun(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".ok.log") || strings.HasSuffix(base, ".crashed.log")
}

func newestRun(logsDir, service string) (string, bool) {
	runs, err := ListServiceRuns(logsDir, service)
	if err != nil || len(runs) == 0 {
		return "", false
	}
	return runs[0], true
}

func trimLogPrefix(line string) string {
	if len(line) < LogTimestampLen {
		return line
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(line[:LogTimestampLen-1])); err != nil {
		return line
	}
	return line[LogTimestampLen:]
}
