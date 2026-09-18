package cmd

import (
	"strings"
	"testing"
)

func TestParsePlannerAnswer(t *testing.T) {
	text := "Here is the plan:\n```json\n{\"summary\":\"limiter first\",\"tasks\":[{\"title\":\"add limiter\",\"body\":\"…\"},{\"title\":\"wire it\",\"body\":\"…\",\"after\":[1,2,9]},{\"title\":\"docs\",\"body\":\"…\",\"after\":[1]}]}\n```"
	ans, err := parsePlannerAnswer(text, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ans.Summary != "limiter first" || len(ans.Tasks) != 2 {
		t.Fatalf("got %+v", ans)
	}
	if got := ans.Tasks[1].After; len(got) != 1 || got[0] != 1 {
		t.Fatalf("after should keep only 1 (2 is itself, 9 is nobody), got %v", got)
	}
	if _, err := parsePlannerAnswer("no json here", 6); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("prose is refused, got %v", err)
	}
	if _, err := parsePlannerAnswer(`{"tasks":[]}`, 6); err == nil {
		t.Fatal("no tasks is refused")
	}
}

func TestRepoPictureIsSmall(t *testing.T) {
	dir := t.TempDir()
	pic := repoPicture(dir)
	if !strings.Contains(pic, "Repository root") || len(pic) > 12000 {
		t.Fatalf("picture: %d bytes", len(pic))
	}
}
