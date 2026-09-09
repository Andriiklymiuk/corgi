package cmd

import "testing"

func TestTagFromReleaseLocation(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/Andriiklymiuk/corgi/releases/tag/v1.21.50":     "v1.21.50",
		"https://github.com/Andriiklymiuk/corgi/releases/tag/v1.21.50?x=1": "v1.21.50",
		"https://github.com/Andriiklymiuk/corgi/releases":                  "",
		"": "",
	} {
		if got := tagFromReleaseLocation(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
