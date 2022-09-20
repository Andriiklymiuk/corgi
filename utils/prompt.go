package utils

import (
	"fmt"

	"github.com/manifoldco/promptui"
)

func PickItemFromListPrompt(label string, items []string, backString string) (string, error) {
	prompt := promptui.Select{
		Label: label,
		Items: append([]string{backString}, items...),
		Size:  8,
	}

	_, result, err := prompt.Run()

	if err != nil {
		return "", fmt.Errorf("prompt failed to choose %s", err)
	}

	if result == backString {
		return "", fmt.Errorf(backString)
	}

	return result, nil
}
