package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"fmt"
)

func PrintFinalMessage() {
	fmt.Println(
		"\nâ¨ Glad for using me â¨ See you next time đ đś",
		string("\n\n\033[36m"),
		GetRandomQuote("famous-quotes"),
		art.WhiteColor,
	)
}
