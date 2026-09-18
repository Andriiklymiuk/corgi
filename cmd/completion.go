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

func completeServiceEquals(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if strings.Contains(toComplete, "=") {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(c.Services))
	for name := range c.Services {
		names = append(names, name+"=")
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

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

func parseServicesFilter(raw []string) map[string]struct{} {
	wanted := map[string]struct{}{}
	for _, entry := range raw {
		for _, name := range strings.Split(entry, ",") {
			name = strings.TrimSpace(name)
			if name != "" && name != "none" {
				wanted[name] = struct{}{}
			}
		}
	}
	return wanted
}

func collectScriptName(sc utils.Script, already, seen map[string]struct{}) (string, bool) {
	if sc.Name == "" {
		return "", false
	}
	if _, dup := already[sc.Name]; dup {
		return "", false
	}
	if _, ok := seen[sc.Name]; ok {
		return "", false
	}
	seen[sc.Name] = struct{}{}
	return sc.Name, true
}

func completeScriptNames(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	servicesFilter, _ := cmd.Flags().GetStringSlice("services")
	wanted := parseServicesFilter(servicesFilter)

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
			if name, ok := collectScriptName(sc, already, seen); ok {
				names = append(names, name)
			}
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

func completeProfiles(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	prefix, _, already := splitCsv(toComplete)
	seen := map[string]struct{}{}
	add := func(profiles []string) {
		for _, p := range profiles {
			if _, dup := already[p]; dup {
				continue
			}
			seen[p] = struct{}{}
		}
	}
	for _, svc := range c.Services {
		add(svc.Profiles)
	}
	for _, db := range c.DatabaseServices {
		add(db.Profiles)
	}
	names := make([]string, 0, len(seen))
	for p := range seen {
		names = append(names, p)
	}
	sort.Strings(names)
	return withCsvPrefix(prefix, names)
}

func completeGitProvider(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"github", "gitlab"}, cobra.ShellCompDirectiveNoFileComp
}

func completeTier(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(c.EnvTiers))
	for n := range c.EnvTiers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeHost(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"auto\tdetect first non-loopback IPv4",
		"ip\talias for auto",
	}, cobra.ShellCompDirectiveNoFileComp
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

func registerCompletions() {
	_ = runCmd.RegisterFlagCompletionFunc("services", completeServices)
	_ = runCmd.RegisterFlagCompletionFunc("dbServices", completeDbServices)

	for _, f := range []string{"service-dir", "service-branch", "service-checkout"} {
		_ = runCmd.RegisterFlagCompletionFunc(f, completeServiceEquals)
		_ = execCmd.RegisterFlagCompletionFunc(f, completeServiceEquals)
		_ = testCmd.RegisterFlagCompletionFunc(f, completeServiceEquals)
	}

	_ = scriptCmd.RegisterFlagCompletionFunc("services", completeServices)
	_ = scriptCmd.RegisterFlagCompletionFunc("names", completeScriptNames)

	_ = statusCmd.RegisterFlagCompletionFunc("service", completeServices)

	_ = cleanCmd.RegisterFlagCompletionFunc("items", completeCleanItems)

	_ = runCmd.RegisterFlagCompletionFunc("omit", completeRunOmit)
	_ = runCmd.RegisterFlagCompletionFunc("host", completeHost)
	_ = runCmd.RegisterFlagCompletionFunc("tier", completeTier)
	_ = envCmd.RegisterFlagCompletionFunc("tier", completeTier)

	_ = restartCmd.RegisterFlagCompletionFunc("service", completeServices)
	_ = restartCmd.RegisterFlagCompletionFunc("host", completeHost)

	_ = testCmd.RegisterFlagCompletionFunc("service", completeServices)
	_ = testCmd.RegisterFlagCompletionFunc("profile", completeProfiles)

	_ = checkoutCmd.RegisterFlagCompletionFunc("service", completeServices)

	_ = forkCmd.RegisterFlagCompletionFunc("service", completeServices)
	_ = forkCmd.RegisterFlagCompletionFunc("gitProvider", completeGitProvider)

	_ = mcpCmd.RegisterFlagCompletionFunc("tunnel-provider", completeTunnelProvider)

	_ = tunnelCmd.RegisterFlagCompletionFunc("provider", completeTunnelProvider)

	_ = rootCmd.RegisterFlagCompletionFunc("dockerContext", completeDockerContext)
	_ = rootCmd.RegisterFlagCompletionFunc("fromTemplateName", completeTemplateName)

	tunnelCmd.ValidArgsFunction = completeTunnelableServices
}
