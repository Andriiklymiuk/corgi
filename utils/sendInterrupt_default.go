//go:build !windows

package utils

import (
	"fmt"
	"syscall"
)

func SendInterrupt() {
	err := syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	if err != nil {
		fmt.Printf("Error sending SIGINT: %v\n", err)
	}
}

func SendRestart() {
	err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP)
	if err != nil {
		fmt.Printf("Error sending SIGHUP: %v\n", err)
	}
}
