package utils

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func lsofPath() string {
	if p, err := exec.LookPath("lsof"); err == nil {
		return p
	}
	for _, p := range []string{"/usr/sbin/lsof", "/usr/bin/lsof", "/sbin/lsof", "/bin/lsof"} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func WaitPortFree(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !IsPortListening(port) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func IsPortListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func PortOwner(port int) string {
	if owners, ok := nativeListeners(port); ok {
		var tags []string
		for _, o := range owners {
			tags = append(tags, fmt.Sprintf("%s(pid=%d)", o.Name, o.PID))
		}
		return strings.Join(tags, " ")
	}
	lsof := lsofPath()
	if lsof == "" {
		return ""
	}
	out, err := exec.Command(
		lsof, "-nP",
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

func IsDockerRunning() bool {
	cmd := exec.Command("docker", "info")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}
