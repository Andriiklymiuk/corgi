package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

// forkCmd represents the fork command
var forkCmd = &cobra.Command{
	Use:   "fork",
	Short: "Fork an existing service repositories to new repos.",
	Long:  `This is command, that helps to start new projects using currently cloned/created repos and pushing them to newly created ones.`,
	Example: `corgi fork --all

corgi fork

corgi fork --all --private --useSameRepoName --gitProvider github`,
	Run:     runFork,
	PreRunE: preRunE,
}

func preRunE(cmd *cobra.Command, args []string) error {
	gitProvider, err := cmd.Flags().GetString("gitProvider")
	if err != nil {
		return err
	}

	gitProvider = strings.ToLower(gitProvider)
	if gitProvider != "github" && gitProvider != "gitlab" && gitProvider != "" {
		return fmt.Errorf(
			"invalid gitProvider %s; must be either 'github' or 'gitlab'",
			gitProvider,
		)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(forkCmd)
	forkCmd.PersistentFlags().BoolP("all", "", false, "Fork all repos")
	forkCmd.PersistentFlags().BoolP("private", "", false, "Create private repo")
	forkCmd.PersistentFlags().BoolP("useSameRepoName", "", false, "Use previous repo name for new repo")
	forkCmd.PersistentFlags().String("gitProvider", "", "Git provider for new repo")
}

func runFork(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Printf("couldn't get services config, error: %s\n", err)
		return
	}

	corgiMap := GetCorgiServicesMap(corgi)

	CloneServices(corgi.Services)

	shouldForkAllServices, err := cmd.Flags().GetBool("all")
	if err != nil {
		fmt.Println(art.RedColor, err, art.WhiteColor)
		return
	}
	var chosenService string

	if !shouldForkAllServices {
		servicesList := getListOfServicesWithClonedFrom(corgi.Services)
		backString := "🚫 abort"
		chosenService, err = utils.PickItemFromListPrompt(
			"Select command",
			servicesList,
			backString,
		)
		if err != nil {
			if err.Error() == backString {
				fmt.Println(art.RedColor, "Fork creation canceled", art.WhiteColor)
				return
			}
			fmt.Println(art.RedColor, err, art.WhiteColor)
			return
		}
	}

	err = CreateForksForServices(cmd, corgi.Services, chosenService, corgiMap)
	if err != nil {
		fmt.Println(art.RedColor, err, art.WhiteColor)
		return
	}
	UpdateCorgiComposeFileWithMap(corgiMap)
}

type forkFlags struct {
	all             bool
	private         bool
	useSameRepoName bool
	gitProvider     string
}

func readForkFlags(cmd *cobra.Command) (forkFlags, error) {
	var f forkFlags
	var err error
	if f.all, err = cmd.Flags().GetBool("all"); err != nil {
		return f, err
	}
	if f.private, err = cmd.Flags().GetBool("private"); err != nil {
		return f, err
	}
	if f.useSameRepoName, err = cmd.Flags().GetBool("useSameRepoName"); err != nil {
		return f, err
	}
	f.gitProvider, err = cmd.Flags().GetString("gitProvider")
	return f, err
}

func CreateForksForServices(
	cmd *cobra.Command,
	services []utils.Service,
	chosenService string,
	corgiMap map[string]interface{},
) error {
	flags, err := readForkFlags(cmd)
	if err != nil {
		return err
	}
	for _, service := range services {
		if !flags.all && service.ServiceName != chosenService {
			continue
		}
		if err := forkOneService(service, flags, corgiMap); err != nil {
			return err
		}
	}
	return nil
}

func forkOneService(service utils.Service, flags forkFlags, corgiMap map[string]interface{}) error {
	fmt.Println(art.BlueColor, fmt.Sprintf("Changing git repo origin for service %s", service.ServiceName), art.WhiteColor)

	confirmedNew := flags.all
	if !flags.all {
		prompt := promptui.Prompt{
			Label:     fmt.Sprintf("Do you want to create new git repo for %s", service.ServiceName),
			IsConfirm: true,
		}
		_, perr := prompt.Run()
		if perr != nil {
			return nil
		}
		confirmedNew = true
	}

	repoToCloneTo, err := chooseForkTarget(service, flags, confirmedNew)
	if err != nil {
		return err
	}
	fmt.Println(repoToCloneTo)
	if repoToCloneTo == "" {
		fmt.Println("Aborting, repo is empty")
		return nil
	}
	if err := changeRepoOrigin(service.AbsolutePath, service.ServiceName, repoToCloneTo); err != nil {
		return err
	}
	servicesMap := corgiMap[utils.ServicesInConfig].(map[string]*utils.Service)
	servicesMap[service.ServiceName].CloneFrom = repoToCloneTo
	return nil
}

func chooseForkTarget(service utils.Service, flags forkFlags, confirmedNew bool) (string, error) {
	if !confirmedNew {
		return promptManualRepoLink()
	}
	isPrivate := determinePrivacy(flags.private)
	repoName, err := determineRepoName(service.ServiceName, flags.useSameRepoName)
	if err != nil {
		return "", err
	}
	provider, err := determineProvider(flags.gitProvider)
	if err != nil {
		return "", err
	}
	return createRepoForProvider(provider, repoName, isPrivate)
}

func determinePrivacy(forcePrivate bool) bool {
	if forcePrivate {
		return true
	}
	privatePrompt := promptui.Prompt{
		Label:     "Do you want this repo to be private",
		IsConfirm: true,
	}
	_, err := privatePrompt.Run()
	return err == nil
}

func determineRepoName(serviceName string, useSame bool) (string, error) {
	if useSame {
		return serviceName, nil
	}
	namePrompt := promptui.Prompt{Label: "Enter the name for this repo", Default: serviceName}
	return namePrompt.Run()
}

func determineProvider(provider string) (string, error) {
	if provider != "" {
		return provider, nil
	}
	options := []string{"github", "gitlab"}
	return utils.PickItemFromListPrompt("Where do you want to store the repo?", options, "🚫 abort")
}

func createRepoForProvider(provider, repoName string, isPrivate bool) (string, error) {
	switch provider {
	case "github":
		return createRepoInProvider("gh", "https://cli.github.com", repoName, isPrivate)
	case "gitlab":
		return createRepoInProvider("glab", "https://gitlab.com/gitlab-org/cli#installation", repoName, isPrivate)
	}
	return "", nil
}

func promptManualRepoLink() (string, error) {
	prompt := promptui.Prompt{
		Label: "Provide your own repo link (needs to be empty repo)",
		Validate: func(input string) error {
			if strings.HasSuffix(input, ".git") {
				return nil
			}
			return errors.New("the repo link must end with .git")
		},
	}
	return prompt.Run()
}

func runCommandToOutput(outBuffer *bytes.Buffer, path string, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if outBuffer != nil {
		cmd.Stdout = outBuffer
	}
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if path != "" {
		cmd.Dir = path
	}
	err := cmd.Run()
	if err != nil {
		// Handle specific error cases here.
		if execErr, ok := err.(*exec.ExitError); ok {
			// The program has exited with an exit code != 0
			if execErr.ExitCode() == 127 {
				return fmt.Errorf("executable not found: %s", command)
			}
		}
		return err
	}
	return nil
}

func getListOfServicesWithClonedFrom(services []utils.Service) []string {
	var servicesList []string
	for _, service := range services {
		if service.CloneFrom != "" {
			servicesList = append(servicesList, service.ServiceName)
		}
	}
	return servicesList
}

func changeRepoOrigin(path, serviceName, newRepoOrigin string) error {
	// Remove the existing 'origin' if it exists
	err := utils.RunServiceCmd(
		serviceName,
		"git remote remove origin",
		path,
		true,
	)

	if err != nil {
		fmt.Println(
			art.RedColor,
			fmt.Sprintf("failed to remove existing remote origin: %s", err),
			art.WhiteColor,
		)
	}

	// Add new 'origin'
	err = utils.RunServiceCmd(
		serviceName,
		fmt.Sprintf("git remote add origin %s", newRepoOrigin),
		path,
		true,
	)

	if err != nil {
		return fmt.Errorf("failed to add remote: %s", err)
	}

	// Push to new 'origin'
	err = utils.RunServiceCmd(
		serviceName,
		"git push -u origin --all",
		path,
		true,
	)
	if err != nil {
		return fmt.Errorf("failed to push to remote: %s", err)
	}
	fmt.Println(art.BlueColor, "Successfully pushed to new repo!", art.WhiteColor)
	return nil
}

func createRepoInProvider(providerCliName string, providerInstallLink string, repoName string, private bool) (string, error) {
	err := utils.CheckCommandExists(fmt.Sprintf("%s version", providerCliName))
	if err != nil {
		return "", fmt.Errorf(
			"you need to install %s cli and authenticate into it to create repo fork.\nCheck %s",
			providerCliName,
			providerInstallLink,
		)
	}
	var out bytes.Buffer
	privacyFlag := "--public"
	if private {
		privacyFlag = "--private"
	}
	err = runCommandToOutput(&out, "", providerCliName, "repo", "create", repoName, privacyFlag)
	if err != nil {
		return "", fmt.Errorf("failed to create repo: %s", err)
	}
	output := out.String()
	output = strings.TrimSpace(output)
	output = strings.ReplaceAll(output, "\n", "")
	newRepoUrl := output + ".git"
	fmt.Println("Extracted repo URL:", newRepoUrl)

	return newRepoUrl, nil
}
func CheckoutToPrimaryBranch(
	name string,
	path string,
	targetBranch string,
	usePrimaryBranch bool,
) error {
	var out bytes.Buffer
	var err error

	if usePrimaryBranch {
		err = runCommandToOutput(&out, path, "git", "remote", "show", "origin")
		if err != nil {
			fmt.Printf("[%s] Error fetching remote details: %s\n", name, err)
			return err
		}
		targetBranch = strings.TrimSpace(strings.Split(strings.TrimSpace(strings.Split(out.String(), "HEAD branch:")[1]), "\n")[0])
	}

	var currentOut bytes.Buffer
	err = runCommandToOutput(&currentOut, path, "git", "branch", "--show-current")
	if err != nil {
		fmt.Printf("[%s] Error determining current branch: %s\n", name, err)
		return err
	}
	currentBranch := strings.TrimSpace(currentOut.String())

	if currentBranch == targetBranch {
		fmt.Printf("[%s] You are already on the target branch: %s\n", name, targetBranch)
		return nil
	}

	err = runCommandToOutput(nil, path, "git", "checkout", targetBranch)
	if err != nil {
		fmt.Printf("[%s] Error checking out the target branch: %s\n", name, err)
		return err
	}

	err = runCommandToOutput(nil, path, "git", "pull", "origin", targetBranch)
	if err != nil {
		fmt.Printf("[%s] Error pulling the latest changes: %s\n", name, err)
		return err
	}

	fmt.Printf("[%s] Successfully checked out and updated the target branch: %s\n", name, targetBranch)
	return nil
}
