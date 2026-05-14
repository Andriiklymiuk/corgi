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
}

func runDoctor(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Printf("couldn't get services config, error: %s\n", err)
		os.Exit(1)
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

