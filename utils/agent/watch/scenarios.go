package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Scenarios are the end-to-end stories a change is judged against. They live in
// the agent directory, outside every repository, so the session that builds the
// change does not read its own exam.

const scenarioResultsFile = "results.jsonl"

var scenarioName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Scenario struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type ScenarioResult struct {
	Scenario  string    `json:"scenario"`
	Satisfied bool      `json:"satisfied"`
	Why       string    `json:"why,omitempty"`
	Ref       string    `json:"ref,omitempty"`
	At        time.Time `json:"at"`
}

func ScenarioDir(agentDir, workspace string) string {
	return filepath.Join(agentDir, "scenarios", workspace)
}

func AddScenario(agentDir, workspace, name, text string) error {
	if !scenarioName.MatchString(name) {
		return fmt.Errorf("%q is not a scenario name: lower-case letters, digits and dashes", name)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("scenario %s is empty: say what a person does and what they should see", name)
	}
	dir := ScenarioDir(agentDir, workspace)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name+".md"), []byte(strings.TrimSpace(text)+"\n"), 0o600)
}

func RemoveScenario(agentDir, workspace, name string) error {
	if !scenarioName.MatchString(name) {
		return fmt.Errorf("%q is not a scenario name", name)
	}
	return os.Remove(filepath.Join(ScenarioDir(agentDir, workspace), name+".md"))
}

func LoadScenarios(agentDir, workspace string) []Scenario {
	files, err := filepath.Glob(filepath.Join(ScenarioDir(agentDir, workspace), "*.md"))
	if err != nil {
		return nil
	}
	sort.Strings(files)
	out := make([]Scenario, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		out = append(out, Scenario{Name: strings.TrimSuffix(filepath.Base(f), ".md"), Text: string(data)})
	}
	return out
}

// ParseVerdict reads the judge's last JSON line. Anything else is not a pass:
// a judge that wandered off must not count as satisfied.
func ParseVerdict(text string) (satisfied bool, why string) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.Trim(strings.TrimSpace(lines[i]), "`")
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var v struct {
			Satisfied *bool  `json:"satisfied"`
			Why       string `json:"why"`
		}
		if json.Unmarshal([]byte(line), &v) == nil && v.Satisfied != nil {
			return *v.Satisfied, v.Why
		}
	}
	return false, "the judge gave no verdict"
}

// Satisfaction is the share of runs that left the person satisfied, in percent.
func Satisfaction(results []ScenarioResult) int {
	if len(results) == 0 {
		return 0
	}
	ok := 0
	for _, r := range results {
		if r.Satisfied {
			ok++
		}
	}
	return ok * 100 / len(results)
}

func AppendScenarioResults(agentDir, workspace string, results []ScenarioResult) error {
	dir := ScenarioDir(agentDir, workspace)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, scenarioResultsFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range results {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}
