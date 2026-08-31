package cmd

import (
	"fmt"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var (
	checkoutOnlyServices  []string
	checkoutSkipWorkspace bool
	checkoutAllowDirty    bool
)

const workspaceRepoName = "workspace"

var checkoutCmd = &cobra.Command{
	Use:     "checkout [branch]",
	Aliases: []string{"co"},
	Short:   "Checkout every repo to a branch and pull it",
	Long: `Puts the workspace repo and every service repo on <branch>, then fast-forwards it.

A repo that does not carry that branch falls back to its own default branch
(origin/HEAD), so "corgi checkout main" still lands next to a repo whose trunk is
called master. A repo that can do neither is reported and the command exits 1.

Without <branch> every repo goes to its own default branch.

Repos with uncommitted changes are skipped, not clobbered.`,
	Example: `corgi checkout main

corgi checkout trunk --service api --service web

corgi checkout --json`,
	Args: cobra.MaximumNArgs(1),
	Run:  runCheckout,
}

func init() {
	rootCmd.AddCommand(checkoutCmd)
	checkoutCmd.Flags().StringArrayVar(&checkoutOnlyServices, "service", nil, "Only these services (repeatable, or comma separated). Leaves the workspace repo alone")
	checkoutCmd.Flags().BoolVar(&checkoutSkipWorkspace, "skip-workspace", false, "Leave the repo holding corgi-compose.yml on its current branch")
	checkoutCmd.Flags().BoolVar(&checkoutAllowDirty, "allow-dirty", false, "Try even with uncommitted changes (git still refuses an unsafe switch)")
}

type checkoutTarget struct {
	name string
	path string
}

func runCheckout(cmd *cobra.Command, args []string) {
	corgi := mustLoadCorgiServices(cmd)

	var branch string
	if len(args) == 1 {
		branch = strings.TrimSpace(args[0])
	}

	only, err := checkoutServiceFilter(corgi)
	if err != nil {
		exitWithError(utils.ErrServiceNotFound, err, 1)
		return
	}
	targets := checkoutTargets(corgi, only)
	if len(targets) == 0 {
		utils.Info("no repos to checkout")
		return
	}

	results := checkoutAll(targets, branch)

	if utils.JSONOutput {
		utils.PrintJSON(results)
	} else {
		printCheckoutSummary(results)
	}
	if countCheckoutStatus(results, utils.CheckoutFailed) > 0 {
		exitProcess(1)
	}
}

func checkoutAll(targets []checkoutTarget, branch string) []utils.RepoCheckout {
	nameWidth := 0
	for _, target := range targets {
		if len(target.name) > nameWidth {
			nameWidth = len(target.name)
		}
	}

	results := make([]utils.RepoCheckout, 0, len(targets))
	handledBy := map[string]string{}
	for _, target := range targets {
		result := checkoutOne(target, branch, handledBy)
		results = append(results, result)
		if !utils.JSONOutput {
			utils.Info(checkoutRow(result, nameWidth))
		}
	}
	return results
}

func checkoutOne(target checkoutTarget, branch string, handledBy map[string]string) utils.RepoCheckout {
	key := target.path
	if root, ok := utils.RepoRootOf(target.path); ok {
		key = root
	}
	if owner, done := handledBy[key]; done {
		return utils.RepoCheckout{
			Name:    target.name,
			Path:    target.path,
			Status:  utils.CheckoutSkipped,
			Message: "same repo as " + owner,
		}
	}
	handledBy[key] = target.name
	return utils.CheckoutRepo(target.name, target.path, branch, checkoutAllowDirty)
}

func checkoutTargets(corgi *utils.CorgiCompose, only map[string]bool) []checkoutTarget {
	var targets, services []checkoutTarget
	if only == nil && !checkoutSkipWorkspace {
		targets = append(targets, checkoutTarget{name: workspaceRepoName, path: utils.CorgiComposePathDir})
	}
	for _, service := range corgi.Services {
		if service.AbsolutePath == "" {
			continue
		}
		if only != nil && !only[service.ServiceName] {
			continue
		}
		services = append(services, checkoutTarget{name: service.ServiceName, path: service.AbsolutePath})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].name < services[j].name })
	return append(targets, services...)
}

func checkoutServiceFilter(corgi *utils.CorgiCompose) (map[string]bool, error) {
	if len(checkoutOnlyServices) == 0 {
		return nil, nil
	}
	known := map[string]bool{}
	for _, service := range corgi.Services {
		known[service.ServiceName] = true
	}
	only := map[string]bool{}
	for _, group := range checkoutOnlyServices {
		for _, name := range strings.Split(group, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !known[name] {
				return nil, fmt.Errorf("no service named %q in corgi-compose.yml", name)
			}
			only[name] = true
		}
	}
	if len(only) == 0 {
		return nil, fmt.Errorf("--service needs at least one service name")
	}
	return only, nil
}

func checkoutRow(result utils.RepoCheckout, nameWidth int) string {
	branch := result.Branch
	if branch == "" {
		branch = "-"
	}
	return fmt.Sprintf("%s %-*s  %-16s  %s", checkoutIcon(result.Status), nameWidth, result.Name, branch, checkoutNote(result))
}

func checkoutIcon(status string) string {
	switch status {
	case utils.CheckoutFailed:
		return "✖"
	case utils.CheckoutSkipped:
		return "•"
	default:
		return "✔"
	}
}

func checkoutNote(result utils.RepoCheckout) string {
	note := result.Status
	if result.Fallback {
		note += " (default branch)"
	}
	if result.Message != "" {
		note += ": " + result.Message
	}
	return note
}

func printCheckoutSummary(results []utils.RepoCheckout) {
	utils.Infof(
		"\n%d updated, %d already current, %d skipped, %d failed\n",
		countCheckoutStatus(results, utils.CheckoutUpdated),
		countCheckoutStatus(results, utils.CheckoutUpToDate),
		countCheckoutStatus(results, utils.CheckoutSkipped),
		countCheckoutStatus(results, utils.CheckoutFailed),
	)
}

func countCheckoutStatus(results []utils.RepoCheckout, status string) int {
	n := 0
	for _, result := range results {
		if result.Status == status {
			n++
		}
	}
	return n
}
