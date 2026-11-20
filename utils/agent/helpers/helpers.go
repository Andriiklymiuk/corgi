package helpers

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed *.py
var scripts embed.FS

var descriptions = map[string]string{
	"mine-sessions": "what went wrong in the last sessions: tool errors, repeated failing calls, skill use, where the person stepped in",
	"soundtrack":    "list sound loops, or mix a track from a sound.json (needs numpy, scipy and soundfile)",
}

func Names() []string {
	names := make([]string, 0, len(descriptions))
	for name := range descriptions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func Describe(name string) string { return descriptions[name] }

func Source(name string) ([]byte, error) {
	if _, ok := descriptions[name]; !ok {
		return nil, fmt.Errorf("no helper %q - there is %s", name, strings.Join(Names(), ", "))
	}
	return scripts.ReadFile(strings.ReplaceAll(name, "-", "_") + ".py")
}

func Run(name string, args []string) (int, error) {
	source, err := Source(name)
	if err != nil {
		return 2, err
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		return 1, fmt.Errorf("%s needs python3 on PATH", name)
	}
	dir, err := os.MkdirTemp("", "corgi-helper-")
	if err != nil {
		return 1, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, strings.ReplaceAll(name, "-", "_")+".py")
	if err := os.WriteFile(path, source, 0o600); err != nil {
		return 1, err
	}
	cmd := exec.Command(python, append([]string{path}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}
