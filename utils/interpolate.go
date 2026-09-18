package utils

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var bracedRe = regexp.MustCompile(`(\$?)\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:-)([^}]*))?\}`)

func scanBraced(raw []byte, lookup func(string) (string, bool)) (out []byte, unresolved []string) {
	seen := map[string]bool{}
	out = bracedRe.ReplaceAllFunc(raw, func(m []byte) []byte {
		sub := bracedRe.FindSubmatch(m)
		escaped := len(sub[1]) > 0
		name := string(sub[2])
		hasDefault := len(sub[3]) > 0
		def := string(sub[4])

		if escaped {
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
		return m
	})
	return out, unresolved
}

func Interpolate(raw []byte, lookup func(string) (string, bool)) ([]byte, error) {
	out, unresolved := scanBraced(raw, lookup)
	if len(unresolved) > 0 {
		return nil, fmt.Errorf("%s: ${%s} is not set and has no default", ErrMissingField, unresolved[0])
	}
	return out, nil
}

func InterpolateTolerant(raw []byte, lookup func(string) (string, bool)) (out []byte, unresolved []string) {
	return scanBraced(raw, lookup)
}

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

func EnvThenDotEnv(dotenv map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if v, ok := os.LookupEnv(name); ok {
			return v, true
		}
		v, ok := dotenv[name]
		return v, ok
	}
}
