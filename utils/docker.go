package utils

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/manifoldco/promptui"
)

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

func OrbctlInit() error {
	err := CheckCommandExists("orbctl version")
	if err != nil {
		prompt := promptui.Prompt{
			Label:     "Orbctl is not found, do you want to install it?",
			IsConfirm: true,
		}

		_, err := prompt.Run()
		if err != nil {
			return fmt.Errorf("orbctl is not installed, because of user's choice")
		}

		err = RunServiceCmd("orbctl", "brew install orbstack", "")
		if err != nil {
			return fmt.Errorf("error happened during installation %s", err)
		}

		err = CheckCommandExists("orbctl version")
		if err != nil {
			return fmt.Errorf("orbctl is not installed still")
		}
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

func GetLocalMachineIpAddress() (string, error) {
	cmd := `ifconfig | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | 
	grep -Eo '([0-9]*\.){3}[0-9]*' | 
	grep -v '127.0.0.1' | 
	head -n1`

	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		fmt.Println("Error on getting local machine ip address:", err)
		return "", err
	}

	return strings.TrimSpace(string(out)), nil
}
