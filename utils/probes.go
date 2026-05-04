package utils

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// IsPortListening returns true if something is listening on localhost:<port>.
// Used both by `corgi doctor` (expects false) and `corgi status` (expects true).
func IsPortListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// PortOwner returns a short description of the process listening on the given
// port, or empty string if nothing is listening or the platform can't answer.
// macOS/Linux only — uses lsof, which isn't on Windows.
func PortOwner(port int) string {
	out, err := exec.Command(
		"lsof", "-nP",
		fmt.Sprintf("-iTCP:%d", port),
		"-sTCP:LISTEN",
	).Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return ""
	}
	// Each line: COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
	var owners []string
	seen := map[string]bool{}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		tag := fmt.Sprintf("%s(pid=%s)", fields[0], fields[1])
		if !seen[tag] {
			seen[tag] = true
			owners = append(owners, tag)
		}
	}
	return strings.Join(owners, " ")
}

// IsHTTPHealthy returns true if a GET on the URL returns any non-5xx
// response within the timeout. Any transport error or 5xx counts as unhealthy.
// reason is "" on HTTP response (regardless of code); on transport error it is
// one of "timeout", "connection refused", "no response".
func IsHTTPHealthy(rawURL string, timeout time.Duration) (healthy bool, code int, reason string) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return false, 0, classifyHTTPErr(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500, resp.StatusCode, ""
}

func classifyHTTPErr(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return "timeout"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection refused"
	}
	return "no response"
}

// IsDockerRunning returns true if the docker daemon responds to `docker info`.
func IsDockerRunning() bool {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}
