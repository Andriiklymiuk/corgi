package utils

import (
	"strings"
	"testing"
)

// The changed surface is what a reviewer reads first: exported symbols,
// routes, contracts, migrations — with removals and signature changes
// marked breaking, and test files ignored.
func TestChangedSurfaceReadsTheDiff(t *testing.T) {
	rd := RepoDiff{Service: "api", Files: []FileDiff{
		{Path: "limits/limits.go", Patch: "@@\n-func Check(id string) error {\n+func Check(id string, window time.Duration) error {\n+func Reset(id string) {\n-func Old() {}\n+type Window struct {\n+func helper() {}\n"},
		{Path: "limits/limits_test.go", Patch: "+func TestCheck(t *testing.T) {}\n"},
		{Path: "http/routes.go", Patch: "+\trouter.Post(\"/limits/reset\", h.reset)\n"},
		{Path: "db/migrations/0042_limits.sql", New: true, Patch: "+ALTER TABLE users ADD COLUMN limit_window int;\n"},
		{Path: "api/openapi.yaml", Patch: "+  /limits/reset:\n+    post:\n-  /limits/old:\n"},
		{Path: "go.mod", Patch: "+\tgolang.org/x/time v0.5.0\n"},
		{Path: "README.md", Patch: "+something\n"},
	}}
	s := SurfaceOf(rd)
	got := map[string]SurfaceChange{}
	for _, c := range s.Changes {
		got[c.Kind+" "+c.Name] = c
	}
	if c := got["symbol Check"]; c.Op != "changed" || !c.Breaking {
		t.Errorf("a signature change is breaking: %+v", c)
	}
	if c := got["symbol Reset"]; c.Op != "added" || c.Breaking {
		t.Errorf("an addition is not: %+v", c)
	}
	if c := got["symbol Old"]; c.Op != "removed" || !c.Breaking {
		t.Errorf("a removal is: %+v", c)
	}
	if _, ok := got["symbol Window"]; !ok {
		t.Error("an exported type counts")
	}
	if _, ok := got["symbol helper"]; ok {
		t.Error("an unexported function is not surface")
	}
	if _, ok := got["symbol TestCheck"]; ok {
		t.Error("tests are not surface")
	}
	if c := got["route POST /limits/reset"]; c.Op != "added" {
		t.Errorf("a route: %+v", c)
	}
	if c := got["migration 0042_limits.sql"]; !c.Breaking {
		t.Errorf("an ALTER is breaking: %+v", c)
	}
	if c := got["contract /limits/old"]; c.Op != "removed" || !c.Breaking {
		t.Errorf("a removed path in the contract: %+v", c)
	}
	if _, ok := got["config go.mod"]; !ok {
		t.Error("a dependency file is surface")
	}
	if s.Changes[0].Kind != "migration" {
		t.Errorf("breaking and migrations first: %+v", s.Changes[0])
	}
	md := SurfaceMarkdown([]RepoSurface{s})
	if !strings.HasPrefix(md, "## Changed surface") || !strings.Contains(md, "⚠ breaking") || !strings.Contains(md, "`POST /limits/reset`") {
		t.Fatalf("markdown:\n%s", md)
	}
	if md := SurfaceMarkdown([]RepoSurface{{Service: "web"}}); !strings.Contains(md, "nothing public changed") {
		t.Fatal("an empty surface says so")
	}
}
