package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"andriiklymiuk/corgi/utils"
)

var secretKeyRe = regexp.MustCompile(`(?i)(password|secret|token|api_?key|_pwd|passwd)`)
var urlCredRe = regexp.MustCompile(`^([a-z][a-z0-9+.-]*://[^:/@\s]+:)([^@/\s]+)(@.*)$`)

// maskStars renders a fixed-width mask that never leaks the secret's length.
func maskStars(s string) string {
	if len(s) <= 4 {
		return "***"
	}
	return s[:2] + "****" + s[len(s)-2:]
}

// maskSecret redacts secret-looking values for the human view. Keys matching
// secretKeyRe are fully masked; connection-string values have only their
// password segment masked.
func maskSecret(key, val string) string {
	if m := urlCredRe.FindStringSubmatch(val); m != nil {
		return m[1] + "****" + m[3]
	}
	if secretKeyRe.MatchString(key) {
		return maskStars(val)
	}
	return val
}

// renderPlain returns the human view: KEY=VALUE with an aligned `# source`
// comment, grouped under `# <service>` headers. Secrets masked unless reveal.
func renderPlain(all map[string][]utils.EnvVar, order []string, reveal bool) string {
	var b strings.Builder
	for i, name := range order {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "# %s\n", name)
		// width for alignment
		w := 0
		for _, e := range all[name] {
			if l := len(e.Key) + len(e.Value) + 1; l > w {
				w = l
			}
		}
		for _, e := range all[name] {
			val := e.Value
			if !reveal {
				val = maskSecret(e.Key, e.Value)
			}
			line := e.Key + "=" + val
			fmt.Fprintf(&b, "%-*s  # %s\n", w, line, e.Source)
		}
	}
	return b.String()
}
