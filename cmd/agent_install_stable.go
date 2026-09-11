package cmd

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"andriiklymiuk/corgi/utils"
)

// On macOS the daemon runs from its own copy of the binary, at a path that
// never changes. launchd resolves /opt/homebrew/bin/corgi to the versioned
// Caskroom directory, and macOS keys "corgi wants access to Documents" on
// that real path — so every `corgi upd` looked like a new tool and asked
// again at the next login. A stable path with a stable signing identity is
// asked once. Linux has no such prompt and keeps the symlink.

func stableDaemonBinary() (string, error) {
	base, err := utils.NativeDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "bin", "corgi"), nil
}

func daemonRunsFromStableCopy() bool {
	return runtime.GOOS == "darwin"
}

// refreshStableDaemonBinary copies the binary at from into the stable path
// unless the copy already matches byte for byte. Returns the stable path.
func refreshStableDaemonBinary(from string) (string, error) {
	dest, err := stableDaemonBinary()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(from); err == nil {
		from = real
	}
	if same, _ := sameFileContent(from, dest); same {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	src, err := os.Open(from)
	if err != nil {
		return "", err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".corgi-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	// A running daemon keeps its old inode open; the rename swaps the path
	// under it without touching the process.
	if err := os.Rename(tmpPath, dest); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return dest, nil
}

func sameFileContent(a, b string) (bool, error) {
	ha, err := fileDigest(a)
	if err != nil {
		return false, err
	}
	hb, err := fileDigest(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ha, hb), nil
}

func fileDigest(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// installedDaemonBinary is the program the login service file names, or ""
// when there is no service file or it cannot be read.
func installedDaemonBinary() string {
	path := loginServicePath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return programFromServiceFile(string(data))
}

// programFromServiceFile pulls the executable out of a launchd plist or a
// systemd unit without a full parser: the first <string> after
// ProgramArguments, or the first word of ExecStart.
func programFromServiceFile(text string) string {
	if i := strings.Index(text, "<key>ProgramArguments</key>"); i >= 0 {
		rest := text[i:]
		if j := strings.Index(rest, "<string>"); j >= 0 {
			rest = rest[j+len("<string>"):]
			if k := strings.Index(rest, "</string>"); k >= 0 {
				return strings.TrimSpace(rest[:k])
			}
		}
		return ""
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "ExecStart=") {
			fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "ExecStart="))
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return ""
}

const checkDaemonBinary = "daemon binary"

// checkDaemonBinaryPath is the doctor's answer to "why does macOS keep
// asking about Documents": the service runs corgi from a path that changes
// with every update, or from a copy older than the corgi you have.
func checkDaemonBinaryPath() agentCheck {
	if !daemonRunsFromStableCopy() || !loginServiceInstalled() {
		return agentCheck{Name: checkDaemonBinary, OK: true, Detail: "not applicable"}
	}
	program := installedDaemonBinary()
	stable, err := stableDaemonBinary()
	if err != nil {
		return agentCheck{Name: checkDaemonBinary, Detail: err.Error()}
	}
	if program == "" {
		return agentCheck{Name: checkDaemonBinary, Detail: "could not read the login service file", Fix: "`corgi agent install` rewrites it"}
	}
	if program != stable {
		return agentCheck{
			Name:   checkDaemonBinary,
			Detail: fmt.Sprintf("the login service runs %s — a path that changes with every update, so macOS asks for Documents access again after each one", program),
			Fix:    "`corgi agent install` moves the daemon to a stable copy; one prompt, then never again",
		}
	}
	self, err := os.Executable()
	if err != nil {
		return agentCheck{Name: checkDaemonBinary, OK: true, Detail: stable}
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	if self == stable {
		return agentCheck{Name: checkDaemonBinary, OK: true, Detail: stable}
	}
	if same, err := sameFileContent(self, stable); err == nil && !same {
		return agentCheck{
			Name:   checkDaemonBinary,
			Detail: "the daemon's copy is not the corgi you are running — it was installed before the last update",
			Fix:    "`corgi agent install` refreshes the copy and restarts the daemon",
		}
	}
	return agentCheck{Name: checkDaemonBinary, OK: true, Detail: stable + " (current)"}
}
