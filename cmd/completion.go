/*
Copyright © 2026 Andrii Klymiuk
*/
package cmd

import (
	"andriiklymiuk/corgi/utils"
	"os"
	"path/filepath"
	"sort"

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

func completeServices(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(c.Services)+1)
	for name := range c.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	names = append(names, "none")
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeDbServices(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(c.DatabaseServices)+1)
	for name := range c.DatabaseServices {
		names = append(names, name)
	}
	sort.Strings(names)
	names = append(names, "none")
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeScriptNames(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	c := loadComposeForCompletion(cmd)
	if c == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	seen := map[string]struct{}{}
	var names []string
	for _, svc := range c.Services {
		for _, sc := range svc.Scripts {
			if sc.Name == "" {
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
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeCleanItems(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"db", "corgi_services", "services", "all"}, cobra.ShellCompDirectiveNoFileComp
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

	tunnelCmd.ValidArgsFunction = completeServices
}
