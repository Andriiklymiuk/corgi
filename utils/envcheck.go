package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EnvCheckRow is one service's drift verdict: which example keys the resolved
// env (or --file override) neither provides nor corgi generates.
type EnvCheckRow struct {
	Service string `json:"service"`
	// Example is the reference file the service repo commits
	// (.env-example / .env.example), relative to the compose dir.
	Example string `json:"example,omitempty"`
	// Source is the file whose keys were checked against the example,
	// relative to the compose dir. Empty when it does not exist.
	Source string `json:"source,omitempty"`
	// Missing are keys the example declares that the source file does not
	// provide and corgi does not generate (db/service deps, port, literals).
	Missing []string `json:"missing,omitempty"`
	// Skipped explains why this service was not checked.
	Skipped string `json:"skipped,omitempty"`
	// SourceAbsent: the service declares an env source but the file is not
	// there, so a run would fall back to the example's placeholder values.
	SourceAbsent bool `json:"sourceAbsent,omitempty"`
}

// OK reports whether this row is free of findings.
func (r EnvCheckRow) OK() bool {
	return !r.SourceAbsent && len(r.Missing) == 0
}

// EnvCheckAll diffs each service's env source against the example file its
// repo commits, subtracting everything corgi generates itself. fileOverride
// checks <service repo>/<fileOverride> instead of the resolved source —
// useful for a committed CI env file before anything copies it into place.
func EnvCheckAll(corgi *CorgiCompose, fileOverride string) ([]EnvCheckRow, error) {
	all, err := ResolveAllEnv(corgi)
	if err != nil {
		return nil, err
	}

	rows := make([]EnvCheckRow, 0, len(corgi.Services))
	for _, svc := range sortedServices(corgi) {
		rows = append(rows, envCheckService(svc, all[svc.ServiceName], fileOverride))
	}
	return rows, nil
}

func envCheckService(svc Service, resolved []EnvVar, fileOverride string) EnvCheckRow {
	row := EnvCheckRow{Service: svc.ServiceName}
	if svc.IgnoreEnv {
		row.Skipped = "ignore_env is set"
		return row
	}

	example := exampleEnvFile(svc)
	if example == "" {
		row.Skipped = "no .env-example / .env.example in the service repo"
		return row
	}
	row.Example = displayPath(example)

	var source string
	if fileOverride != "" {
		candidate := filepath.Join(svc.AbsolutePath, fileOverride)
		if !fileExists(candidate) {
			row.Source = displayPath(candidate)
			row.SourceAbsent = true
			return row
		}
		source = candidate
	} else {
		rel := resolveCopyEnvPath(svc, "")
		if rel == "" {
			row.Skipped = "no copyEnvFromFilePath — env comes from the example file itself"
			return row
		}
		if ActiveTierName != "" {
			rel = strings.ReplaceAll(rel, "${tier}", ActiveTierName)
		}
		candidate := filepath.Join(CorgiComposePathDir, rel)
		if !fileExists(candidate) {
			row.Source = rel
			row.SourceAbsent = true
			return row
		}
		source = candidate
	}
	if sameFile(source, example) {
		// Diffing the example against itself proves nothing.
		row.Skipped = "env source is the example file itself — nothing to diff"
		return row
	}
	row.Source = displayPath(source)

	provided := envFileKeys(source)
	generated := map[string]bool{}
	for _, e := range resolved {
		// file:* entries come from the copied env file; everything else
		// (db:, service:, self:port, literal) corgi generates at run time.
		if !strings.HasPrefix(e.Source, "file:") {
			generated[e.Key] = true
		}
	}

	for key := range envFileKeys(example) {
		if !provided[key] && !generated[key] {
			row.Missing = append(row.Missing, key)
		}
	}
	sort.Strings(row.Missing)
	return row
}

func exampleEnvFile(svc Service) string {
	for _, name := range []string{".env-example", ".env.example"} {
		candidate := filepath.Join(svc.AbsolutePath, name)
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func envFileKeys(path string) map[string]bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	keys := map[string]bool{}
	for _, e := range parseChunkInOrder(string(data), "") {
		keys[e.Key] = true
	}
	return keys
}

func sameFile(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// displayPath renders a path relative to the compose dir when possible, so
// output stays portable between a laptop and a runner.
func displayPath(path string) string {
	rel, err := filepath.Rel(CorgiComposePathDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// EnvCheckSummary renders the human view and says whether findings exist.
func EnvCheckSummary(rows []EnvCheckRow) (string, bool) {
	var b strings.Builder
	checked, findings := 0, false
	for _, row := range rows {
		switch {
		case row.Skipped != "":
			fmt.Fprintf(&b, "⏭️  %s: %s\n", row.Service, row.Skipped)
		case row.SourceAbsent:
			findings = true
			checked++
			fmt.Fprintf(&b, "❌ %s: env source %s does not exist — a run would fall back to %s's placeholder values\n",
				row.Service, row.Source, row.Example)
		case len(row.Missing) > 0:
			findings = true
			checked++
			fmt.Fprintf(&b, "❌ %s: %s is missing keys that %s declares and corgi does not generate:\n",
				row.Service, row.Source, row.Example)
			for _, key := range row.Missing {
				fmt.Fprintf(&b, "     %s\n", key)
			}
		default:
			checked++
			fmt.Fprintf(&b, "✅ %s: %s covers %s\n", row.Service, row.Source, row.Example)
		}
	}
	if checked == 0 {
		// A vacuous pass would read as coverage; refuse it.
		findings = true
		b.WriteString("nothing was checked — no service pairs an env source with a committed .env-example / .env.example\n")
	}
	return b.String(), findings
}
