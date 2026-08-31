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
	skipHistory := hadRun && isFinishedRun(preexisting)

	var current *os.File
	var reader *bufio.Reader
	openedPath := ""
	defer func() {
		if current != nil {
			_ = current.Close()
		}
	}()

	for {
		if time.Now().After(deadline) {
			return "", false, nil
		}

		newest, found := newestRun(logsDir, wait.Service)
		if found && newest != openedPath {
			if current != nil {
				_ = current.Close()
			}
			file, openErr := os.Open(newest)
			if openErr != nil {
				time.Sleep(poll)
				continue
			}
			if newest == preexisting && skipHistory && wait.Since.IsZero() {
				if _, seekErr := file.Seek(0, io.SeekEnd); seekErr != nil {
					_ = file.Close()
					return "", false, seekErr
				}
			}
			current, reader, openedPath = file, bufio.NewReader(file), newest
		}
		if reader == nil {
			time.Sleep(poll)
			continue
		}

		line, readErr := readBoundedLine(reader)
		if line != "" {
			content := trimLogPrefix(strings.TrimRight(line, "\n"))
			if waitLineAllowed(content, line, wait.Since) && re.MatchString(content) {
				return content, true, nil
			}
			continue
		}
		if readErr != nil && readErr != io.EOF {
			return "", false, readErr
		}
		time.Sleep(poll)
	}
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
