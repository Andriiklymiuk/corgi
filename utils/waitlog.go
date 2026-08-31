package utils

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// WaitForLogLine tails a service's newest log until a line matches, or the
// timeout passes. It waits for the log file itself to appear, so it can be
// called before the service has written anything.
func WaitForLogLine(logsDir, service, pattern string, timeout time.Duration) (string, bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", false, fmt.Errorf("pattern is not a valid regexp: %v", err)
	}
	deadline := time.Now().Add(timeout)

	path, err := awaitLogFile(logsDir, service, deadline)
	if err != nil {
		return "", false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			content := trimLogPrefix(strings.TrimRight(line, "\n"))
			if re.MatchString(content) {
				return content, true, nil
			}
		}
		if readErr != nil && readErr != io.EOF {
			return "", false, readErr
		}
		if readErr == io.EOF {
			if time.Now().After(deadline) {
				return "", false, nil
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func awaitLogFile(logsDir, service string, deadline time.Time) (string, error) {
	for {
		runs, err := ListServiceRuns(logsDir, service)
		if err == nil && len(runs) > 0 {
			return runs[0], nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("no log file appeared for %s", service)
		}
		time.Sleep(250 * time.Millisecond)
	}
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
