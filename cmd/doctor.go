package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

// doctorCmd represents the doctor command
var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check required properties in corgi-compose",
	Long:  `Checks what is required for corgi-compose and installs, if not found.`,
	Run:   runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Printf("couldn't get services config, error: %s\n", err)
		return
	}

	RunRequired(corgi.Required)

}

func RunRequired(required []utils.Required) {
	if len(required) == 0 {
		fmt.Println("No required is added in corgi-compose")
		return
	}
	var notFoundRequiredItems []string

	for _, required := range required {
		isFound := processRequired(required)
		if !isFound {
			notFoundRequiredItems = append(notFoundRequiredItems, required.Name)
		}
	}
	if len(notFoundRequiredItems) != 0 {
		fmt.Println(
			"💬 Some required commands were not found:",
			art.RedColor,
			strings.Join(notFoundRequiredItems, ", "),
			art.WhiteColor,
		)
		return
	}

	fmt.Println("🎉 All required software was found successfully")
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
		err := utils.RunServiceCmd(required.Name, installStep, "")
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

	commandSlice := strings.Fields(cmdToRunForCheck)
	cmd := exec.Command(commandSlice[0], commandSlice[1:]...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Println(err)
	}

	err = cmd.Start()
	if err != nil {
		fmt.Println(err)
	}

	scannerError := bufio.NewScanner(io.MultiReader(stderr))
	scannerError.Split(bufio.ScanLines)
	for scannerError.Scan() {
		message := scannerError.Text()
		fmt.Println(message)
		if strings.Contains(message, "command not found") {
			return false, fmt.Sprintf("\n❌ %s is not found: %s\n", required.Name, message)
		}
	}

	scanner := bufio.NewScanner(io.MultiReader(stdout))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		message := scanner.Text()
		fmt.Println(message)
		if strings.Contains(message, "command not found") {
			return false, fmt.Sprintf("\n❌ %s is not found: %s\n", required.Name, message)
		}
	}

	err = cmd.Wait()
	if err != nil {
		if strings.Contains(err.Error(), "not started") {
			return false, fmt.Sprintf("\n❌ %s is not found: %s\n", required.Name, err)
		}
	}

	return true, fmt.Sprintf("\n✅ %s is found\n", required.Name)
}
