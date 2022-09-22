package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

type DatabaseService struct {
	ServiceName  string
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	DatabaseName string `yaml:"databaseName"`
	Port         int    `yaml:"port"`
}

type Service struct {
	ServiceName   string
	Path          string              `yaml:"path"`
	DockerEnabled bool                `yaml:"docker_enabled"`
	Environment   []map[string]string `yaml:"environment"`
	BeforeStart   []string            `yaml:"beforeStart"`
	Start         []string            `yaml:"start"`
	AfterStart    []string            `yaml:"afterStart"`
}

type CorgiCompose struct {
	DatabaseServices []DatabaseService
	Services         []Service
}

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run all databases and services",
	Long:  `This command helps to run all services and their dependent services.`,
	Run:   runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.PersistentFlags().BoolP(
		"omitBeforeStart",
		"",
		false,
		"Omits all before start commands from corgi-compose config",
	)
}

func runRun(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices("corgi-compose.yml")
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, service := range corgi.Services {
		// think about making it as goroutine
		runService(service, cmd)
	}
}

func runService(service utils.Service, cobraCmd *cobra.Command) {
	fmt.Println(string("\n\033[34m"), "🐶 RUNNING SERVICE", service.ServiceName, string("\033[0m"))
	omitBeforeStart, err := cobraCmd.Flags().GetBool("omitBeforeStart")
	if err != nil {
		return
	}

	if service.BeforeStart != nil && !omitBeforeStart {
		fmt.Println("\nBefore start commands:")
		for _, beforeStartCmd := range service.BeforeStart {
			err := runServiceCmd(beforeStartCmd, service.Path)
			if err != nil {
				fmt.Println(
					string("\033[31m"),
					"aborting all other beforeStart commands for ", service, ", because of ", err,
					string("\033[0m"),
				)
				break
			}
		}
	}
	if service.Start != nil {
		fmt.Println("\nStart commands:")
		for _, startCmd := range service.Start {
			// think about making it as goroutine
			err := runServiceCmd(startCmd, service.Path)
			if err != nil {
				fmt.Println(
					string("\033[31m"),
					"aborting all other start commands for ", service, ", because of ", err,
					string("\033[0m"),
				)
				break
			}
		}
	}
}

func runServiceCmd(serviceCommand string, path string) error {
	fmt.Println("\n🚀 🤖 Executing command: ", string("\033[32m"), serviceCommand, string("\033[0m"))

	commandSlice := strings.Fields(serviceCommand)
	cmd := exec.Command(commandSlice[0], commandSlice[1:]...)

	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
