package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

// doctorCmd represents the doctor command
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Preflight checks: required tools, Docker, port availability",
	Long: `Preflight checks for running this corgi-compose.

Verifies in order:
  1. Every tool declared in the 'required:' block is installed (offers install
     if present and interactive).
  2. Docker daemon is reachable (skipped if no db_services are declared).
  3. Every port used by db_services and services is free. Ports already in use
     are reported with the owning process (COMMAND, pid).

Exit code is non-zero if anything fails so CI / scripts can consume it.`,
	Run:     runDoctor,
	Aliases: []string{"check", "preflight"},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().Bool("fix", false, "Attempt to remediate failed checks")
	doctorCmd.Flags().Bool("yes", false, "Skip confirmation prompts (required for destructive fixes when non-interactive)")
}

type fixKind int

const (
	fixDocker   fixKind = iota // start Docker daemon — safe, auto-OK
	fixInstall                 // install a missing tool — needs consent
	fixKillPort                // kill a port hog — destructive
)

// shouldAutoFix decides whether a remediation may run without a prompt.
// Docker start is always safe. Installs and kills require either an
// interactive terminal (to prompt) or an explicit --yes.
func shouldAutoFix(kind fixKind, nonInteractive, yes bool) bool {
	if kind == fixDocker {
		return true
	}
	if yes {
		return true
	}
	return !nonInteractive
}

type fixActions struct {
	startDocker func() error
	installTool func(name string) error
	killPort    func(port int) error
	confirm     func(prompt string) bool // asked before destructive fixes in interactive mode
}

type fixSkip struct {
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

type fixOutcome struct {
	OK      bool      `json:"ok"`
	Fixed   []string  `json:"fixed"`
	Skipped []fixSkip `json:"skipped"`
}

const requiredCheckPrefix = "required:"

func classifyCheck(name string) (fixKind, bool) {
	switch {
	case name == "docker":
		return fixDocker, true
	case strings.HasPrefix(name, requiredCheckPrefix):
		return fixInstall, true
	case strings.HasPrefix(name, "port:"):
		return fixKillPort, true
	}
	return 0, false
}

func portFromCheckName(name string) int {
	var p int
	fmt.Sscanf(name, "port:%d", &p)
	return p
}

// applyFix runs the remediation for a kind. Pure dispatch over acts.
func applyFix(kind fixKind, name string, acts fixActions) error {
	switch kind {
	case fixDocker:
		return acts.startDocker()
	case fixInstall:
		return acts.installTool(strings.TrimPrefix(name, requiredCheckPrefix))
	case fixKillPort:
		return acts.killPort(portFromCheckName(name))
	}
	return nil
}

// fixOneCheck decides and applies the remediation for a single failed check.
// Returns fixed=true on success, or a skip reason otherwise. Flat early
// returns keep cognitive complexity low and make it independently testable.
func fixOneCheck(c doctorCheck, acts fixActions, nonInteractive, yes bool) (bool, string) {
	kind, ok := classifyCheck(c.Name)
	if !ok {
		return false, "no remediation available"
	}
	if !shouldAutoFix(kind, nonInteractive, yes) {
		return false, "needs --yes (destructive or requires consent)"
	}
	// Interactive destructive fixes still ask first, unless --yes.
	if kind != fixDocker && !yes && !nonInteractive && acts.confirm != nil && !acts.confirm(fmt.Sprintf("Fix %s?", c.Name)) {
		return false, "declined"
	}
	if err := applyFix(kind, c.Name, acts); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// runFixes walks failed checks and applies the matching remediation, honoring
// the auto-fix gate. All side effects go through acts so it stays testable.
func runFixes(res doctorResult, acts fixActions, nonInteractive, yes bool) fixOutcome {
	out := fixOutcome{OK: true}
	for _, c := range res.Checks {
		if c.OK {
			continue
		}
		if fixed, reason := fixOneCheck(c, acts, nonInteractive, yes); fixed {
			out.Fixed = append(out.Fixed, c.Name)
		} else {
			out.Skipped = append(out.Skipped, fixSkip{c.Name, reason})
			out.OK = false
		}
	}
	return out
}

// installRequiredByName runs the declared install steps for a required tool.
func installRequiredByName(corgi *utils.CorgiCompose, name string) error {
	for _, r := range corgi.Required {
		if r.Name != name {
			continue
		}
		if len(r.Install) == 0 {
			return fmt.Errorf("no install steps declared for %s", name)
		}
		// In JSON mode run non-interactively so the installer's output is
		// routed to stderr (via ConsoleOut), keeping stdout pure JSON.
		interactive := !utils.JSONOutput
		for _, step := range r.Install {
			if err := utils.RunServiceCmd(r.Name, step, "", interactive); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("required tool %s not declared", name)
}

type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type doctorResult struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}

func (r *doctorResult) computeOK() {
	r.OK = true
	for _, c := range r.Checks {
		if !c.OK {
			r.OK = false
			return
		}
	}
}

func runDoctor(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.PrintJSON(doctorResult{OK: false, Checks: []doctorCheck{
				{Name: "config", OK: false, Detail: err.Error()},
			}})
		} else {
			fmt.Printf("couldn't get services config, error: %s\n", err)
		}
		os.Exit(1)
	}

	if fix, _ := cmd.Flags().GetBool("fix"); fix {
		runDoctorFix(cmd, corgi)
		return
	}

	if utils.JSONOutput {
		runDoctorJSON(corgi)
		return
	}

	requiredOK := RunRequired(corgi.Required)
	dockerOK := runDockerCheck(corgi)
	portsOK := runPortChecks(corgi)

	fmt.Println()
	if requiredOK && dockerOK && portsOK {
		fmt.Println(art.GreenColor, "🎉 Doctor: all checks passed", art.WhiteColor)
		return
	}

	fmt.Println(art.RedColor, "❌ Doctor: one or more checks failed", art.WhiteColor)
	os.Exit(1)
}

func runDoctorFix(cmd *cobra.Command, corgi *utils.CorgiCompose) {
	yes, _ := cmd.Flags().GetBool("yes")
	res := buildDoctorResult(corgi)
	acts := fixActions{
		startDocker: utils.StartDockerDaemon,
		installTool: func(name string) error { return installRequiredByName(corgi, name) },
		killPort:    utils.KillPortOwner,
		confirm: func(prompt string) bool {
			p := promptui.Prompt{Label: prompt, IsConfirm: true}
			_, err := p.Run()
			return err == nil
		},
	}
	out := runFixes(res, acts, utils.NonInteractive, yes)

	if utils.JSONOutput {
		utils.PrintJSON(out)
	} else {
		for _, f := range out.Fixed {
			utils.Infof("✅ fixed: %s\n", f)
		}
		for _, s := range out.Skipped {
			utils.Infof("⏭️  skipped %s: %s\n", s.Check, s.Reason)
		}
	}
	if !out.OK {
		os.Exit(1)
	}
}

func runDoctorJSON(corgi *utils.CorgiCompose) {
	res := buildDoctorResult(corgi)
	utils.PrintJSON(res)
	if !res.OK {
		os.Exit(1)
	}
}

// buildDoctorResult runs the preflight checks (required tools, Docker, ports)
// and returns the structured result without printing or exiting.
func buildDoctorResult(corgi *utils.CorgiCompose) doctorResult {
	var res doctorResult

	for _, r := range corgi.Required {
		found, _ := checkRequiredIsFoundQuiet(r)
		c := doctorCheck{Name: "required:" + r.Name, OK: found}
		if !found {
			c.Detail = "not found"
		}
		res.Checks = append(res.Checks, c)
	}

	if len(corgi.DatabaseServices) > 0 {
		dockerOK := utils.IsDockerRunning()
		c := doctorCheck{Name: "docker", OK: dockerOK}
		if !dockerOK {
			c.Detail = "Docker daemon not reachable"
		}
		res.Checks = append(res.Checks, c)
	}

	for _, p := range collectDeclaredPorts(corgi) {
		busy := utils.IsPortListening(p.Port)
		c := doctorCheck{Name: fmt.Sprintf("port:%d", p.Port), OK: !busy}
		if busy {
			owner := utils.PortOwner(p.Port)
			if owner == "" {
				owner = "unknown"
			}
			c.Detail = fmt.Sprintf("busy — needed for %s — held by: %s", p.Desc, owner)
		}
		res.Checks = append(res.Checks, c)
	}

	res.computeOK()
	return res
}

func checkRequiredIsFoundQuiet(required utils.Required) (bool, string) {
	cmdToRunForCheck := required.Name
	if required.CheckCmd != "" {
		cmdToRunForCheck = required.CheckCmd
	}
	if err := utils.CheckCommandExists(cmdToRunForCheck); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// RunRequired is kept for backwards compatibility with cmd/init.go.
// It returns whether all required tools were found.
func RunRequired(required []utils.Required) bool {
	if len(required) == 0 {
		return true
	}
	var notFound []string
	for _, r := range required {
		if !processRequired(r) {
			notFound = append(notFound, r.Name)
		}
	}
	if len(notFound) == 0 {
		return true
	}
	fmt.Println(
		"💬 Missing required tools:",
		art.RedColor,
		strings.Join(notFound, ", "),
		art.WhiteColor,
	)
	return false
}

func runDockerCheck(corgi *utils.CorgiCompose) bool {
	if len(corgi.DatabaseServices) == 0 {
		return true
	}
	fmt.Println()
	if utils.IsDockerRunning() {
		fmt.Println("✅", art.GreenColor, "Docker daemon is running", art.WhiteColor)
		return true
	}
	fmt.Println("❌", art.RedColor,
		"Docker daemon is not reachable — start Docker Desktop / colima / dockerd",
		art.WhiteColor)
	return false
}

// portOwnerInfo names what a port is declared for, in the running compose.
type portOwnerInfo struct {
	Port int
	Desc string // e.g. "db_services.api-db (postgres)" or "services.api"
}

func collectDeclaredPorts(corgi *utils.CorgiCompose) []portOwnerInfo {
	var ports []portOwnerInfo
	for _, db := range corgi.DatabaseServices {
		if db.Port == 0 {
			continue
		}
		ports = append(ports, portOwnerInfo{
			Port: db.Port,
			Desc: fmt.Sprintf("db_services.%s (%s)", db.ServiceName, db.Driver),
		})
	}
	for _, svc := range corgi.Services {
		if svc.Port == 0 || svc.ManualRun {
			continue
		}
		ports = append(ports, portOwnerInfo{
			Port: svc.Port,
			Desc: fmt.Sprintf("services.%s", svc.ServiceName),
		})
	}
	sort.SliceStable(ports, func(i, j int) bool { return ports[i].Port < ports[j].Port })
	return ports
}

func runPortChecks(corgi *utils.CorgiCompose) bool {
	ports := collectDeclaredPorts(corgi)
	if len(ports) == 0 {
		return true
	}
	fmt.Println()
	fmt.Println("🔌 Port availability:")
	allFree := true
	for _, p := range ports {
		if utils.IsPortListening(p.Port) {
			owner := utils.PortOwner(p.Port)
			if owner == "" {
				owner = "(unknown — lsof unavailable)"
			}
			fmt.Printf("  %s ❌ %d busy — needed for %s — held by: %s%s\n",
				art.RedColor, p.Port, p.Desc, owner, art.WhiteColor)
			allFree = false
		} else {
			fmt.Printf("  %s ✅ %d free — for %s%s\n",
				art.GreenColor, p.Port, p.Desc, art.WhiteColor)
		}
	}
	return allFree
}

func processRequired(required utils.Required) bool {
	isFound, description := checkRequiredIsFound(required)
	if isFound {
		fmt.Println(description)
		return true
	}
	fmt.Println(description)
	fmt.Printf("\n%s is needed to:\n", required.Name)
	for _, why := range required.Why {
		fmt.Println("-", why)
	}
	if len(required.Install) == 0 {
		fmt.Printf("\nThere are no install steps for %s\n", required.Name)
		return false
	}
	if required.Optional {
		if utils.NonInteractive {
			fmt.Printf("\n❌ %s is not installed (optional, skipped — no terminal to confirm install)\n", required.Name)
			return false
		}
		prompt := promptui.Prompt{
			Label:     fmt.Sprintf("Do you want to install %s?", required.Name),
			IsConfirm: true,
		}

		_, err := prompt.Run()
		if err != nil {
			fmt.Printf("\n❌ %s is not installed\n", required.Name)
			return false
		}
	}

	for _, installStep := range required.Install {
		err := utils.RunServiceCmd(required.Name, installStep, "", true)
		if err != nil {
			fmt.Println("error happened during installation", err)
			break
		}
	}
	isFound, description = checkRequiredIsFound(required)
	if isFound {
		fmt.Println(description)
		return true
	}
	fmt.Println(description)

	return false
}

func checkRequiredIsFound(required utils.Required) (bool, string) {
	fmt.Println("\n🤖 Required:", art.GreenColor, required.Name, art.WhiteColor)

	var cmdToRunForCheck string
	if required.CheckCmd != "" {
		cmdToRunForCheck = required.CheckCmd
	} else {
		cmdToRunForCheck = required.Name
	}

	err := utils.CheckCommandExists(cmdToRunForCheck)
	if err != nil {
		return false, fmt.Sprintf("\n❌ %s is not found: %s\n", required.Name, err.Error())
	}

	return true, fmt.Sprintf("\n✅ %s is found\n", required.Name)
}

