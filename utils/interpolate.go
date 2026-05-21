package utils

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// bracedRe matches ${VAR} and ${VAR:-default}. An optional leading `$` is
// captured (group 1) so $${...} can be detected as an escape. The default
// segment ([^}]*) cannot cross a `}`, so a run of ${a}${b} never collapses
// into one match.
//
//	group 1: leading "$" (present only for the $${...} escape)
//	group 2: VAR name
//	group 3: ":" + "-" sentinel — non-empty when a :-default is supplied
//	group 4: default value
var bracedRe = regexp.MustCompile(`(\$?)\$\{([A-Za-z_][A-Za-z0-9_]*)(:-)?([^}]*)\}`)

// Interpolate expands ${VAR} and ${VAR:-default} in raw, using lookup for
// values. $${X} escapes to the literal ${X} (no lookup). Only braced forms are
// expanded — bare $VAR is left untouched (too risky for shell snippets). An
// unset var with no default returns an error naming the var.
func Interpolate(raw []byte, lookup func(string) (string, bool)) ([]byte, error) {
	var firstErr error
	out := bracedRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		sub := bracedRe.FindSubmatch(m)
		// sub[1] is the leading "$" of an escape ($${...}), empty otherwise.
		escaped := len(sub[1]) > 0
		name := string(sub[2])
		hasDefault := len(sub[3]) > 0 // ":-" present
		def := string(sub[4])

		if escaped {
			// $${X} -> literal ${X}, dropping one $.
			return m[1:]
		}
		if v, ok := lookup(name); ok && v != "" {
			return []byte(v)
		}
		if hasDefault {
			return []byte(def)
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("%s: ${%s} is not set and has no default", ErrMissingField, name)
		}
		return m
	})
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// LoadDotEnv parses a minimal KEY=value .env file into a map. Blank lines and
// `#` comments are ignored; surrounding quotes on the value and whitespace
// around the key are trimmed. A missing file yields an empty map (no error).
func LoadDotEnv(path string) (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.Trim(strings.TrimSpace(line[eq+1:]), `"'`)
		out[k] = v
	}
	return out, sc.Err()
}

// EnvThenDotEnv builds an Interpolate lookup that checks the process env first,
// then the given .env map. Process env takes precedence.
func EnvThenDotEnv(dotenv map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if v, ok := os.LookupEnv(name); ok {
			return v, true
		}
		v, ok := dotenv[name]
		return v, ok
	}
}
