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
		row, err := envCheckService(svc, all[svc.ServiceName], fileOverride)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func envCheckService(svc Service, resolved []EnvVar, fileOverride string) (EnvCheckRow, error) {
	row := EnvCheckRow{Service: svc.ServiceName}
	if svc.IgnoreEnv {
		row.Skipped = "ignore_env is set"
		return row, nil
	}

	example := exampleEnvFile(svc)
	if example == "" {
		row.Skipped = "no .env-example / .env.example in the service repo"
		return row, nil
	}
	row.Example = displayPath(example)

	exampleKeys, err := envFileKeys(example)
	if err != nil {
		return row, err
	}
	generated := map[string]bool{}
	for _, e := range resolved {
		if e.IsGenerated() {
			generated[e.Key] = true
		}
	}
	missingFrom := func(provided map[string]bool) []string {
		var missing []string
		for key := range exampleKeys {
			if !provided[key] && !generated[key] {
				missing = append(missing, key)
			}
		}
		sort.Strings(missing)
		return missing
	}
	// An absent source is only a finding when the example declares keys corgi
	// does not generate — an example of purely generated keys needs no file.
	absentSource := func(display string) EnvCheckRow {
		if missing := missingFrom(nil); len(missing) > 0 {
			row.Source = display
			row.Missing = missing
			row.SourceAbsent = true
		}
		return row
	}

	var source string
	if fileOverride != "" {
		candidate := filepath.Join(svc.AbsolutePath, fileOverride)
		if !fileExists(candidate) {
			return absentSource(displayPath(candidate)), nil
		}
		source = candidate
	} else {
		// The same resolution corgi run uses, so check and run can never
		// disagree about which file a service's env comes from.
		resolvedSrc := resolveEnvSourceFile(CorgiComposePathDir, svc, "", ActiveTierName, ActiveTierDir)
		if resolvedSrc == "" || sameFile(resolvedSrc, example) {
			// Resolution fell through to the example (or nothing): diffing
			// the example against itself proves nothing.
			if svc.CopyEnvFromFilePath == "" {
				row.Skipped = "no copyEnvFromFilePath — env comes from the example file itself"
				return row, nil
			}
			return absentSource(svc.CopyEnvFromFilePath), nil
		}
		source = resolvedSrc
	}
	row.Source = displayPath(source)

	provided, err := envFileKeys(source)
	if err != nil {
		return row, err
	}
	row.Missing = missingFrom(provided)
	return row, nil
}

// envFileKeys errors instead of returning an empty set: an unreadable file
// silently treated as "declares nothing" would pass exactly the check it
// should fail.
func envFileKeys(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("env check: %w", err)
	}
	keys := map[string]bool{}
	for _, e := range parseChunkInOrder(string(data), "") {
		// Shell-sourceable files write `export KEY=value`; the key is KEY.
		keys[strings.TrimPrefix(e.Key, "export ")] = true
	}
	return keys, nil
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

// EnvCheckStats counts the rows that were actually checked and whether any
// finding exists. Zero checked rows count as a finding — a vacuous pass
// would read as coverage.
func EnvCheckStats(rows []EnvCheckRow) (checked int, findings bool) {
	for _, row := range rows {
		if row.Skipped != "" {
			continue
		}
		checked++
		if !row.OK() {
			findings = true
		}
	}
	if checked == 0 {
		findings = true
	}
	return checked, findings
}

// EnvCheckNothingChecked is the shared explanation for a vacuous run, used by
// both the human summary and the JSON reason field.
const EnvCheckNothingChecked = "nothing was checked — no service pairs an env source with a committed .env-example / .env.example"

// EnvCheckSummary renders the human view and says whether findings exist.
func EnvCheckSummary(rows []EnvCheckRow) (string, bool) {
	var b strings.Builder
	for _, row := range rows {
		switch {
		case row.Skipped != "":
			fmt.Fprintf(&b, "⏭️  %s: %s\n", row.Service, row.Skipped)
		case row.SourceAbsent:
			fmt.Fprintf(&b, "❌ %s: env source %s does not exist, and %s declares keys corgi does not generate:\n",
				row.Service, row.Source, row.Example)
			for _, key := range row.Missing {
				fmt.Fprintf(&b, "     %s\n", key)
			}
		case len(row.Missing) > 0:
			fmt.Fprintf(&b, "❌ %s: %s is missing keys that %s declares and corgi does not generate:\n",
				row.Service, row.Source, row.Example)
			for _, key := range row.Missing {
				fmt.Fprintf(&b, "     %s\n", key)
			}
		case row.Source == "":
			fmt.Fprintf(&b, "✅ %s: %s declares only keys corgi generates — no env file needed\n",
				row.Service, row.Example)
		default:
			fmt.Fprintf(&b, "✅ %s: %s covers %s\n", row.Service, row.Source, row.Example)
		}
	}
	checked, findings := EnvCheckStats(rows)
	if checked == 0 {
		b.WriteString(EnvCheckNothingChecked + "\n")
	}
	return b.String(), findings
}
