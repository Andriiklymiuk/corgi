package utils

import (
	"fmt"

	"github.com/manifoldco/promptui"
)

type PickPromptOptionSetter func(*PickPromptOptions)

type PickPromptOptions struct {
	backStringAtTheEnd bool
}

func WithBackStringAtTheEnd() PickPromptOptionSetter {
	return func(opts *PickPromptOptions) {
		opts.backStringAtTheEnd = true
	}
}

func PickItemFromListPrompt(label string, items []string, backString string, setters ...PickPromptOptionSetter) (string, error) {
	opts := &PickPromptOptions{}
	for _, setter := range setters {
		setter(opts)
	}

	// Add backString based on the options
	if opts.backStringAtTheEnd {
		items = append(items, backString)
	} else {
		items = append([]string{backString}, items...)
	}

	prompt := promptui.Select{
		Label: label,
		Items: items,
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
