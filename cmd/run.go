package cmd

import (
	"fmt"

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
	Run:   run,
}

func run(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices("corgi-compose.yml")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(corgi)
}

func init() {
	rootCmd.AddCommand(runCmd)
}
