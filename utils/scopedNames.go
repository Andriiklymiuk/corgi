package utils

import "path/filepath"

var containerScope string

func SetContainerScope(c *CorgiCompose) {
	if c == nil || !c.ScopeContainers {
		containerScope = ""
		return
	}
	base := c.Name
	if base == "" {
		base = filepath.Base(CorgiComposePathDir)
	}
	containerScope = DockerSafeName(base)
}

func ContainerScope() string { return containerScope }

func ScopedContainerBase(name string) string {
	if containerScope == "" {
		return name
	}
	return containerScope + "-" + name
}

func ServiceContainerName(serviceName string) string {
	return DockerSafeName(ScopedContainerBase(serviceName))
}
