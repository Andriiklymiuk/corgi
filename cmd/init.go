package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create db service",
	Long: `
This is used to create db service from template.	
	`,
	Run:     runInit,
	Aliases: []string{"initialize", "clone"},
}

var initDepthFlag int
var initFeatureFlag string

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().IntVar(&initDepthFlag, "depth", 0,
		"Clone each service repo shallow, keeping this many commits (0 = full clone)")
	initCmd.Flags().StringVar(&initFeatureFlag, "feature", "",
		`Check out this branch in every service repo that has it, leaving the rest
on their default branch. Same rule as run --feature, applied to the checkouts
themselves, so anything reading a repo's files afterwards sees the branch.`)
}

// checkoutFeatureBranches switches the freshly cloned repos to the feature
// branch, so a later step reading their files gets the code under review rather
// than the default branch.
func checkoutFeatureBranches(services []utils.Service, branch string) {
	if branch == "" {
		return
	}
	for _, service := range services {
		switched, err := utils.CheckoutFeatureBranch(service.AbsolutePath, branch)
		switch {
		case err != nil:
			fmt.Printf("feature: %s → %v\n", service.ServiceName, err)
		case switched:
			utils.Info("feature:", service.ServiceName, "→", branch)
		default:
			utils.Info("feature:", service.ServiceName, "→ no", branch, "branch, staying on its default")
		}
	}
}

func runInit(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Printf("couldn't get services config, error: %s\n", err)
		return
	}
	utils.CleanFromScratch(cmd, *corgi)

	CreateMissingEnvFiles(corgi.Services)
	CreateDatabaseServices(corgi.DatabaseServices)
	CreateServices(corgi.Services)
	cloneFailures := CloneServices(corgi.Services)
	checkoutFeatureBranches(corgi.Services, initFeatureFlag)
	RunRequired(corgi.Required)

	utils.RunServiceCommands(
		utils.InitInConfig,
		"corgi",
		corgi.Init,
		"",
		false,
		true,
	)

	filesToIgnore := []string{
		"# Added by corgi cli",
		"corgi_services/*",
		".env*",
	}
	filesToIgnore = getGitignoreServicePath(corgi.Services, filesToIgnore)

	for _, fileToIgnore := range filesToIgnore {
		_ = addFileToGitignore(fileToIgnore)
	}

	if len(cloneFailures) > 0 {
		msg := fmt.Sprintf("could not clone: %s — check the remote URLs and your git credentials",
			strings.Join(cloneFailures, ", "))
		if utils.JSONOutput {
			utils.JSONError(utils.ErrConfig, msg)
		} else {
			fmt.Fprintln(os.Stderr, "❌", msg)
		}
		os.Exit(1)
	}
}

func CreateMissingEnvFiles(services []utils.Service) {
	for _, service := range services {
		utils.CreateFileForPath(service.CopyEnvFromFilePath)
	}
}

// Generate database files for each database service
func CreateDatabaseServices(databaseServices []utils.DatabaseService) {
	if len(databaseServices) == 0 {
		utils.Info(`
No db_services info provided -> no db_services created.
Provide them in corgi-compose.yml file`)
		return
	}

	for _, service := range databaseServices {
		filesToCreate := getFilesToCreate(service.Driver)
		var errDuringFileCreation bool
		for _, file := range filesToCreate {
			err := createFileFromTemplate(
				service,
				file.Name,
				file.Template,
				service.ServiceName,
				utils.RootDbServicesFolder,
			)

			if err != nil {
				errDuringFileCreation = true
				fmt.Printf(
					"error creating %s for service %s, error: %s\n",
					file.Name,
					service.ServiceName,
					err,
				)
				break
			}
		}
		if errDuringFileCreation {
			fmt.Print(art.RedColor, "❌ ", art.WhiteColor)
			fmt.Printf("Db service %s had error during creation\n", service.ServiceName)
			continue
		}

		if err := applyDriverPostInit(service); err != nil {
			fmt.Print(art.RedColor, "❌ ", art.WhiteColor)
			fmt.Printf("Db service %s post-init failed: %s\n", service.ServiceName, err)
			continue
		}

		fmt.Print(art.GreenColor, "✅ ", art.WhiteColor)
		fmt.Printf("Db service %s was successfully created\n", service.ServiceName)
	}
}

// Copies the user's configTomlPath into the corgi-managed supabase service
// dir on every init. No-op when configTomlPath isn't set (legacy mode keeps
// supabase/config.toml at project root, created by `supabase init`).
func applyDriverPostInit(service utils.DatabaseService) error {
	if service.Driver != "supabase" || service.ConfigTomlPath == "" {
		return nil
	}

	src := service.ConfigTomlPath
	if !filepath.IsAbs(src) {
		src = filepath.Join(utils.CorgiComposePathDir, src)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read configTomlPath %q: %w", src, err)
	}

	destDir := filepath.Join(
		utils.CorgiComposePathDir,
		utils.RootDbServicesFolder,
		service.ServiceName,
		"supabase",
	)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create %q: %w", destDir, err)
	}
	dest := filepath.Join(destDir, "config.toml")
	if err := os.WriteFile(dest, data, 0644); err != nil {
		return fmt.Errorf("write %q: %w", dest, err)
	}
	fmt.Printf("  copied configTomlPath %s → %s\n", service.ConfigTomlPath, dest)
	return nil
}

func shouldCreateService(service utils.Service) bool {
	if service.Runner.Name == "" || service.Runner.Name != "docker" {
		return false
	}
	if service.Port == 0 {
		fmt.Printf(
			"Service %s does not have port specified, skipping docker runner creation\n",
			service.ServiceName,
		)
		return false
	}
	dockerfileExists, err := utils.CheckIfFileExistsInDirectory(
		service.AbsolutePath,
		"Dockerfile",
	)
	if err != nil {
		fmt.Println(err)
	}
	if !dockerfileExists {
		fmt.Printf(
			"Service %s does not have Dockerfile in path %s\n",
			service.ServiceName,
			service.AbsolutePath,
		)
		return false
	}
	return true
}

func writeServiceFiles(service utils.Service) bool {
	for _, file := range getServiceFilesToCreate(service.Runner.Name) {
		err := createFileFromTemplate(
			service,
			file.Name,
			file.Template,
			service.ServiceName,
			utils.RootServicesFolder,
		)
		if err != nil {
			fmt.Printf(
				"error creating %s for service %s, error: %s\n",
				file.Name,
				service.ServiceName,
				err,
			)
			return false
		}
	}
	return true
}

func createSingleService(service utils.Service) {
	if !shouldCreateService(service) {
		return
	}

	if err := copyEnvFileWithSubstitutions(service); err != nil {
		fmt.Printf("Error copying .env file for service %s: %s\n", service.ServiceName, err)
	} else {
		fmt.Printf("Successfully copied .env file for service %s with substitutions\n", service.ServiceName)
	}

	if writeServiceFiles(service) {
		fmt.Print(art.GreenColor, "✅ ", art.WhiteColor)
		fmt.Printf("Service %s was successfully created\n", service.ServiceName)
	} else {
		fmt.Print(art.RedColor, "❌ ", art.WhiteColor)
		fmt.Printf("Service %s had error during creation\n", service.ServiceName)
	}
}

func CreateServices(services []utils.Service) {
	for _, service := range services {
		createSingleService(service)
	}
}

func copyEnvFileWithSubstitutions(service utils.Service) error {
	envPath := utils.GetPathToEnv(service)
	sourceEnvPath := fmt.Sprintf("%s/%s", service.AbsolutePath, envPath)

	_, err := os.Stat(sourceEnvPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s file does not exist at %s", envPath, sourceEnvPath)
		}
		return fmt.Errorf("error checking %s file at %s: %w", envPath, sourceEnvPath, err)
	}

	content, err := os.ReadFile(sourceEnvPath)
	if err != nil {
		return fmt.Errorf("error reading .env file at %s: %w", sourceEnvPath, err)
	}

	modifiedContent := strings.ReplaceAll(string(content), "localhost", "host.docker.internal")
	modifiedContent = strings.ReplaceAll(modifiedContent, "127.0.0.1", "host.docker.internal")

	destPath := fmt.Sprintf("%s/%s/%s/.env",
		utils.CorgiComposePathDir,
		utils.RootServicesFolder,
		service.ServiceName)

	destDir := fmt.Sprintf("%s/%s/%s",
		utils.CorgiComposePathDir,
		utils.RootServicesFolder,
		service.ServiceName)

	err = os.MkdirAll(destDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("error creating directory %s: %w", destDir, err)
	}

	err = os.WriteFile(destPath, []byte(modifiedContent), 0644)
	if err != nil {
		return fmt.Errorf("error writing modified .env file for docker to %s: %w", destPath, err)
	}

	return nil
}

func getFilesToCreate(driver string) []utils.FilenameForService {
	driverConfig, ok := utils.DriverConfigs[driver]
	if !ok {
		driverConfig = utils.DriverConfigs["default"]
	}

	return driverConfig.FilesToCreate
}

func getServiceFilesToCreate(driver string) []utils.FilenameForService {
	driverConfig, ok := utils.ServiceConfigs[driver]
	if !ok {
		return nil
	}

	return driverConfig.FilesToCreate
}

func CheckClonedReposExistence(services []utils.Service) bool {
	var someRepoShouldBeCloned bool
	for _, service := range services {
		if service.CloneFrom == "" {
			continue
		}
		if service.Path == "" || service.Path == "." {
			continue
		}
		if service.Branch != "" {
			someRepoShouldBeCloned = true
		}
		_, err := os.Stat(
			service.AbsolutePath,
		)
		if err != nil {
			fmt.Printf("Path %s does not exist for service %s. It should be cloned.\n", service.AbsolutePath, service.ServiceName)
			someRepoShouldBeCloned = true
			break
		}
	}
	return someRepoShouldBeCloned
}

// CloneServices clones every service that needs it, returning the names that
// could not be cloned. A caller that carries on regardless — CI, typically —
// otherwise fails much later with an error that says nothing about the clone.
func CloneServices(services []utils.Service) []string {
	var failed []string
	for _, service := range services {
		if !cloneOneService(service) {
			failed = append(failed, service.ServiceName)
		}
	}
	return failed
}

func cloneOneService(service utils.Service) bool {
	if service.Path == "" {
		fmt.Println("\nNo path for", service.ServiceName, ". Using current directory")
		return true
	}

	_, statErr := os.Stat(service.AbsolutePath)
	switch {
	case statErr == nil:
		handleExistingServiceDir(service)
	case errors.Is(statErr, os.ErrNotExist):
		if !cloneMissingServiceDir(service) {
			return false
		}
	default:
		fmt.Println(statErr)
		return false
	}

	maybeRunNestedCorgiInit(service)
	return true
}

func cloneMissingServiceDir(service utils.Service) bool {
	if service.CloneFrom == "" {
		fmt.Printf(
			"No directory %s, please provide cloneFrom url or create service in the path",
			service.CloneFrom,
		)
		return false
	}
	pathSlice := strings.Split(service.AbsolutePath, "/")
	pathWithoutLastFolder := strings.Join(pathSlice[:len(pathSlice)-1], "/")
	if err := os.MkdirAll(pathWithoutLastFolder, os.ModePerm); err != nil {
		fmt.Println(err)
		return false
	}
	if !runGitClone(service, pathWithoutLastFolder) {
		return false
	}
	if service.Branch != "" {
		runBranchCheckout(service)
	}
	return true
}

func gitCloneCmd(service utils.Service, depth int) string {
	if depth > 0 {
		return fmt.Sprintf("git clone --depth %d %s %s", depth, service.CloneFrom, service.AbsolutePath)
	}
	return fmt.Sprintf("git clone %s %s", service.CloneFrom, service.AbsolutePath)
}

func runGitClone(service utils.Service, pathWithoutLastFolder string) bool {
	err := utils.RunServiceCmd(
		service.ServiceName,
		gitCloneCmd(service, initDepthFlag),
		pathWithoutLastFolder,
		true,
	)
	if err == nil {
		return true
	}
	if strings.Contains(err.Error(), "exit status 128") {
		fmt.Printf("Repo %s already exists in %s, skipping clone", service.CloneFrom, service.AbsolutePath)
		return false
	}
	fmt.Printf("output error: %s, in path %s with git clone %s", err, pathWithoutLastFolder, service.CloneFrom)
	return false
}

func runBranchCheckout(service utils.Service) {
	err := utils.RunServiceCmd(
		service.ServiceName,
		fmt.Sprintf("git checkout %s", service.Branch),
		service.AbsolutePath,
		true,
	)
	if err != nil {
		fmt.Printf("output error: %s, in path %s with git checkout %s\n", err, service.AbsolutePath, service.Branch)
		return
	}
	err = utils.RunServiceCmd(service.ServiceName, "corgi pull --silent", service.AbsolutePath, true)
	if err != nil {
		fmt.Printf("output error: %s, in path %s with git pull %s\n", err, service.AbsolutePath, service.Branch)
	}
}

func handleExistingServiceDir(service utils.Service) {
	if service.CloneFrom == "" || service.Branch == "" {
		return
	}
	if err := CheckoutToPrimaryBranch(service.ServiceName, service.AbsolutePath, service.Branch, false); err != nil {
		fmt.Println(err)
	}
}

func nestedInitCmd() string {
	if initDepthFlag > 0 {
		return fmt.Sprintf("corgi init --silent --depth %d", initDepthFlag)
	}
	return "corgi init --silent"
}

func maybeRunNestedCorgiInit(service utils.Service) {
	corgiComposeExists, err := utils.CheckIfFileExistsInDirectory(service.AbsolutePath, utils.CorgiComposeDefaultName)
	if err != nil {
		fmt.Println(err)
	}
	if !corgiComposeExists || service.CloneFrom == "" {
		return
	}
	nested := nestedInitCmd()
	if err := utils.RunServiceCmd(service.ServiceName, nested, service.AbsolutePath, true); err != nil {
		fmt.Printf("output error: %s, in path %s with %s\n", err, service.AbsolutePath, nested)
	}
}

func addFileToGitignore(fileToIgnore string) error {
	gitignorePath := fmt.Sprintf("%s/%s", utils.CorgiComposePathDir, ".gitignore")
	f, err := os.OpenFile(
		gitignorePath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return fmt.Errorf("couldn't open .gitignore file, error: %s", err)
	}
	defer f.Close()

	content, err := os.ReadFile(
		gitignorePath,
	)
	if err != nil {
		return fmt.Errorf("couldn't read .gitignore file, error: %s", err)
	}

	if !strings.Contains(string(content), fileToIgnore) {
		_, err := f.WriteString(fmt.Sprintf(`
%s`, fileToIgnore))
		if err != nil {
			return fmt.Errorf(
				"couldn't add %s to .gitignore, error: %s",
				fileToIgnore,
				err,
			)
		}
		defer f.Close()
	}
	return nil
}

func getGitignoreServicePath(
	services []utils.Service,
	filesToIgnore []string,
) []string {
	for _, service := range services {
		if service.CloneFrom == "" {
			continue
		}
		if service.Path == "" {
			continue
		}
		if strings.Contains(service.Path, "../") {
			continue
		}
		gitignorePath := strings.ReplaceAll(
			service.Path,
			"./",
			"",
		)
		if len(strings.Split(gitignorePath, "/")) > 1 {
			continue
		}
		filesToIgnore = append(filesToIgnore, gitignorePath)
	}
	return filesToIgnore
}

func createFileFromTemplate(
	service interface{},
	fileName string,
	fileTemplate string,
	serviceName string,
	serviceFolder string,
) error {
	fileName, pathToFileName := getPathToFileName(fileName)
	path := fmt.Sprintf(
		"%s/%s/%s/%s",
		utils.CorgiComposePathDir,
		serviceFolder,
		serviceName,
		pathToFileName,
	)

	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return fmt.Errorf("error of creating %s, error: %s", path, err)
	}

	filePath := fmt.Sprintf("%s/%s", path, fileName)
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("error of creating %s, error: %s", filePath, err)
	}
	defer f.Close()

	if sv, ok := service.(utils.Service); ok && fileName == "docker-compose.yml" {
		exposedPort, err := utils.GetExposedPortFromDockerfile(sv)
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			fmt.Println("To fix this, add an EXPOSE directive to your Dockerfile, e.g., EXPOSE 3020")
		} else {
			fileTemplate = strings.Replace(
				fileTemplate,
				"${DOCKERFILE_PORT}",
				exposedPort,
				-1,
			)
		}
	}

	tmp := template.Must(template.New("simple").Parse(fileTemplate))
	err = tmp.Execute(f, service)
	if err != nil {
		return fmt.Errorf(
			"error of creating template %s, error: %s",
			filePath,
			err,
		)
	}
	return nil
}

func getPathToFileName(file string) (string, string) {
	pathSlice := strings.Split(file, "/")
	if len(pathSlice) > 1 {
		fileName := pathSlice[len(pathSlice)-1]
		return fileName, strings.Join(pathSlice[:len(pathSlice)-1], "/") + "/"
	}
	return file, ""
}
