package watch

import "testing"

func TestGapsInNamesTheChecksARunCouldNotRun(t *testing.T) {
	cases := map[string][]Gap{
		"Not done: a DB-backed spec. API CI has no Postgres.":                  {GapNoDatabase},
		"Not run: biome isn't installed in the worktree, so lint did not run.": {GapTestsSkip, GapNoTool},
		"Not checked on a device or simulator.":                                {GapNoDevice},
		"The flag is untested against a live EAS account.":                     {GapNoService},
		"I ran the tests: 18 passed.":                                          {},
	}
	for out, want := range cases {
		got := GapsIn(out)
		if len(got) != len(want) {
			t.Errorf("%q: %v, want %v", out, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q: %v, want %v", out, got, want)
			}
		}
	}
}
