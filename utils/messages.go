package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"fmt"
)

func PrintBye() {
	fmt.Println(
		"\n✨ Glad for using me ✨ See you next time 🚀 🐶",
		string("\n\n\033[36m"),
		GetRandomQuote("famous-quotes"),
		art.WhiteColor,
	)
}
