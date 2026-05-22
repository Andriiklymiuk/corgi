package utils

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// bracedRe matches ${VAR} and ${VAR:-default}. The name must be a simple
// identifier, so a dotted form like ${producer.VAR} (cross-service ref) does NOT
// match and is left for the cross-service resolver. Groups: 1=leading "$" of the
// $${...} escape, 2=name, 3=":-" sentinel, 4=default.
var bracedRe = regexp.MustCompile(`(\$?)\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:-)([^}]*))?\}`)

// scanBraced walks raw, expanding ${VAR} / ${VAR:-default} via lookup and
// handling the $${X} escape. Unset vars with no default are left untouched and
// their names appended (de-duplicated) to unresolved. Shared core for
// Interpolate (strict) and InterpolateTolerant.
func scanBraced(raw []byte, lookup func(string) (string, bool)) (out []byte, unresolved []string) {
	seen := map[string]bool{}
	out = bracedRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		sub := bracedRe.FindSubmatch(m)
		escaped := len(sub[1]) > 0
		name := string(sub[2])
		hasDefault := len(sub[3]) > 0
		def := string(sub[4])

		if escaped {
			return m[1:] // $${X} -> literal ${X}
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
		return m // leave untouched for later resolvers
	})
	return out, unresolved
}

// Interpolate expands ${VAR} / ${VAR:-default} via lookup, erroring on an unset
// var with no default. Bare $VAR is left untouched (too risky for shell snippets).
func Interpolate(raw []byte, lookup func(string) (string, bool)) ([]byte, error) {
	out, unresolved := scanBraced(raw, lookup)
	if len(unresolved) > 0 {
		return nil, fmt.Errorf("%s: ${%s} is not set and has no default", ErrMissingField, unresolved[0])
	}
	return out, nil
}

// InterpolateTolerant is the non-breaking variant: instead of erroring on an
// unset var, it leaves the token untouched and returns the unresolved names.
func InterpolateTolerant(raw []byte, lookup func(string) (string, bool)) (out []byte, unresolved []string) {
	return scanBraced(raw, lookup)
}

// LoadDotEnv parses a minimal KEY=value .env file into a map. Blank lines and
// `#` comments are ignored. A missing file yields an empty map (no error).
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
