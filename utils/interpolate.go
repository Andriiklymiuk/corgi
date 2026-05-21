package utils

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// bracedRe matches ${VAR} and ${VAR:-default}. An optional leading `$` is
// captured (group 1) so $${...} can be detected as an escape. The name must be
// a simple identifier ([A-Za-z_][A-Za-z0-9_]*) immediately followed by either
// `}` or a `:-default` — so a dotted form like ${producer.VAR} (cross-service
// ref) does NOT match and is left completely untouched for the cross-service
// resolver to own.
//
//	group 1: leading "$" (present only for the $${...} escape)
//	group 2: VAR name
//	group 3: ":-" sentinel — non-empty when a :-default is supplied
//	group 4: default value
var bracedRe = regexp.MustCompile(`(\$?)\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:-)([^}]*))?\}`)

// scanBraced walks raw, expanding ${VAR} / ${VAR:-default} via lookup and
// handling the $${X} escape. Unset vars with no default are left UNTOUCHED in
// the output and their names are appended (de-duplicated) to unresolved. It is
// the shared core for both Interpolate (strict) and InterpolateTolerant.
func scanBraced(raw []byte, lookup func(string) (string, bool)) (out []byte, unresolved []string) {
	seen := map[string]bool{}
	out = bracedRe.ReplaceAllFunc(raw, func(m []byte) []byte {
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
		if !seen[name] {
			seen[name] = true
			unresolved = append(unresolved, name)
		}
		// Leave the ${VAR} token untouched so later resolvers can handle it.
		return m
	})
	return out, unresolved
}

// Interpolate expands ${VAR} and ${VAR:-default} in raw, using lookup for
// values. $${X} escapes to the literal ${X} (no lookup). Only braced forms are
// expanded — bare $VAR is left untouched (too risky for shell snippets). An
// unset var with no default returns an error naming the var (fail-loud). Use
// InterpolateTolerant for the non-breaking load path.
func Interpolate(raw []byte, lookup func(string) (string, bool)) ([]byte, error) {
	out, unresolved := scanBraced(raw, lookup)
	if len(unresolved) > 0 {
		return nil, fmt.Errorf("%s: ${%s} is not set and has no default", ErrMissingField, unresolved[0])
	}
	return out, nil
}

// InterpolateTolerant is the non-breaking variant of Interpolate: instead of
// erroring on an unset var with no default, it leaves the ${VAR} token
// untouched and returns the list of such names (de-duplicated, in order of
// first appearance) so the caller can warn. Dotted forms like ${producer.VAR}
// never match the braced pattern and are neither touched nor reported.
func InterpolateTolerant(raw []byte, lookup func(string) (string, bool)) (out []byte, unresolved []string) {
	return scanBraced(raw, lookup)
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
