package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// corgi in a container: the same daemon loop as on a laptop, on any Docker
// host, this machine or a server over ssh. The image carries corgi, Claude
// Code, git and the forge CLIs; the agent dir and the clones live in
// volumes, so the container is replaceable and the state is not.

const containerName = "corgi-agent"

const containerDockerfile = `FROM debian:bookworm-slim
ARG CORGI_VERSION=
ENV DEBIAN_FRONTEND=noninteractive CORGI_DATA_DIR=/data CLAUDE_CONFIG_DIR=/claude DISABLE_AUTOUPDATER=1
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl git openssh-client gnupg jq tmux procps \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && curl -fsSL https://download.docker.com/linux/debian/gpg -o /usr/share/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker.asc] https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list \
    && apt-get update && apt-get install -y --no-install-recommends docker-ce-cli docker-compose-plugin \
    && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 1000 corgi && mkdir -p /data /work /claude && chown -R corgi:corgi /data /work /claude
USER corgi
ENV HOME=/home/corgi PATH=/home/corgi/.local/bin:/usr/local/bin:/usr/bin:/bin
RUN curl -fsSL https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh | CORGI_VERSION=$CORGI_VERSION CORGI_INSTALL_DIR=/home/corgi/.local/bin sh \
    && curl -fsSL https://claude.ai/install.sh | bash
COPY --chown=corgi:corgi entrypoint.sh /home/corgi/entrypoint.sh
WORKDIR /work
ENTRYPOINT ["/home/corgi/entrypoint.sh"]
`

const containerEntrypoint = `#!/bin/sh
set -e
export CORGI_DATA_DIR=/data CLAUDE_CONFIG_DIR=/claude
git config --global --get user.name >/dev/null || git config --global user.name "${GIT_AUTHOR_NAME:-corgi}"
git config --global --get user.email >/dev/null || git config --global user.email "${GIT_AUTHOR_EMAIL:-corgi@localhost}"
if [ -n "$CORGI_CLONE" ] && [ ! -d /work/stack ]; then
  git clone "$CORGI_CLONE" /work/stack
  cd /work/stack && corgi init --json >/dev/null 2>&1 || true
  corgi agent init >/dev/null 2>&1 || true
fi
cd /work/stack 2>/dev/null || cd /work
corgi agent up ${CORGI_UP_FLAGS} || corgi agent serve &
sleep 2
exec tail -F /data/agent/serve.log /data/agent/mcp.log 2>/dev/null
`

const containerCompose = `services:
  corgi-agent:
    image: corgi-agent:local
    build: .
    container_name: corgi-agent
    restart: unless-stopped
    env_file: .env
    volumes:
      - corgi-agent-data:/data
      - corgi-agent-work:/work
      - corgi-agent-claude:/claude
%s
volumes:
  corgi-agent-data:
  corgi-agent-work:
  corgi-agent-claude:
`

var agentContainerCmd = &cobra.Command{
	Use:   "container [up|shell|logs|stop|down|status]",
	Short: "Run the agent daemon in a Docker container, here or on a server over ssh",
	Long: `The same loop as on a laptop - the daemon, the watch, the routines, the
headless fixes, the phone through a tunnel - in a container on any Docker
host. The image has corgi, Claude Code, git, gh and the docker CLI; the
agent dir, the clones and the Claude login live in volumes.

  corgi agent container up                              # build + start here
  corgi agent container up --host ssh://me@build-1      # on a server (its Docker, its volumes)
  corgi agent container up --clone git@github.com:acme/stack.git --docker
  corgi agent container shell                           # a shell inside: corgi agent watch enable ...
  corgi agent container logs | stop | down | status

up asks once for a Claude token (claude setup-token prints one) and keeps
it in the container's env file. --docker mounts the host's Docker socket so
a fix can run the stack. The files are written under the agent dir
(container/) - edit the Dockerfile or compose.yml there and run up again.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAgentContainer,
}

func containerDir(agent string) string { return filepath.Join(agent, "container") }

func runAgentContainer(cmd *cobra.Command, args []string) error {
	dir, err := agentDir()
	if err != nil {
		return err
	}
	action := "status"
	if len(args) == 1 {
		action = strings.ToLower(args[0])
	}
	host, _ := cmd.Flags().GetString("host")
	env := os.Environ()
	if host != "" {
		env = append(env, "DOCKER_HOST="+host)
	}
	compose := func(stream bool, parts ...string) error {
		c := exec.Command("docker", append([]string{"compose", "-f", filepath.Join(containerDir(dir), "compose.yml")}, parts...)...)
		c.Env = env
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if stream {
			c.Stdin = os.Stdin
		}
		return c.Run()
	}
	switch action {
	case "up":
		clone, _ := cmd.Flags().GetString("clone")
		withDocker, _ := cmd.Flags().GetBool("docker")
		if err := writeContainerFiles(dir, clone, withDocker); err != nil {
			return err
		}
		if err := ensureContainerToken(dir); err != nil {
			return err
		}
		if err := compose(false, "up", "-d", "--build"); err != nil {
			return err
		}
		utils.Info("✓ corgi-agent is up")
		utils.Info("next: corgi agent container logs   (the pairing QR and the public URL are in there)")
		if clone == "" {
			utils.Info("      corgi agent container shell  → git clone … /work/stack && cd /work/stack && corgi init && corgi agent init")
		}
		utils.Info("      then in the shell: corgi agent watch auth …, watch enable …, notify telegram … (docs/agent.md › Running it on a server)")
		return nil
	case "shell":
		c := exec.Command("docker", "exec", "-it", "-w", "/work", containerName, "sh", "-lc", "cd /work/stack 2>/dev/null; exec bash || exec sh")
		c.Env, c.Stdin, c.Stdout, c.Stderr = env, os.Stdin, os.Stdout, os.Stderr
		return c.Run()
	case "logs":
		return compose(true, "logs", "-f", "--tail", "200")
	case "stop":
		return compose(false, "stop")
	case "down":
		utils.Info("the volumes (agent dir, clones, Claude login) stay; docker volume rm corgi-agent-data corgi-agent-work corgi-agent-claude removes them")
		return compose(false, "down")
	case "status":
		if _, err := os.Stat(filepath.Join(containerDir(dir), "compose.yml")); err != nil {
			utils.Info("no container yet - corgi agent container up")
			return nil
		}
		return compose(false, "ps")
	}
	return fmt.Errorf("container takes up, shell, logs, stop, down or status, not %q", action)
}

func writeContainerFiles(dir, clone string, withDocker bool) error {
	out := containerDir(dir)
	if err := os.MkdirAll(out, 0o700); err != nil {
		return err
	}
	extra := ""
	if withDocker {
		extra = "      - /var/run/docker.sock:/var/run/docker.sock\n"
	}
	envLines := []string{"TZ=" + firstNonEmpty(os.Getenv("TZ"), "UTC"), "CORGI_UP_FLAGS="}
	if clone != "" {
		envLines = append(envLines, "CORGI_CLONE="+clone)
	}
	files := map[string]string{
		"Dockerfile":    strings.Replace(containerDockerfile, "ARG CORGI_VERSION=", "ARG CORGI_VERSION="+APP_VERSION, 1),
		"entrypoint.sh": containerEntrypoint,
		"compose.yml":   fmt.Sprintf(containerCompose, extra),
	}
	for name, body := range files {
		mode := os.FileMode(0o600)
		if name == "entrypoint.sh" {
			mode = 0o700
		}
		if err := os.WriteFile(filepath.Join(out, name), []byte(body), mode); err != nil {
			return err
		}
	}
	envPath := filepath.Join(out, ".env")
	if existing, err := os.ReadFile(envPath); err == nil {
		for _, line := range strings.Split(string(existing), "\n") {
			if strings.HasPrefix(line, "CLAUDE_CODE_OAUTH_TOKEN=") || strings.HasPrefix(line, "GIT_AUTHOR_") {
				envLines = append(envLines, line)
			}
		}
	}
	return os.WriteFile(envPath, []byte(strings.Join(envLines, "\n")+"\n"), 0o600)
}

func ensureContainerToken(dir string) error {
	envPath := filepath.Join(containerDir(dir), ".env")
	existing, _ := os.ReadFile(envPath)
	if strings.Contains(string(existing), "CLAUDE_CODE_OAUTH_TOKEN=") {
		return nil
	}
	fmt.Fprint(os.Stderr, "Claude token for the container (claude setup-token prints one; empty = log in inside later): ")
	raw, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		utils.Info("no token - run `claude auth login` from `corgi agent container shell` once")
		return nil
	}
	f, err := os.OpenFile(envPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("CLAUDE_CODE_OAUTH_TOKEN=" + token + "\n")
	return err
}

func init() {
	agentContainerCmd.Flags().String("host", "", "Docker host to build and run on, e.g. ssh://me@build-1 (DOCKER_HOST)")
	agentContainerCmd.Flags().String("clone", "", "A git URL the container clones to /work/stack and registers at first start")
	agentContainerCmd.Flags().Bool("docker", false, "Mount the host's Docker socket so fixes can run the stack")
	agentCmd.AddCommand(agentContainerCmd)
}
