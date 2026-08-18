package utils

import "regexp"

var (
	redactAssignRe  = regexp.MustCompile(`(?i)([A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|anon[_-]?key|service[_-]?role|credential)[A-Za-z0-9_.-]*)(\s*[:=]\s*)("?)([^"\s]{4,})`)
	redactURLCredRe = regexp.MustCompile(`([a-z][a-z0-9+.-]*://[^/\s:@]*:)([^@/\s]{3,})(@)`)
	redactAWSKeyRe  = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	redactJWTRe     = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+`)
	redactPEMRe     = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----.*`)
)

const redactMask = "****"

func RedactLine(s string) string {
	s = redactPEMRe.ReplaceAllString(s, "-----BEGIN PRIVATE KEY----- "+redactMask)
	s = redactAssignRe.ReplaceAllString(s, "${1}${2}${3}"+redactMask)
	s = redactURLCredRe.ReplaceAllString(s, "${1}"+redactMask+"${3}")
	s = redactAWSKeyRe.ReplaceAllString(s, redactMask)
	s = redactJWTRe.ReplaceAllString(s, redactMask)
	return s
}

func RedactBytes(b []byte) []byte {
	s := string(b)
	out := RedactLine(s)
	if out == s {
		return b
	}
	return []byte(out)
}
