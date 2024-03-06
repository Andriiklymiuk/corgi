//go:build windows

package utils

import (
	"fmt"
)

func sendInterrupt() {
	fmt.Println("sendInterrupt is not implemented for windows")
}
