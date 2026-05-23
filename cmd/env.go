package cmd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env [service...]",
	Short: "Print services' fully-resolved environment (read-only)",
	Long: `Resolves and prints each service's environment exactly as corgi would
generate it (db deps, service deps, ports, literal environment, copied env files,
and cross-service references), with the source of each variable. Writes nothing.

  corgi env                 # all services, masked, human view
  corgi env api             # one service
  eval $(corgi env api --export)
  corgi env --json`,
	RunE: runEnv,
}

func init() {
	envCmd.Flags().Bool("export", false, "Emit eval-able `export KEY=VALUE` lines (real values)")
	envCmd.Flags().Bool("json", false, "Emit JSON {service:{KEY:{value,source}}} (real values)")
	envCmd.Flags().Bool("reveal", false, "Do not mask secret values in the human view")
	rootCmd.AddCommand(envCmd)
}

func runEnv(cmd *cobra.Command, args []string) error {
	asExport, _ := cmd.Flags().GetBool("export")
	asJSON, _ := cmd.Flags().GetBool("json")
	reveal, _ := cmd.Flags().GetBool("reveal")
	if asExport && asJSON {
		return fmt.Errorf("%s: choose either --export or --json, not both", utils.ErrUsage)
	}

	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		return fmt.Errorf("%s: %v", utils.ErrComposeNotFound, err)
	}

	all, err := utils.ResolveAllEnv(corgi)
	if err != nil {
		return err
	}

	order, err := selectEnvServices(corgi, args, all)
	if err != nil {
		return err
	}

	switch {
	case asJSON:
		out, err := renderJSON(all, order)
		if err != nil {
			return err
		}
		fmt.Println(out)
	case asExport:
		fmt.Print(renderExport(all, order))
	default:
		utils.Info(renderPlain(all, order, reveal))
	}
	return nil
}

// selectEnvServices returns the requested service order, or all services
// sorted, validating any explicitly-named services.
func selectEnvServices(corgi *utils.CorgiCompose, args []string, all map[string][]utils.EnvVar) ([]string, error) {
	if len(args) == 0 {
		order := make([]string, 0, len(all))
		for name := range all {
			order = append(order, name)
		}
		sort.Strings(order)
		return order, nil
	}
	for _, name := range args {
		if _, ok := all[name]; !ok {
			return nil, fmt.Errorf("%s: service %q not found", utils.ErrServiceNotFound, name)
		}
	}
	return args, nil
}

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

// shellSingleQuote wraps s in single quotes, escaping embedded quotes so the
// result is safe for `eval`.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// renderExport emits eval-able `export KEY='VALUE'` lines with real values.
func renderExport(all map[string][]utils.EnvVar, order []string) string {
	var b strings.Builder
	for i, name := range order {
		if len(order) > 1 {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "# --- %s ---\n", name)
		}
		for _, e := range all[name] {
			fmt.Fprintf(&b, "export %s=%s\n", e.Key, shellSingleQuote(e.Value))
		}
	}
	return b.String()
}

// renderJSON emits {service: {KEY: {value, source}}} with REAL values for
// machine consumption.
func renderJSON(all map[string][]utils.EnvVar, order []string) (string, error) {
	type entry struct {
		Value  string `json:"value"`
		Source string `json:"source"`
	}
	doc := map[string]map[string]entry{}
	for _, name := range order {
		m := map[string]entry{}
		for _, e := range all[name] {
			m[e.Key] = entry{Value: e.Value, Source: e.Source}
		}
		doc[name] = m
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
