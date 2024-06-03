package utils

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
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

func IsServiceRunning(driver, targetService string) (bool, error) {
	cmd := exec.Command(
		"docker",
		"ps",
		"--all",
		"--filter",
		fmt.Sprintf("name=%s-%s", driver, targetService),
		"--format",
		"{{.Names}}\t{{.Status}}",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return false, fmt.Errorf("error executing docker ps: %v", err)
	}

	lines := strings.Split(out.String(), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) > 1 {
			fmt.Printf("parts: %s\n", parts)
			containerName := parts[0]
			containerStatus := parts[1]

			if strings.Contains(containerName, fmt.Sprintf("%s-%s", driver, targetService)) &&
				strings.HasPrefix(containerStatus, "Up") &&
				parts[len(parts)-1] != "(Paused)" {
				return true, nil
			}
		}
	}

	return false, nil
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

func StartOrbctl() error {
	err := CheckCommandExists("orbctl version")

	if err != nil {
		return fmt.Errorf("orbctl is not installed")
	}

	err = RunServiceCmd("orbctl", "orbctl start", "", false)
	if err != nil {
		return fmt.Errorf("orbctl run failed: %s", err)
	}
	return nil
}

func StartColima() error {
	err := CheckCommandExists("colima version")

	if err != nil {
		return fmt.Errorf("colima is not installed")
	}

	err = RunServiceCmd("colima", "colima start", "", false)
	if err != nil {
		return fmt.Errorf("colima run failed: %s", err)
	}
	return nil
}

type DockerContextConfig struct {
	Name  string
	Start func() error
}

var DockerContextConfigs = map[string]DockerContextConfig{
	"orbctl": {
		Name: "orbctl",
		Start: func() error {
			return StartOrbctl()
		},
	},
	"colima": {
		Name: "colima",
		Start: func() error {
			return StartColima()
		},
	},
	"default": {
		Name: "default",
		Start: func() error {
			return StartDocker()
		},
	},
	"docker-linux": {
		Name: "docker-linux",
		Start: func() error {
			return StartDocker()
		},
	},
}

func isDockerContextValid(dockerContext string) bool {
	validDockerContexts := []string{"default", "orbctl", "colima", "docker-linux"}
	for _, validValue := range validDockerContexts {
		if dockerContext == validValue {
			return true
		}
	}
	return false
}

func DockerInit(cobra *cobra.Command) error {
	err := CheckDockerStatus()
	if err != nil {
		if err.Error() != "docker not opened" {
			return err
		}

		if err.Error() == "docker not opened" {
			dockerContext, err := cobra.Root().Flags().GetString("dockerContext")
			if err != nil {
				fmt.Println("error on getting dockerContext flag", err)
				return err
			}
			isDockerContextValid := isDockerContextValid(dockerContext)

			if isDockerContextValid && dockerContext != "default" && dockerContext != "docker-linux" {
				err = DockerContextConfigs[dockerContext].Start()
				if err == nil {
					fmt.Printf("%s run successfully\n", dockerContext)
					return nil
				}
				fmt.Printf("couldn't open %s, error: %s", dockerContext, err)
			}

			err = StartDocker()
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
	fmt.Println("docker is already running")
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
