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
	Long:  `This is command, that helps to bootstrap new projects using currently cloned/created repos and pushing them to newly created ones.`,
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

func CreateForksForServices(
	cmd *cobra.Command,
	services []utils.Service,
	chosenService string,
	corgiMap map[string]interface{},
) error {
	shouldForkAllServices, err := cmd.Flags().GetBool("all")
	if err != nil {
		return err
	}
	shouldNewReposBePrivate, err := cmd.Flags().GetBool("private")
	if err != nil {
		return err
	}
	useSameRepoName, err := cmd.Flags().GetBool("useSameRepoName")
	if err != nil {
		return err
	}
	newGitProvider, err := cmd.Flags().GetString("gitProvider")
	if err != nil {
		return err
	}
	for _, service := range services {
		if !shouldForkAllServices && service.ServiceName != chosenService {
			continue
		}

		fmt.Println(art.BlueColor, fmt.Sprintf("Changing git repo origin for service %s", service.ServiceName), art.WhiteColor)

		if !shouldForkAllServices {
			prompt := promptui.Prompt{
				Label:     fmt.Sprintf("Do you want to create new git repo for %s", service.ServiceName),
				IsConfirm: true,
			}

			_, err = prompt.Run()
			if err != nil {
				continue
			}
		}

		var repoToCloneTo string
		var isNewRepoPrivate bool
		var repoName string

		if err == nil {
			if !shouldNewReposBePrivate {
				privatePrompt := promptui.Prompt{
					Label:     "Do you want this repo to be private",
					IsConfirm: true,
				}
				_, err = privatePrompt.Run()
				isNewRepoPrivate = (err == nil)
			} else {
				isNewRepoPrivate = shouldNewReposBePrivate
			}

			if useSameRepoName {
				repoName = service.ServiceName
			} else {
				namePrompt := promptui.Prompt{
					Label:   "Enter the name for this repo",
					Default: service.ServiceName,
				}
				repoName, err = namePrompt.Run()
				if err != nil {
					return err
				}
			}

			if newGitProvider == "" {
				options := []string{"github", "gitlab"}
				newGitProvider, err = utils.PickItemFromListPrompt("Where do you want to store the repo?", options, "🚫 abort")
				if err != nil {
					return err
				}
			}
			// ok, user wants to create new git repo
			switch newGitProvider {
			case "github":
				repoToCloneTo, err = createRepoInProvider("gh", "https://cli.github.com", repoName, isNewRepoPrivate)
				if err != nil {
					return err
				}
			case "gitlab":
				repoToCloneTo, err = createRepoInProvider("glab", "https://gitlab.com/gitlab-org/cli#installation", repoName, isNewRepoPrivate)
				if err != nil {
					return err
				}
			}
		} else {
			prompt := promptui.Prompt{
				Label: "Provide your own repo link (needs to be empty repo)",
				Validate: func(input string) error {
					if strings.HasSuffix(input, ".git") {
						return nil
					}
					return errors.New("the repo link must end with .git")
				},
			}
			repoToCloneTo, err = prompt.Run()
			if err != nil {
				return err
			}
		}

		fmt.Println(repoToCloneTo)
		if repoToCloneTo == "" {
			fmt.Println("Aborting, repo is empty")
			continue
		}

		err = changeRepoOrigin(service.AbsolutePath, service.ServiceName, repoToCloneTo)
		if err != nil {
			return err
		}

		servicesMap := corgiMap[utils.ServicesInConfig].(map[string]*utils.Service)
		servicesMap[service.ServiceName].CloneFrom = repoToCloneTo
	}
	return nil
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

func changeRepoOrigin(path string, serviceName string, newRepoOrigin string) error {
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
