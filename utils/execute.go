package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AfterStartTimeout bounds each cleanup command. Hung afterStart must
// not block corgi shutdown.
var AfterStartTimeout = 60 * time.Second

const (
	servicePathFmt           = "%s/%s/%s"
	errPathToServiceNotFound = "path to target service is not found: %s"
)

// withEnvSource prepends a POSIX `set -a; . <envFile>; set +a; ` prefix to a
// command when an env file exists for the service. Lets a start command like
// `npx vite --port $PORT` see PORT and other corgi-emitted vars without each
// service writing its own `source .env` boilerplate.
//
// envFile is the absolute path to the file. Empty / missing → command
// returned untouched. Sourcing uses POSIX `.` so works under /bin/sh.
func withEnvSource(command, envFile string) string {
	if envFile == "" {
		return command
	}
	if _, err := os.Stat(envFile); err != nil {
		return command
	}
	return fmt.Sprintf("set -a; . %q; set +a; %s", envFile, command)
}

// SkipAutoSourceEnv disables auto-sourcing for a single command.
const SkipAutoSourceEnv = "<<corgi:no-env-source>>"

func resolveEnvFile(path string, envFileOverride []string) string {
	if path == "" {
		return ""
	}
	if len(envFileOverride) > 0 {
		override := envFileOverride[0]
		if override == SkipAutoSourceEnv {
			return ""
		}
		if override != "" {
			if filepath.IsAbs(override) {
				return override
			}
			return filepath.Join(path, override)
		}
	}
	return filepath.Join(path, ".env")
}

var (
	ProcessHandles []*os.Process
	pidMutex       sync.Mutex
)

func addProcess(proc *os.Process) {
	pidMutex.Lock()
	ProcessHandles = append(ProcessHandles, proc)
	pidMutex.Unlock()
}

func removeProcess(proc *os.Process) {
	pidMutex.Lock()
	for i, storedProc := range ProcessHandles {
		if storedProc.Pid == proc.Pid {
			ProcessHandles = append(ProcessHandles[:i], ProcessHandles[i+1:]...)
			break
		}
	}
	pidMutex.Unlock()
}

func KillAllStoredProcesses() {
	pidMutex.Lock()
	defer pidMutex.Unlock()
	for _, proc := range ProcessHandles {
		KillProcessGroup(proc.Pid)
		proc.Release()
	}
	ProcessHandles = []*os.Process{}
}

// RunServiceCmd executes a single shell command for a service.
//
// Optional envFile (variadic for backwards compatibility): filename or
// absolute path of the env file to source before the command. When omitted
// or empty, defaults to `<path>/.env` if present.
func RunServiceCmd(
	serviceName, serviceCommand, path string,
	interactive bool,
	envFile ...string,
) error {
	resolvedEnvFile := resolveEnvFile(path, envFile)
	fmt.Println(serviceCommand)
	lines := strings.Split(serviceCommand, "\n")
	var accumulatedCommand string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			accumulatedCommand += strings.TrimSuffix(line, "\\") + " "
			continue
		}
		finalCommand := line
		if accumulatedCommand != "" {
			finalCommand = accumulatedCommand + line
			accumulatedCommand = ""
		}
		if err := executeShellLine(serviceName, finalCommand, path, interactive, resolvedEnvFile, envFile); err != nil {
			return err
		}
	}
	return nil
}

func executeShellLine(serviceName, finalCommand, path string, interactive bool, resolvedEnvFile string, envFile []string) error {
	fmt.Printf("\n🚀 🤖 Executing command for %s:  %s%s%s\n", serviceName, art.GreenColor, finalCommand, art.WhiteColor)
	commandSlice := strings.Fields(finalCommand)
	if len(commandSlice) == 0 {
		return nil
	}
	shellCommand := withEnvSource(finalCommand, resolvedEnvFile)
	cmd := exec.Command("/bin/sh", "-c", shellCommand)
	cmd.Dir = path

	if interactive {
		return runInteractive(cmd)
	}
	return runManaged(cmd, commandSlice, serviceName, finalCommand, path, envFile)
}

func runInteractive(cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { done <- cmd.Wait() }()
	return <-done
}

func runManaged(cmd *exec.Cmd, commandSlice []string, serviceName, finalCommand, path string, envFile []string) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	addProcess(cmd.Process)
	if err := cmd.Wait(); err != nil {
		removeProcess(cmd.Process)
		return handleCommandFailure(err, commandSlice, serviceName, finalCommand, path, envFile)
	}
	return nil
}

func handleCommandFailure(err error, commandSlice []string, serviceName, finalCommand, path string, envFile []string) error {
	if !strings.Contains(err.Error(), "executable file not found") {
		return err
	}
	missingCommand := commandSlice[0]
	cmdInfo, ok := CommandInstructions[missingCommand]
	if !ok {
		return fmt.Errorf("unknown command %s, no install instructions found", missingCommand)
	}
	fmt.Printf("\n❗%s is missing. Attempting to install it using: %s\n", missingCommand, cmdInfo.Install)
	installCmd := exec.Command("/bin/bash", "-c", cmdInfo.Install)
	installCmd.Dir = path
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install %s: %v", missingCommand, err)
	}
	fmt.Printf("\n🔄 Retrying the command: %s\n", finalCommand)
	return RunServiceCmd(serviceName, finalCommand, path, false, envFile...)
}

// RunServiceCommands runs a list of commands for a service.
//
// Optional envFile (variadic): see RunServiceCmd. Forwarded unchanged.
func RunServiceCommands(
	commandsName, serviceName string,
	commands []string,
	path string,
	isParallel, interactive bool,
	envFile ...string,
) {
	if isParallel {
		runCommandsParallel(commandsName, serviceName, commands, path, interactive, envFile)
		return
	}
	runCommandsSequential(commandsName, serviceName, commands, path, interactive, envFile)
}

func runCommandsParallel(commandsName, serviceName string, commands []string, path string, interactive bool, envFile []string) {
	for _, command := range commands {
		go func(command string) {
			err := RunServiceCmd(serviceName, command, path, interactive, envFile...)
			if err != nil {
				fmt.Println(
					art.RedColor,
					fmt.Sprintf("aborting %s command `%s` for %s, because of %s", commandsName, command, serviceName, err),
					art.WhiteColor,
				)
				return
			}
			if interactive {
				SendInterrupt()
			}
		}(command)
	}
}

// RunCleanupCommands runs cleanup commands sequentially with a per-cmd
// timeout. Not tracked in ProcessHandles — must survive concurrent
// KillAllStoredProcesses sweeps. Own pgroup so timeout can kill children.
func RunCleanupCommands(
	commandsName, serviceName string,
	commands []string,
	path string,
	envFile string,
) {
	if len(commands) == 0 {
		return
	}
	resolvedEnvFile := resolveEnvFile(path, []string{envFile})
	for _, command := range commands {
		if err := runCleanupCommand(commandsName, serviceName, command, path, resolvedEnvFile); err != nil {
			fmt.Println(
				art.RedColor,
				fmt.Sprintf("aborting %s commands for %s, because of %s", commandsName, serviceName, err),
				art.WhiteColor,
			)
			return
		}
	}
}

func runCleanupCommand(commandsName, serviceName, command, path, resolvedEnvFile string) error {
	fmt.Printf("\n🚀 🤖 Executing %s for %s:  %s%s%s\n", commandsName, serviceName, art.GreenColor, command, art.WhiteColor)
	shellCommand := withEnvSource(command, resolvedEnvFile)

	ctx, cancel := context.WithTimeout(context.Background(), AfterStartTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", shellCommand)
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	SetProcessGroup(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return KillProcessGroup(cmd.Process.Pid)
	}

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timeout after %s", AfterStartTimeout)
	}
	return err
}

func runCommandsSequential(commandsName, serviceName string, commands []string, path string, interactive bool, envFile []string) {
	if len(commands) == 0 {
		return
	}
	combinedCommand := strings.Join(commands, " && ")
	err := RunServiceCmd(serviceName, combinedCommand, path, interactive, envFile...)
	if err != nil {
		fmt.Println(
			art.RedColor,
			fmt.Sprintf("aborting %s commands for %s, because of %s", commandsName, serviceName, err),
			art.WhiteColor,
		)
	}
}

func RunCombinedCmd(command, path string) error {
	fmt.Println("🚀 🤖 Executing command: ", art.GreenColor, command, art.WhiteColor)

	commandSlice := strings.Fields(command)
	cmd := exec.Command(commandSlice[0], commandSlice[1:]...)
	_, err := cmd.CombinedOutput()
	return err
}

func GetPathToDbService(targetService string) (string, error) {
	path := fmt.Sprintf(
		servicePathFmt,
		CorgiComposePathDir,
		RootDbServicesFolder,
		targetService,
	)
	return path, nil
}

func GetPathToService(targetService string) (string, error) {
	path := fmt.Sprintf(
		servicePathFmt,
		CorgiComposePathDir,
		RootServicesFolder,
		targetService,
	)
	return path, nil
}

func GetMakefileCommandsInDirectory(targetService string) ([]string, error) {
	makeFileExists, err := CheckIfFileExistsInDirectory(
		fmt.Sprintf(
			servicePathFmt,
			CorgiComposePathDir,
			RootDbServicesFolder,
			targetService,
		),
		"Makefile",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to check if Makefile exists in %s, error: %s",
			targetService,
			err,
		)
	}

	if !makeFileExists {
		return nil, fmt.Errorf("no makefile found in %s", targetService)
	}

	path, err := GetPathToDbService(targetService)
	if err != nil {
		return nil, fmt.Errorf(errPathToServiceNotFound, err)
	}

	cmd := exec.Command("make", "help")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("output error: %s in path %s", err, path)
	}

	outputSlice := strings.Split(
		strings.TrimSpace(string(output)),
		"\n",
	)

	return outputSlice[1:], nil
}

func ExecuteMakeCommand(targetService string, makeCommand ...string) ([]byte, error) {
	path, err := GetPathToDbService(targetService)
	if err != nil {
		return nil, fmt.Errorf(errPathToServiceNotFound, err)
	}

	cmd := exec.Command("make", makeCommand...)
	cmd.Dir = path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(`%s
		output error: %s in path %s, with command make %s
		`, output, err, path, makeCommand)
	}

	fmt.Print(string(output))

	return output, nil
}

func ExecuteCommandRun(targetService string, command ...string) error {
	path, err := GetPathToDbService(targetService)
	if err != nil {
		return fmt.Errorf(errPathToServiceNotFound, err)
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf(`output error: %s, in path %s with command make %s
		`, err, path, command)
	}

	return nil
}

func ExecuteServiceCommandRun(targetService string, command ...string) error {
	path, err := GetPathToService(targetService)
	if err != nil {
		return fmt.Errorf(errPathToServiceNotFound, err)
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf(`output error: %s, in path %s with command make %s
		`, err, path, command)
	}

	return nil
}

func ExecuteSeedMakeCommand(targetService string, makeCommand ...string) ([]byte, error) {
	path, err := GetPathToDbService(targetService)
	if err != nil {
		return nil, fmt.Errorf(errPathToServiceNotFound, err)
	}

	cmd := exec.Command("make", makeCommand...)
	cmd.Dir = path

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	scannerError := bufio.NewScanner(io.MultiReader(stderr))
	scannerError.Split(bufio.ScanLines)

	var errorsList []string
	for scannerError.Scan() {
		errorsList = append(errorsList, scannerError.Text())
	}

	if len(errorsList) != 0 {
		errorString := strings.Join(errorsList, "\n")
		if len(errorsList) >= 10 {
			return nil, fmt.Errorf("%s", errorString)
		} else {
			fmt.Println(errorString)
		}
	}

	scanner := bufio.NewScanner(io.MultiReader(stdout))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		m := scanner.Text()
		fmt.Println(m)
	}

	err = cmd.Wait()
	if err != nil {
		return nil, err
	}

	output := []byte(
		fmt.Sprintf("Successful make command of %s",
			strings.Join(makeCommand, " ")),
	)

	return output, nil
}

func CheckCommandExists(command string) error {
	commandSlice := strings.Fields(command)
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
			return fmt.Errorf("%s", message)
		}
	}

	scanner := bufio.NewScanner(io.MultiReader(stdout))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		message := scanner.Text()
		fmt.Println(message)
		if strings.Contains(message, "command not found") {
			return fmt.Errorf("%s", message)
		}
	}

	err = cmd.Wait()
	if err != nil {
		if strings.Contains(err.Error(), "not started") {
			return err
		}
	}

	return nil
}
