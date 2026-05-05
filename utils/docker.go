package utils

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
)

const (
	errDockerNotOpened = "docker not opened"
	dockerLinuxCtx     = "docker-linux"
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
		if strings.Contains(errorString, "Is the docker daemon running") ||
			strings.Contains(errorString, "Cannot connect to the Docker daemon") ||
			strings.Contains(errorString, "failed to connect to the docker API") ||
			strings.Contains(errorString, "if the daemon is running") ||
			strings.Contains(errorString, "docker daemon socket") ||
			strings.Contains(errorString, "connect: no such file or directory") ||
			strings.Contains(errorString, "connect: connection refused") {
			return fmt.Errorf("%s", errDockerNotOpened)
		}

		return fmt.Errorf("%s", errorString)
	}
	return nil
}

func IsServiceRunning(containerName string) (bool, error) {
	cmd := exec.Command(
		"docker",
		"ps",
		"--all",
		"--filter",
		fmt.Sprintf("name=%s", containerName),
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

			if strings.Contains(containerName, containerName) &&
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

		return false, fmt.Errorf("%s", errorString)
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
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("open", "/Applications/Docker.app")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("open Docker.app failed: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	case "linux":
		if out, err := exec.Command("systemctl", "start", "docker").CombinedOutput(); err == nil {
			return nil
		} else if strings.Contains(string(out), "Interactive authentication required") ||
			strings.Contains(string(out), "Failed to") {
			if out2, err2 := exec.Command("sudo", "-n", "systemctl", "start", "docker").CombinedOutput(); err2 == nil {
				return nil
			} else {
				_ = out2
			}
		}
		if err := exec.Command("systemctl", "--user", "start", "docker").Run(); err == nil {
			return nil
		}
		return fmt.Errorf("could not start docker daemon on linux. Start manually: `sudo systemctl start docker` (rootful) or `systemctl --user start docker` (rootless)")
	case "windows":
		cmd := exec.Command("cmd", "/C", "start", "", "Docker Desktop.exe")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("could not start Docker Desktop on windows: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported OS for auto-starting docker: %s", runtime.GOOS)
	}
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
	dockerLinuxCtx: {
		Name: dockerLinuxCtx,
		Start: func() error {
			return StartDocker()
		},
	},
}

func isDockerContextValid(dockerContext string) bool {
	validDockerContexts := []string{"default", "orbctl", "colima", dockerLinuxCtx}
	for _, validValue := range validDockerContexts {
		if dockerContext == validValue {
			return true
		}
	}
	return false
}

func DockerInit(cobra *cobra.Command) error {
	err := CheckDockerStatus()
	if err == nil {
		fmt.Println("docker is already running")
		return nil
	}
	if err.Error() != errDockerNotOpened {
		return err
	}

	dockerContext, err := cobra.Root().Flags().GetString("dockerContext")
	if err != nil {
		fmt.Println("error on getting dockerContext flag", err)
		return err
	}

	if tryDockerContextStart(dockerContext) {
		return nil
	}
	return startDockerAndWait()
}

func tryDockerContextStart(dockerContext string) bool {
	if !isDockerContextValid(dockerContext) || dockerContext == "default" || dockerContext == dockerLinuxCtx {
		return false
	}
	err := DockerContextConfigs[dockerContext].Start()
	if err == nil {
		fmt.Printf("%s run successfully\n", dockerContext)
		return true
	}
	fmt.Printf("couldn't open %s, error: %s", dockerContext, err)
	return false
}

func startDockerAndWait() error {
	if err := StartDocker(); err != nil {
		return fmt.Errorf("couldn't open docker, error: %s", err)
	}
	s := spinner.New(spinner.CharSets[39], 100*time.Millisecond)
	s.Suffix = " doing woof magic to start docker"
	s.Start()
	defer s.Stop()
	deadline := time.Now().Add(60 * time.Second)
	for {
		if CheckDockerStatus() == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("docker daemon did not become reachable within 60s — start it manually and retry")
		}
		time.Sleep(500 * time.Millisecond)
	}
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
