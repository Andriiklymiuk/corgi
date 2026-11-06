package watch

import "testing"

func TestACorrectionOnTopOfAWrongCommentIsRefused(t *testing.T) {
	for _, text := range []string{
		"Correction: ignore my earlier comment, it searches both",
		"correction - it does",
		"Please disregard my last comment",
		"ignore my comments above",
		"my previous comment was wrong",
	} {
		if CorrectionRefusal(text) == nil {
			t.Errorf("went through: %q", text)
		}
	}
	for _, text := range []string{
		"Search looks at both lines, tested on staging",
		"the correction factor is 1.2",
		"I ignored the old flag in my last commit",
	} {
		if err := CorrectionRefusal(text); err != nil {
			t.Errorf("%q: %v", text, err)
		}
	}
}
