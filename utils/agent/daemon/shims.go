package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const forgeCallLimit = 120

var forgeTools = []string{"gh", "glab"}

var shimOnce sync.Map

func forgeShims(agentDir string) string {
	if runtime.GOOS == "windows" || agentDir == "" {
		return ""
	}
	if dir, ok := shimOnce.Load(agentDir); ok {
		return dir.(string)
	}
	dir := writeForgeShims(agentDir)
	shimOnce.Store(agentDir, dir)
	return dir
}

func writeForgeShims(agentDir string) string {
	perl, err := exec.LookPath("perl")
	if err != nil {
		return ""
	}
	dir := filepath.Join(agentDir, "shims")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	wrote := false
	for _, tool := range forgeTools {
		real, err := exec.LookPath(tool)
		if err != nil || strings.HasPrefix(real, dir) {
			continue
		}
		script := "#!/bin/sh\n" +
			"exec " + strconv.Quote(perl) + " -e 'alarm shift; exec @ARGV or die \"cannot run $ARGV[0]\\n\"' " +
			strconv.Itoa(forgeCallLimit) + " " + strconv.Quote(real) + " \"$@\"\n"
		if os.WriteFile(filepath.Join(dir, tool), []byte(script), 0o700) == nil {
			wrote = true
		}
	}
	if !wrote {
		return ""
	}
	return dir
}

func withForgeShims(agentDir string, env []string) []string {
	dir := forgeShims(agentDir)
	if dir == "" {
		return env
	}
	return append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
