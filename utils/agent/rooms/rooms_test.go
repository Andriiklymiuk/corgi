package rooms

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var at = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func post(t *testing.T, dir string, m Message) Message {
	t.Helper()
	got, err := Post(dir, m, at)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRoomNamesReadLikePaths(t *testing.T) {
	for in, want := range map[string]string{"security": "security", "/Security/Findings/": "security/findings", "a/b/c/d": "a/b/c/d"} {
		if got, err := CleanName(in); err != nil || got != want {
			t.Errorf("%q = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "../etc", "a/../b", "a b", "a/b/c/d/e", "-x", "x_y", strings.Repeat("a", 41)} {
		if _, err := CleanName(bad); err == nil {
			t.Errorf("%q is accepted", bad)
		}
	}
}

func TestMessagesThreadsAndReadingOnFromAnId(t *testing.T) {
	dir := t.TempDir()
	first := post(t, dir, Message{Room: "security/findings", By: "scanner", Text: "SQL built from input", Labels: map[string]string{"kind": "bug"}})
	post(t, dir, Message{Room: "security/findings", By: "scanner", Text: "a second one"})
	reply := post(t, dir, Message{Room: "security/findings", By: "fixer", Text: "fixed in #88", Thread: first.ID})
	if first.ID != "m1" || reply.ID != "m3" || !reply.At.Equal(at) {
		t.Fatalf("ids = %s %s", first.ID, reply.ID)
	}
	if _, err := Post(dir, Message{Room: "security/findings", By: "x", Text: "y", Thread: reply.ID}, at); err == nil {
		t.Fatal("a reply under a reply is accepted: a thread is named by its first message")
	}
	if _, err := Post(dir, Message{Room: "security/findings", By: "x", Text: "y", Thread: "m99"}, at); err == nil {
		t.Fatal("a reply to nothing is accepted")
	}
	all, _ := Read(dir, Query{Room: "security/findings"})
	if len(all) != 3 {
		t.Fatalf("all = %d", len(all))
	}
	since, _ := Read(dir, Query{Room: "security/findings", After: "m1"})
	if len(since) != 2 || since[0].ID != "m2" {
		t.Fatalf("since = %+v", since)
	}
	thread, _ := Read(dir, Query{Room: "security/findings", Thread: "m1"})
	if len(thread) != 2 || thread[1].By != "fixer" {
		t.Fatalf("thread = %+v", thread)
	}
	bugs, _ := Read(dir, Query{Room: "security/findings", Label: map[string]string{"kind": "bug"}})
	if len(bugs) != 1 || bugs[0].ID != "m1" {
		t.Fatalf("bugs = %+v", bugs)
	}
	last, _ := Read(dir, Query{Room: "security/findings", Limit: 1})
	if len(last) != 1 || last[0].ID != "m3" {
		t.Fatalf("the newest is not the one kept: %+v", last)
	}
	empty, err := Read(dir, Query{Room: "nobody"})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("an empty room = %v %v", empty, err)
	}
	list := List(dir)
	if len(list) != 1 || list[0].Name != "security/findings" || list[0].Messages != 3 || list[0].LastBy != "fixer" {
		t.Fatalf("list = %+v", list)
	}
}

func TestAMessageNeedsAnAuthorAndWordsOrData(t *testing.T) {
	dir := t.TempDir()
	for _, m := range []Message{
		{Room: "r", Text: "no author"},
		{Room: "r", By: "a"},
		{Room: "r", By: "a", Text: strings.Repeat("x", maxText+1)},
		{Room: "r", By: "a", Data: json.RawMessage(`{not json`)},
		{Room: "bad name", By: "a", Text: "x"},
	} {
		if _, err := Post(dir, m, at); err == nil {
			t.Errorf("%+v is accepted", m)
		}
	}
	if _, err := Post(dir, Message{Room: "r", By: "a", Data: json.RawMessage(`{"ok":true}`)}, at); err != nil {
		t.Fatalf("data alone is refused: %v", err)
	}
}

func TestPostsAtOnceGetTheirOwnIds(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Post(dir, Message{Room: "busy", By: "a", Text: "hi"}, at); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	all, _ := Read(dir, Query{Room: "busy"})
	seen := map[string]bool{}
	for _, m := range all {
		seen[m.ID] = true
	}
	if len(all) != 20 || len(seen) != 20 {
		t.Fatalf("messages = %d ids = %d", len(all), len(seen))
	}
}

const findingShape = `{"type":"object","required":["repo","severity"],"onlyListed":true,"properties":{
"repo":{"type":"string","maxLength":100},
"severity":{"type":"string","enum":["low","medium","high"]},
"line":{"type":"integer","minimum":1},
"tags":{"type":"array","items":{"type":"string"}},
"fixed":{"type":"boolean"}}}`

func TestDataIsCheckedAgainstItsShape(t *testing.T) {
	dir := t.TempDir()
	s, err := SaveShape(dir, "Finding@1", []byte(findingShape), "one security finding")
	if err != nil || s.Name != "finding@1" || s.About != "one security finding" {
		t.Fatalf("shape = %+v err = %v", s, err)
	}
	if _, err := SaveShape(dir, "finding@1", []byte(findingShape), ""); err == nil {
		t.Fatal("a saved shape was changed in place")
	}
	for _, bad := range []string{"finding", "finding@x", "Finding Two@1"} {
		if _, err := SaveShape(dir, bad, []byte(findingShape), ""); err == nil {
			t.Errorf("shape name %q is accepted", bad)
		}
	}
	if _, err := SaveShape(dir, "plain@1", []byte(`{"type":"string"}`), ""); err == nil {
		t.Fatal("a shape that is not an object is accepted")
	}
	good := `{"repo":"acme/api","severity":"high","line":12,"tags":["sql"],"fixed":false}`
	if _, err := Post(dir, Message{Room: "security", By: "scanner", Shape: "finding@1", Data: json.RawMessage(good)}, at); err != nil {
		t.Fatalf("good data is refused: %v", err)
	}
	cases := map[string]string{
		`{"repo":"acme/api"}`:                                          "severity is missing",
		`{"repo":"acme/api","severity":"huge"}`:                        "not one of",
		`{"repo":"acme/api","severity":"low","line":0}`:                "line is out of range",
		`{"repo":"acme/api","severity":"low","line":1.5}`:              "whole number",
		`{"repo":"acme/api","severity":"low","tags":[1]}`:              "tags[0] is not text",
		`{"repo":"acme/api","severity":"low","fixed":"no"}`:            "not true or false",
		`{"repo":"acme/api","severity":"low","extra":1}`:               "extra is not in the shape",
		`{"repo":"` + strings.Repeat("a", 101) + `","severity":"low"}`: "longer than 100",
		`[1]`: "not an object",
	}
	for data, want := range cases {
		_, err := Post(dir, Message{Room: "security", By: "scanner", Shape: "finding@1", Data: json.RawMessage(data)}, at)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", data, err, want)
		}
	}
	if _, err := Post(dir, Message{Room: "security", By: "a", Shape: "missing@1", Data: json.RawMessage(good)}, at); err == nil {
		t.Fatal("an unknown shape is accepted")
	}
	if _, err := Post(dir, Message{Room: "security", By: "a", Text: "x", Shape: "finding@1"}, at); err == nil {
		t.Fatal("a shape with no data is accepted")
	}
	if got := Shapes(dir); len(got) != 1 || got[0].Name != "finding@1" {
		t.Fatalf("shapes = %+v", got)
	}
}

func TestGrantsGoByTheLongestPathAndANameBeatsEveryone(t *testing.T) {
	dir := t.TempDir()
	add := func(who, spec string) Grant {
		t.Helper()
		g, err := AddGrant(dir, who, spec, at)
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	add(Everyone, "all=read")
	ci := add("ci-runner", "security/=read,post")
	add("ci-runner", "security/secrets=none")
	list := Grants(dir)
	checks := []struct {
		who, room, action string
		want              bool
	}{
		{"ci-runner", "security/findings", CanPost, true},
		{"ci-runner", "security", CanRead, true},
		{"ci-runner", "security/secrets", CanRead, false},
		{"ci-runner", "security/secrets/keys", CanRead, false},
		{"ci-runner", "securityish", CanPost, false},
		{"ci-runner", "deploys", CanRead, true},
		{"ci-runner", "deploys", CanPost, false},
		{"phone", "deploys", CanRead, true},
		{"phone", "deploys", CanPost, false},
	}
	for _, c := range checks {
		if v := Explain(list, c.who, c.room, c.action); v.Allowed != c.want || v.Because == "" {
			t.Errorf("%s %s %s = %+v", c.who, c.action, c.room, v)
		}
	}
	if v := Explain(list, "ci-runner", "security/findings", CanPost); v.Grant != ci.ID || !strings.Contains(v.Because, "security/") {
		t.Errorf("the reason does not name the grant: %+v", v)
	}
	if v := Explain(nil, "x", "a", CanRead); v.Allowed || !strings.Contains(v.Because, "no grant") {
		t.Errorf("no grants = %+v", v)
	}
	if !Covered(list, "anyone") || Covered([]Grant{{Who: "a"}}, "b") {
		t.Error("covered")
	}
	again := add("ci-runner", "security/=read")
	if len(Grants(dir)) != 3 || again.ID == ci.ID {
		t.Fatalf("a second grant on the same path did not replace the first: %+v", Grants(dir))
	}
	if _, err := RevokeGrant(dir, again.ID); err != nil || len(Grants(dir)) != 2 {
		t.Fatalf("revoke: %v %d", err, len(Grants(dir)))
	}
	if _, err := RevokeGrant(dir, "g99"); err == nil {
		t.Fatal("a missing grant was revoked")
	}
	for _, bad := range []string{"security", "security/=write", "../x=read"} {
		if _, err := AddGrant(dir, "a", bad, at); err == nil {
			t.Errorf("grant %q is accepted", bad)
		}
	}
	if _, err := AddGrant(dir, " ", "all=read", at); err == nil {
		t.Error("a grant for nobody is accepted")
	}
	if info, _ := os.Stat(filepath.Join(Dir(dir), "grants.json")); info.Mode().Perm() != 0o600 {
		t.Errorf("grants file mode = %v", info.Mode().Perm())
	}
}

func TestLabelsParse(t *testing.T) {
	got, err := ParseLabels([]string{"kind=bug", " pr = 88 "})
	if err != nil || got["kind"] != "bug" || got["pr"] != "88" {
		t.Fatalf("labels = %v %v", got, err)
	}
	if _, err := ParseLabels([]string{"nokey"}); err == nil {
		t.Fatal("a label without = is accepted")
	}
	if got, _ := ParseLabels(nil); got != nil {
		t.Fatal("no labels is not nil")
	}
}
