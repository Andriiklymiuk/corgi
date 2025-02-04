package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"fmt"
)

func PrintFinalMessage() {
	fmt.Println(
		"\n✨ Thanks for using me ✨ See you next time 🚀 🐶",
		string("\n\n\033[36m"),
		GetRandomQuote(),
		art.WhiteColor,
	)
}
