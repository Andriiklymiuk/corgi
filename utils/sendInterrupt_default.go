//go:build !windows

package utils

import (
	"fmt"
	"syscall"
)

func sendInterrupt() {
	err := syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	if err != nil {
		fmt.Printf("Error sending SIGINT: %v\n", err)
	}
}
