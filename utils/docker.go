package utils

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/briandowns/spinner"
)

func getPathToService(targetService string) (string, error) {
	currentWorkingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot get path to service %s", err)
	}

	path := fmt.Sprintf(
		"%s/%s/%s",
		currentWorkingDirectory,
		RootDbServicesFolder,
		targetService,
	)
	return path, nil
}

func GetContainerId(targetService string) (string, error) {
	output, err := ExecuteMakeCommand(targetService, "id")
	if err != nil {
		return "", fmt.Errorf("output error: %s in service %s", err, targetService)
	}

	outputSlice := strings.Split(string(output), "\n")
	if len(outputSlice) < 2 {
		return "", fmt.Errorf("no container id found in service %s", targetService)
	}

	containerId := outputSlice[1]

	if len(containerId) == 0 {
		return "", fmt.Errorf("the service %s isn't started yet", targetService)
	}

	return outputSlice[1], nil
}

func GetMakefileCommandsInDirectory(targetService string) ([]string, error) {
	makeFileExists, err := CheckIfFileExistsInDirectory(
		fmt.Sprintf("./%s/%s", RootDbServicesFolder, targetService),
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

	path, err := getPathToService(targetService)
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
	path, err := getPathToService(targetService)
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

func CheckDockerStatus() error {
	cmd := exec.Command("docker", "ps", "-q")
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorString := fmt.Sprintf(`
		%s
		output error: %s with command %s 
		`, output, err, output)

		if strings.Contains(errorString, "executable file not found") {
			return fmt.Errorf("docker not installed")
		}
		if strings.Contains(errorString, "Is the docker daemon running") {
			return fmt.Errorf("docker not opened")
		}

		return fmt.Errorf(errorString)
	}
	return nil
}

func ExecuteSeedMakeCommand(targetService string, makeCommand ...string) ([]byte, error) {
	path, err := getPathToService(targetService)
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

func GetServiceInfo(targetService string) (string, error) {
	f, err := os.Open(fmt.Sprintf("%s/%s/docker-compose.yml", RootDbServicesFolder, targetService))
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	var service []string
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "POSTGRES") {
			serviceInfo := strings.Replace(strings.TrimSpace(scanner.Text()), "POSTGRES_", "", 1)
			v := strings.Split(serviceInfo, "=")
			l := strings.Split(v[0], " ")[1] + " " + v[len(v)-1]

			service = append(service, l)
		}

		if strings.Contains(scanner.Text(), "5432") {
			serviceInfo := strings.ReplaceAll(strings.TrimSpace(scanner.Text()), `"`, "")
			v := strings.Split(serviceInfo, ":")

			service = append(service, "PORT "+strings.Split(v[0], " ")[1])
		}
	}

	if len(service) == 0 {
		return "", fmt.Errorf("haven't found postgres service info")
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	result := fmt.Sprintf(`
Connection info to %s:
%s

`,
		targetService,
		strings.Join(service, "\n"),
	)

	return result, nil
}

func GetStatusOfService(targetService string) (bool, error) {
	cmd := exec.Command("docker", "container", "ls")
	output, err := cmd.CombinedOutput()
	if err != nil {
		errorString := fmt.Sprintf(`
		%s
		output error: %s 
		`, output, err)

		return false, fmt.Errorf(errorString)
	}
	v := strings.Split(string(output), "\n")

	var serviceIsRunning bool
	for _, a := range v {
		if strings.Contains(a, fmt.Sprintf("postgres-%s", targetService)) {
			serviceIsRunning = true
		}
	}
	return serviceIsRunning, nil
}

func StartDocker() error {
	cmd := exec.Command("open", "/Applications/Docker.app")
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

func DockerInit() error {
	err := CheckDockerStatus()
	if err != nil {
		if err.Error() != "docker not opened" {
			return err
		}

		if err.Error() == "docker not opened" {
			err := StartDocker()
			if err != nil {
				return fmt.Errorf("couldn't open docker, error: %s", err)
			}
			s := spinner.New(spinner.CharSets[39], 100*time.Millisecond)
			s.Suffix = " doing woof magic to start docker"
			s.Start()
			for CheckDockerStatus() != nil {
			}
			s.Stop()
			return nil
		}
	}
	return nil
}
