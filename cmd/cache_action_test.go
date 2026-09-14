package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The composite actions cannot loop, so cache/emit-outputs.sh turns one
// `corgi cache paths --json` into the fixed cache-N-* slots both of them
// publish. Driven here with a fake corgi on PATH, so the shell stays honest
// without a runner.
func runEmitOutputs(t *testing.T, planJSON string, env ...string) (outputs, log string) {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "cache", "emit-outputs.sh")

	bin := t.TempDir()
	fake := "#!/bin/sh\necho \"$@\" >> \"$CORGI_FAKE_ARGS\"\n" +
		"if [ \"$1\" = cache ]; then printf '%s' \"$CORGI_FAKE_PLAN\"; exit \"${CORGI_FAKE_EXIT:-0}\"; fi\necho corgi fake\n"
	if err := os.WriteFile(filepath.Join(bin, "corgi"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(t.TempDir(), "outputs")
	if err := os.WriteFile(outFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	argsFile := filepath.Join(t.TempDir(), "args")
	cmd := exec.Command("bash", script)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_OUTPUT="+outFile,
		"CORGI_FAKE_PLAN="+planJSON,
		"CORGI_FAKE_ARGS="+argsFile,
	)
	cmd.Env = append(cmd.Env, env...)
	combined, err := cmd.CombinedOutput()
	log = string(combined)
	if err != nil {
		log += "\nexit: " + err.Error()
	}
	raw, readErr := os.ReadFile(outFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if args, err := os.ReadFile(argsFile); err == nil {
		log += "\ncorgi args: " + string(args)
	}
	return string(raw), log
}

const twoGroupPlan = `{"paths":["api/node_modules","~/.npm",".corgi/corgi_services/.cache"],"key":"corgi-deps-aaaa",` +
	`"groups":[{"id":"node","paths":["api/node_modules","~/.npm"],"key":"corgi-deps-node-1111","pathsText":"api/node_modules\n~/.npm","restorePrefix":"corgi-deps-node-","missingFiles":[]},` +
	`{"id":"markers","paths":[".corgi/corgi_services/.cache"],"key":"corgi-deps-aaaa-markers","pathsText":".corgi/corgi_services/.cache","missingFiles":[]}],` +
	`"missingFiles":[],"complete":true}`

func TestEmitOutputsPublishesEverySlot(t *testing.T) {
	outputs, log := runEmitOutputs(t, twoGroupPlan)
	if strings.Contains(log, "exit:") {
		t.Fatalf("script failed:\n%s", log)
	}
	for _, want := range []string{
		"cache-key=corgi-deps-aaaa\n",
		"cache-1-key=corgi-deps-node-1111\n",
		"cache-1-restore-keys=corgi-deps-node-\n",
		"cache-2-key=corgi-deps-aaaa-markers\n",
		"cache-2-restore-keys=\n",
		"cache-3-key=\n",
		"cache-4-key=\n",
		"cache-overflow=0\n",
		"cache-complete=true\n",
		"api/node_modules\n~/.npm\n",
	} {
		if !strings.Contains(outputs, want) {
			t.Errorf("missing %q in outputs:\n%s", want, outputs)
		}
	}
	if !strings.Contains(outputs, "cache-paths<<") || !strings.Contains(outputs, ".corgi/corgi_services/.cache\n") {
		t.Errorf("cache-paths must carry the full newline-joined list:\n%s", outputs)
	}
	if !strings.Contains(outputs, `cache-groups=[{"id":"node"`) {
		t.Errorf("cache-groups must be the compact groups JSON:\n%s", outputs)
	}
	if strings.Contains(log, "::warning::") {
		t.Errorf("a complete plan must not warn:\n%s", log)
	}
}

// The install action runs before `corgi init`, so its plan is usually hashed
// from files that are not there yet. It still publishes the outputs (older
// workflows read them) but says so, naming the files.
func TestEmitOutputsWarnsOnAnIncompletePlan(t *testing.T) {
	plan := strings.Replace(twoGroupPlan, `"missingFiles":[],"complete":true`, `"missingFiles":["api/package-lock.json"],"complete":false`, 1)
	outputs, log := runEmitOutputs(t, plan)
	if strings.Contains(log, "exit:") {
		t.Fatalf("an incomplete plan must still publish outputs:\n%s", log)
	}
	if !strings.Contains(log, "::warning::") || !strings.Contains(log, "api/package-lock.json") || !strings.Contains(log, "corgi init") {
		t.Errorf("expected a ::warning:: naming the file and the fix:\n%s", log)
	}
	if !strings.Contains(outputs, "cache-complete=false\n") || !strings.Contains(outputs, "cache-1-key=corgi-deps-node-1111\n") {
		t.Errorf("outputs must still be published:\n%s", outputs)
	}
}

// A corgi predating complete/missingFiles must keep working when a workflow
// pins its version.
func TestEmitOutputsTreatsAnOldPlanAsComplete(t *testing.T) {
	plan := strings.Replace(twoGroupPlan, `,"missingFiles":[],"complete":true`, "", 1)
	plan = strings.ReplaceAll(plan, `,"missingFiles":[]`, "")
	outputs, log := runEmitOutputs(t, plan)
	if strings.Contains(log, "exit:") || strings.Contains(log, "::warning::") {
		t.Fatalf("an old plan has nothing to warn about:\n%s", log)
	}
	if !strings.Contains(outputs, "cache-complete=true\n") {
		t.Errorf("expected cache-complete=true:\n%s", outputs)
	}
}

func TestEmitOutputsCountsOverflow(t *testing.T) {
	group := func(id string) string {
		return `{"id":"` + id + `","paths":["x"],"key":"corgi-deps-` + id + `-1","pathsText":"x","restorePrefix":"corgi-deps-` + id + `-","missingFiles":[]}`
	}
	plan := `{"paths":["x"],"key":"corgi-deps-k","groups":[` + group("a") + "," + group("b") + "," + group("c") + "," + group("d") + "," + group("e") + `],"missingFiles":[],"complete":true}`
	outputs, log := runEmitOutputs(t, plan)
	if !strings.Contains(outputs, "cache-overflow=1\n") {
		t.Errorf("five groups in four slots is one overflow:\n%s", outputs)
	}
	if !strings.Contains(log, "::warning::") {
		t.Errorf("overflow must warn:\n%s", log)
	}
}

// The strict variant is what cache/action.yml runs after `corgi init`: a
// plan hashed from nothing fails the step instead of freezing the key.
func TestEmitOutputsStrictPassesTheFlagAndFailsWithCorgi(t *testing.T) {
	outputs, log := runEmitOutputs(t, twoGroupPlan, "CORGI_CACHE_STRICT=true")
	if strings.Contains(log, "exit:") {
		t.Fatalf("strict with a complete plan must succeed:\n%s", log)
	}
	if !strings.Contains(log, "corgi args: cache paths --json --strict") {
		t.Errorf("strict mode must run corgi cache paths --json --strict:\n%s", log)
	}
	if !strings.Contains(outputs, "cache-1-key=corgi-deps-node-1111\n") {
		t.Errorf("outputs must be published:\n%s", outputs)
	}

	plan := strings.Replace(twoGroupPlan, `"missingFiles":[],"complete":true`, `"missingFiles":["api/package-lock.json"],"complete":false`, 1)
	_, log = runEmitOutputs(t, plan, "CORGI_CACHE_STRICT=true", "CORGI_FAKE_EXIT=1")
	if !strings.Contains(log, "exit:") {
		t.Fatalf("strict mode must fail when corgi --strict fails:\n%s", log)
	}
}
