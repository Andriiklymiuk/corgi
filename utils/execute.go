package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

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

func RunServiceCmd(
	serviceName string,
	serviceCommand string,
	path string,
	interactive bool,
) error {
	fmt.Println(serviceCommand)
	lines := strings.Split(serviceCommand, "\n")
	var accumulatedCommand string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// If the line ends with a backslash, remove it and append the line to the next
		if strings.HasSuffix(line, "\\") {
			accumulatedCommand += strings.TrimSuffix(line, "\\") + " "
			continue
		}

		// Execute the accumulated command if any, otherwise execute the line itself
		finalCommand := line
		if accumulatedCommand != "" {
			finalCommand = accumulatedCommand + line
			accumulatedCommand = ""
		}
		executingMessage := fmt.Sprintf("\n🚀 🤖 Executing command for %s: ", serviceName)
		fmt.Println(executingMessage, art.GreenColor, finalCommand, art.WhiteColor)

		commandSlice := strings.Fields(finalCommand)
		if len(commandSlice) == 0 {
			continue
		}

		cmd := exec.Command("/bin/sh", "-c", finalCommand)
		cmd.Dir = path

		if interactive {
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			done := make(chan error, 1)

			err := cmd.Start()
			if err != nil {
				return err
			}

			go func() {
				done <- cmd.Wait()
			}()

			if err := <-done; err != nil {
				return err
			}
		} else {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			SetProcessGroup(cmd)
			if err := cmd.Start(); err != nil {
				return err
			}

			addProcess(cmd.Process)

			if err := cmd.Wait(); err != nil {
				removeProcess(cmd.Process)
				// Check the error directly
				if strings.Contains(err.Error(), "executable file not found") {
					// Attempt to install missing command
					missingCommand := commandSlice[0]
					if cmdInfo, ok := CommandInstructions[missingCommand]; ok {
						fmt.Printf("\n❗%s is missing. Attempting to install it using: %s\n", missingCommand, cmdInfo.Install)
						installCmd := exec.Command("/bin/bash", "-c", cmdInfo.Install)
						installCmd.Dir = path
						installCmd.Stdout = os.Stdout
						installCmd.Stderr = os.Stderr
						if err := installCmd.Run(); err != nil {
							return fmt.Errorf("failed to install %s: %v", missingCommand, err)
						}
						// Rerun the original command
						fmt.Printf("\n🔄 Retrying the command: %s\n", finalCommand)
						return RunServiceCmd(serviceName, finalCommand, path, interactive)
					} else {
						return fmt.Errorf("unknown command %s, no install instructions found", missingCommand)
					}
				} else {
					return err
				}
			}
		}
	}
	return nil
}

func RunServiceCommands(
	commandsName string,
	serviceName string,
	commands []string,
	path string,
	isParallel bool,
	interactive bool,
) {
	if isParallel {
		for _, command := range commands {
			go func(command string) {
				err := RunServiceCmd(
					serviceName,
					command,
					path,
					interactive,
				)
				if err != nil {
					fmt.Println(
						art.RedColor,
						fmt.Sprintf("aborting %s command `%s` for %s, because of %s", commandsName, command, serviceName, err),
						art.WhiteColor,
					)
					return
				}
				if interactive {
					// maybe there is other way to stop the process, but it will do for now
					SendInterrupt()
				}
			}(command)
		}
	} else {
		if len(commands) > 0 {
			combinedCommand := strings.Join(commands, " && ")

			err := RunServiceCmd(
				serviceName,
				combinedCommand,
				path,
				interactive,
			)
			if err != nil {
				fmt.Println(
					art.RedColor,
					fmt.Sprintf("aborting %s commands for %s, because of %s", commandsName, serviceName, err),
					art.WhiteColor,
				)
				return
			}
		}
	}
}

func RunCombinedCmd(command string, path string) error {
	fmt.Println("🚀 🤖 Executing command: ", art.GreenColor, command, art.WhiteColor)

	commandSlice := strings.Fields(command)
	cmd := exec.Command(commandSlice[0], commandSlice[1:]...)
	_, err := cmd.CombinedOutput()
	return err
}

func GetPathToDbService(targetService string) (string, error) {
	path := fmt.Sprintf(
		"%s/%s/%s",
		CorgiComposePathDir,
		RootDbServicesFolder,
		targetService,
	)
	return path, nil
}

func GetPathToService(targetService string) (string, error) {
	path := fmt.Sprintf(
		"%s/%s/%s",
		CorgiComposePathDir,
		RootServicesFolder,
		targetService,
	)
	return path, nil
}

func GetMakefileCommandsInDirectory(targetService string) ([]string, error) {
	makeFileExists, err := CheckIfFileExistsInDirectory(
		fmt.Sprintf(
			"%s/%s/%s",
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
		return nil, fmt.Errorf("path to target service is not found: %s", err)
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
		return nil, fmt.Errorf("path to target service is not found: %s", err)
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
		return fmt.Errorf("path to target service is not found: %s", err)
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
		return fmt.Errorf("path to target service is not found: %s", err)
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
		return nil, fmt.Errorf("path to target service is not found: %s", err)
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
			return nil, fmt.Errorf(errorString)
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
			return fmt.Errorf(message)
		}
	}

	scanner := bufio.NewScanner(io.MultiReader(stdout))
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		message := scanner.Text()
		fmt.Println(message)
		if strings.Contains(message, "command not found") {
			return fmt.Errorf(message)
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
