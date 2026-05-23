package cmd

import (
	"regexp"
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
