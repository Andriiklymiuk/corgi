package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type EnvCheckRow struct {
	Service      string   `json:"service"`
	Example      string   `json:"example,omitempty"`
	Source       string   `json:"source,omitempty"`
	Missing      []string `json:"missing,omitempty"`
	Skipped      string   `json:"skipped,omitempty"`
	SourceAbsent bool     `json:"sourceAbsent,omitempty"`
}

func (r EnvCheckRow) OK() bool {
	return !r.SourceAbsent && len(r.Missing) == 0
}

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

type envKeyCheck struct {
	exampleKeys map[string]bool
	generated   map[string]bool
}

func (c envKeyCheck) missingFrom(provided map[string]bool) []string {
	var missing []string
	for key := range c.exampleKeys {
		if !provided[key] && !c.generated[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func (c envKeyCheck) absentSource(row EnvCheckRow, display string) EnvCheckRow {
	if missing := c.missingFrom(nil); len(missing) > 0 {
		row.Source = display
		row.Missing = missing
		row.SourceAbsent = true
	}
	return row
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
	check := envKeyCheck{exampleKeys: exampleKeys, generated: map[string]bool{}}
	for _, e := range resolved {
		if e.IsGenerated() {
			check.generated[e.Key] = true
		}
	}
	source, done := envCheckSource(svc, example, fileOverride, check, &row)
	if done {
		return row, nil
	}
	row.Source = displayPath(source)
	provided, err := envFileKeys(source)
	if err != nil {
		return row, err
	}
	row.Missing = check.missingFrom(provided)
	return row, nil
}

func envCheckSource(svc Service, example, fileOverride string, check envKeyCheck, row *EnvCheckRow) (string, bool) {
	if fileOverride != "" {
		candidate := filepath.Join(svc.AbsolutePath, fileOverride)
		if !fileExists(candidate) {
			*row = check.absentSource(*row, displayPath(candidate))
			return "", true
		}
		return candidate, false
	}
	resolvedSrc := resolveEnvSourceFile(CorgiComposePathDir, svc, "", ActiveTierName, ActiveTierDir)
	if resolvedSrc != "" && !sameFile(resolvedSrc, example) {
		return resolvedSrc, false
	}
	if svc.CopyEnvFromFilePath == "" {
		row.Skipped = "no copyEnvFromFilePath - env comes from the example file itself"
		return "", true
	}
	*row = check.absentSource(*row, svc.CopyEnvFromFilePath)
	return "", true
}

func envFileKeys(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("env check: %w", err)
	}
	keys := map[string]bool{}
	for _, e := range parseChunkInOrder(string(data), "") {
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

func displayPath(path string) string {
	rel, err := filepath.Rel(CorgiComposePathDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

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

const EnvCheckNothingChecked = "nothing was checked - no service pairs an env source with a committed .env-example / .env.example"

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
			fmt.Fprintf(&b, "✅ %s: %s declares only keys corgi generates - no env file needed\n",
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
