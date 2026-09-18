package utils

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func readGitLabTemplate(t *testing.T) (spec map[string]any, jobs map[string]any, raw string) {
	t.Helper()
	data, err := os.ReadFile("../gitlab/corgi.yml")
	if err != nil {
		t.Fatal(err)
	}
	raw = string(data)

	decoder := yaml.NewDecoder(strings.NewReader(raw))
	var first, second map[string]any
	if err := decoder.Decode(&first); err != nil {
		t.Fatalf("the spec document does not parse: %v", err)
	}
	if err := decoder.Decode(&second); err != nil {
		t.Fatalf("the job document does not parse: %v", err)
	}

	specDoc, ok := first["spec"].(map[string]any)
	if !ok {
		t.Fatal("the first document must be a spec: header, or include: inputs are rejected")
	}
	inputs, ok := specDoc["inputs"].(map[string]any)
	if !ok {
		t.Fatal("spec: must declare inputs")
	}
	return inputs, second, raw
}

func TestGitLabTemplateDeclaresTheDocumentedInputs(t *testing.T) {
	inputs, _, _ := readGitLabTemplate(t)
	for _, want := range []string{
		"corgi_version", "working_directory", "branch", "stage",
		"runner_tags", "wait_timeout", "job_timeout", "artifacts_dir", "allow_container",
	} {
		if _, ok := inputs[want]; !ok {
			t.Errorf("missing input %q", want)
		}
	}
}

func TestGitLabTemplateKeepsItsJobNames(t *testing.T) {
	_, jobs, _ := readGitLabTemplate(t)
	for _, want := range []string{".corgi-setup", ".corgi-stack-e2e"} {
		if _, ok := jobs[want]; !ok {
			t.Errorf("missing job template %q", want)
		}
	}
}

func TestGitLabTemplateNeverRunsInAContainer(t *testing.T) {
	_, jobs, _ := readGitLabTemplate(t)
	for name, job := range jobs {
		fields, ok := job.(map[string]any)
		if !ok {
			continue
		}
		for _, forbidden := range []string{"image", "services"} {
			if _, bad := fields[forbidden]; bad {
				t.Errorf("%s sets %s:, which containerises the job", name, forbidden)
			}
		}
	}
}

func TestGitLabTemplateRestoresPathForTheLogDump(t *testing.T) {
	_, jobs, _ := readGitLabTemplate(t)
	job, ok := jobs[".corgi-stack-e2e"].(map[string]any)
	if !ok {
		t.Fatal(".corgi-stack-e2e must be a mapping")
	}
	after, ok := job["after_script"].([]any)
	if !ok || len(after) == 0 {
		t.Fatal("expected an after_script that dumps logs")
	}
	joined := ""
	for _, line := range after {
		joined += toString(line) + "\n"
	}
	if !strings.Contains(joined, "export PATH=") {
		t.Errorf("after_script must put corgi back on PATH:\n%s", joined)
	}
	if !strings.Contains(joined, "corgi logs --dump") {
		t.Errorf("after_script must dump the logs:\n%s", joined)
	}
}

func TestGitLabTemplateMatchesTheGeneratedCacheName(t *testing.T) {
	_, _, raw := readGitLabTemplate(t)
	if !strings.Contains(raw, gitlabCacheTemplate) {
		t.Errorf("the template must document extending %q", gitlabCacheTemplate)
	}
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}
