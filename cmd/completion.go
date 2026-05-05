/*
Copyright © 2026 Andrii Klymiuk
*/
package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/tunnel"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// loadComposeForCompletion reads + unmarshals corgi-compose.yml without
// touching globals, printing, or persisting state. Honors -f/--filename.
// Falls back to ./corgi-compose.yml in cwd. Returns nil on any error so
// shell completion stays silent.
func loadComposeForCompletion(cmd *cobra.Command) *utils.CorgiComposeYaml {
	path, _ := cmd.Root().Flags().GetString("filename")
	if path == "" {
		path = utils.CorgiComposeDefaultName
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	var c utils.CorgiComposeYaml
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil
	}
	return &c
}

// splitCsv parses a `--flag=a,b,c` value into the trailing fragment the
// user is typing + the prefix of already-completed entries. Lets us hide
// already-listed names from suggestions and return CSV-aware completions.
//
//	"api,broker,el"  -> prefix="api,broker,", current="el"
//	"api,"           -> prefix="api,",        current=""
//	"api"            -> prefix="",            current="api"
func splitCsv(toComplete string) (prefix, current string, already map[string]struct{}) {
	already = map[string]struct{}{}
	idx := strings.LastIndex(toComplete, ",")
	if idx < 0 {
		return "", toComplete, already
	}
	prefix = toComplete[:idx+1]
	current = toComplete[idx+1:]
	for _, p := range strings.Split(toComplete[:idx], ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			already[p] = struct{}{}
		}
	}
	return prefix, current, already
}

// withCsvPrefix prepends prefix to each suggestion + adds NoSpace so the
// shell doesn't insert a space after a comma.
func withCsvPrefix(prefix string, items []string) ([]string, cobra.ShellCompDirective) {
	if prefix == "" {
		return items, cobra.ShellCompDirectiveNoFileComp
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, prefix+it)
	}
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// completeServices is the catch-all service-name completer (script,
// status, tunnel use this — they don't filter by manualRun).
func completeServices(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	prefix, _, already := splitCsv(toComplete)
	names := make([]string, 0, len(c.Services)+1)
	for name := range c.Services {
		if _, dup := already[name]; dup {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if _, dup := already["none"]; !dup {
		names = append(names, "none")
	}
	return withCsvPrefix(prefix, names)
}

// completeTunnelableServices is `corgi tunnel <args>` specific — only
// services with port: > 0 (no port = nothing to tunnel). manualRun is
// allowed: tunnel cmd respects explicit positional args even for them.
func completeTunnelableServices(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	prefix, _, already := splitCsv(toComplete)
	for _, a := range args {
		for _, name := range strings.Split(a, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				already[name] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(c.Services))
	for name, svc := range c.Services {
		if svc.Port == 0 {
			continue
		}
		if _, dup := already[name]; dup {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return withCsvPrefix(prefix, names)
}

func completeDbServices(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	prefix, _, already := splitCsv(toComplete)
	names := make([]string, 0, len(c.DatabaseServices)+1)
	for name := range c.DatabaseServices {
		if _, dup := already[name]; dup {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if _, dup := already["none"]; !dup {
		names = append(names, "none")
	}
	return withCsvPrefix(prefix, names)
}

func completeScriptNames(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// If user already passed --services, narrow script list to those services.
	// Empty / unset --services = show every script across the whole compose.
	servicesFilter, _ := cmd.Flags().GetStringSlice("services")
	wanted := map[string]struct{}{}
	for _, raw := range servicesFilter {
		for _, name := range strings.Split(raw, ",") {
			name = strings.TrimSpace(name)
			if name != "" && name != "none" {
				wanted[name] = struct{}{}
			}
		}
	}

	prefix, _, already := splitCsv(toComplete)
	seen := map[string]struct{}{}
	var names []string
	for svcName, svc := range c.Services {
		if len(wanted) > 0 {
			if _, ok := wanted[svcName]; !ok {
				continue
			}
		}
		for _, sc := range svc.Scripts {
			if sc.Name == "" {
				continue
			}
			if _, dup := already[sc.Name]; dup {
				continue
			}
			if _, ok := seen[sc.Name]; ok {
				continue
			}
			seen[sc.Name] = struct{}{}
			names = append(names, sc.Name)
		}
	}
	sort.Strings(names)
	return withCsvPrefix(prefix, names)
}

func completeCleanItems(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	prefix, _, already := splitCsv(toComplete)
	all := []string{"db", "corgi_services", "services", "all"}
	out := make([]string, 0, len(all))
	for _, it := range all {
		if _, dup := already[it]; dup {
			continue
		}
		out = append(out, it)
	}
	return withCsvPrefix(prefix, out)
}

func completeRunOmit(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	prefix, _, already := splitCsv(toComplete)
	all := []string{"beforeStart", "afterStart"}
	out := make([]string, 0, len(all))
	for _, it := range all {
		if _, dup := already[it]; dup {
			continue
		}
		out = append(out, it)
	}
	return withCsvPrefix(prefix, out)
}

func completeTunnelProvider(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	names := tunnel.Names()
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeDockerContext(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"default", "orbctl", "colima"}, cobra.ShellCompDirectiveNoFileComp
}

func completeTemplateName(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	names := make([]string, 0, len(utils.ExampleProjects))
	for _, ex := range utils.ExampleProjects {
		if ex.Path != "" {
			names = append(names, ex.Path)
		}
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

// registerCompletions wires shell completion handlers. Called from Execute()
// so all subcommands have already registered their flags via init().
func registerCompletions() {
	_ = runCmd.RegisterFlagCompletionFunc("services", completeServices)
	_ = runCmd.RegisterFlagCompletionFunc("dbServices", completeDbServices)

	_ = scriptCmd.RegisterFlagCompletionFunc("services", completeServices)
	_ = scriptCmd.RegisterFlagCompletionFunc("names", completeScriptNames)

	_ = statusCmd.RegisterFlagCompletionFunc("service", completeServices)

	_ = cleanCmd.RegisterFlagCompletionFunc("items", completeCleanItems)

	_ = runCmd.RegisterFlagCompletionFunc("omit", completeRunOmit)

	_ = tunnelCmd.RegisterFlagCompletionFunc("provider", completeTunnelProvider)

	_ = rootCmd.RegisterFlagCompletionFunc("dockerContext", completeDockerContext)
	_ = rootCmd.RegisterFlagCompletionFunc("fromTemplateName", completeTemplateName)

	tunnelCmd.ValidArgsFunction = completeTunnelableServices
}
