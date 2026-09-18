package utils

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type CommandInfo struct {
	Install string
	Check   string
	// Hint is shown instead of running Install when the tool needs a
	// package manager corgi will not drive (sudo apt, dnf …).
	Hint string
}

var CommandInstructions = map[string]CommandInfo{
	"brew": {
		Install: `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`,
		Check:   "brew -v",
	},
	"yarn": {
		Install: "brew install yarn",
		Check:   "yarn -v",
	},
	"pnpm": {
		Install: "brew install pnpm",
		Check:   "pnpm -v",
	},
	"go": {
		Install: `brew install go`,
		Check:   "go version",
	},
	"bun": {
		Install: `brew install oven-sh/bun/bun`,
		Check:   "bun -v",
	},
	"deno": {
		Install: `brew install deno`,
		Check:   "deno --version",
	},
	"pg_dump": {
		Install: `brew install libpq`,
		Check:   "psql --version",
	},
}

var linuxCommandInstructions = map[string]CommandInfo{
	"yarn": {Install: "npm install -g yarn", Check: "yarn -v"},
	"pnpm": {Install: "npm install -g pnpm", Check: "pnpm -v"},
	"bun":  {Install: "curl -fsSL https://bun.sh/install | bash", Check: "bun -v"},
	"deno": {Install: "curl -fsSL https://deno.land/install.sh | sh", Check: "deno --version"},
	"go": {
		Check: "go version",
		Hint:  "https://go.dev/dl/ or your package manager (apt install golang-go · dnf install golang · apk add go)",
	},
	"pg_dump": {
		Check: "psql --version",
		Hint:  "apt install postgresql-client · dnf install postgresql · apk add postgresql-client",
	},
}

func commandInstructionsFor(goos, name string) (CommandInfo, bool) {
	if goos == "linux" {
		if info, ok := linuxCommandInstructions[name]; ok {
			return info, true
		}
	}
	info, ok := CommandInstructions[name]
	return info, ok
}

var toolInstallHints = map[string][2]string{
	"gh":    {"brew install gh", "https://github.com/cli/cli/blob/trunk/docs/install_linux.md (apt, dnf or apk)"},
	"glab":  {"brew install glab", "https://gitlab.com/gitlab-org/cli/-/releases (deb, rpm or a binary)"},
	"ngrok": {"brew install ngrok", "https://ngrok.com/download (apt, snap or a binary)"},
	"tmux":  {"brew install tmux", "apt install tmux · dnf install tmux · apk add tmux"},
}

func installHintFor(goos, tool string) string {
	hints, ok := toolInstallHints[tool]
	if !ok {
		return ""
	}
	if goos == "linux" {
		return hints[1]
	}
	return hints[0]
}

// InstallHint says how to get tool on this OS: a brew formula on macOS, a
// package or download on Linux.
func InstallHint(tool string) string {
	return installHintFor(runtime.GOOS, tool)
}

// NotInstalledError is the one-line "X is not installed — how to get it".
func NotInstalledError(tool string) error {
	if hint := InstallHint(tool); hint != "" {
		return fmt.Errorf("%s is not installed — `%s`", tool, hint)
	}
	return fmt.Errorf("%s is not installed", tool)
}

func usesBrew(install string) bool {
	return strings.HasPrefix(install, "brew ")
}

func brewAvailable() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}
