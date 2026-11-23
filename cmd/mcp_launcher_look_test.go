package cmd

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var cssToken = regexp.MustCompile(`--([a-z-]+):(#[0-9a-f]{6})`)

func launcherPalettes(t *testing.T) (light, dark map[string]string) {
	t.Helper()
	head, rest, ok := strings.Cut(launcherPageHTML, "@media (prefers-color-scheme:dark)")
	if !ok {
		t.Fatal("the page has a light and a dark palette")
	}
	read := func(css string) map[string]string {
		tokens := map[string]string{}
		for _, m := range cssToken.FindAllStringSubmatch(css, -1) {
			if _, seen := tokens[m[1]]; !seen {
				tokens[m[1]] = m[2]
			}
		}
		return tokens
	}
	return read(head), read(rest[:strings.Index(rest, "}}")])
}

func channels(hex string) [3]float64 {
	var c [3]float64
	for i := range c {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c[i] = float64(v)
	}
	return c
}

func contrast(a, b string) float64 {
	luminance := func(hex string) float64 {
		var l float64
		for i, c := range channels(hex) {
			c /= 255
			if c <= 0.03928 {
				c /= 12.92
			} else {
				c = math.Pow((c+0.055)/1.055, 2.4)
			}
			l += c * [3]float64{0.2126, 0.7152, 0.0722}[i]
		}
		return l
	}
	x, y := luminance(a), luminance(b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}

func TestLauncherTextIsReadableInLightAndDark(t *testing.T) {
	light, dark := launcherPalettes(t)
	if len(light) < 12 || len(light) != len(dark) {
		t.Fatalf("both palettes name the same colours: %d and %d", len(light), len(dark))
	}
	for name, p := range map[string]map[string]string{"light": light, "dark": dark} {
		for _, ground := range []string{"bg", "raise", "hover"} {
			for _, text := range []string{"text", "dim", "accent", "ok", "warn", "bad"} {
				if got := contrast(p[text], p[ground]); got < 4.5 {
					t.Errorf("%s: %s on %s is %.2f:1", name, text, ground, got)
				}
			}
			c := channels(p[ground])
			if math.Max(c[0], math.Max(c[1], c[2]))-math.Min(c[0], math.Min(c[1], c[2])) > 12 {
				t.Errorf("%s: %s %s is tinted", name, ground, p[ground])
			}
		}
		if got := contrast(p["on-fill"], p["fill"]); got < 4.5 {
			t.Errorf("%s: a filled button is %.2f:1", name, got)
		}
	}
}

func TestLauncherBoardFitsAndHidesNothing(t *testing.T) {
	style, _, _ := strings.Cut(launcherPageHTML, "</style>")
	for what, want := range map[string]string{
		"the board takes the whole width":       "[data-pane=kanban]{max-width:none}",
		"a column may shrink to fit":            "minmax(10rem,1fr)",
		"cards stack on a narrow screen":        "@media (max-width:959px){.kb{display:block",
		"long text can be opened":               "'Show all'",
		"a routine's row opens its report":      "'Read the report'",
		"a failed load says so and can retry":   "This page cannot reach corgi",
		"a redraw keeps a half-typed comment":   "if (inUse(box)) return;",
		"the inbox shows the rest on request":   "wholeInbox = true",
		"the sheet closes with Escape":          "e.key === 'Escape'",
		"the full ref stays one hover away":     "el.title = ref",
		"the phone keeps its larger targets":    "@media (pointer:coarse){:root{--tap:10px}}",
		"empty columns say what belongs there":  "Nothing is stuck",
		"the board loads without the sessions":  "loadInbox();\n    loadKanban();\n    try {",
		"the light theme colours the phone bar": `media="(prefers-color-scheme:light)"`,
	} {
		if !strings.Contains(launcherPageHTML, want) {
			t.Errorf("%s: missing %q", what, want)
		}
	}
	for what, not := range map[string]string{
		"gradients":                    "linear-gradient",
		"tracked capitals":             "text-transform:uppercase",
		"a page as narrow as a phone":  "max-width:34rem",
		"text cut with no way to open": "max-height:6.5rem",
	} {
		if strings.Contains(style, not) {
			t.Errorf("%s: %q is back", what, not)
		}
	}
}
