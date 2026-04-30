package tunnel

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// LoadEnvFile parses KEY=VALUE lines (`#` comments + blanks ignored).
// Quotes around values are stripped. Errors propagate.
func LoadEnvFile(path string) (map[string]string, error) {
	out := map[string]string{}
	if path == "" {
		return out, nil
	}
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
		v := strings.TrimSpace(line[eq+1:])
		v = strings.Trim(v, `"'`)
		out[k] = v
	}
	return out, sc.Err()
}

// Substitute replaces ${VAR} and $VAR refs in s. Lookup order: shell env,
// then fileEnv. Missing keys recorded in `missing` and left as the
// original ref string (caller decides strictness).
func Substitute(s string, fileEnv map[string]string, missing *[]string) string {
	return envRefRe.ReplaceAllStringFunc(s, func(match string) string {
		var key string
		if strings.HasPrefix(match, "${") {
			key = match[2 : len(match)-1]
		} else {
			key = match[1:]
		}
		if v, ok := os.LookupEnv(key); ok {
			return v
		}
		if v, ok := fileEnv[key]; ok {
			return v
		}
		if missing != nil {
			*missing = append(*missing, key)
		}
		return match
	})
}

// MissingError formats a strict-mode error listing unresolved env refs.
func MissingError(field string, missing []string) error {
	return fmt.Errorf("env vars not set for %s: %s", field, strings.Join(dedup(missing), ", "))
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
