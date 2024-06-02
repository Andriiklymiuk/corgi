//go:build windows

package utils

import (
	"fmt"
)

func SendInterrupt() {
	fmt.Println("SendInterrupt is not implemented for windows")
}

func SendRestart() {
	fmt.Println("SendRestart is not implemented for windows")
}
