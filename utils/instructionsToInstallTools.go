package utils

type CommandInfo struct {
	Install string
	Check   string
}

var CommandInstructions = map[string]CommandInfo{
	"brew": {
		Install: `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`,
		Check:   "brew -v",
	},
	"yarn": {
		Install: "brew install yarn",
		Check:   "yarn -v",
	},
	"pnpm": {
		Install: "brew install pnpm",
		Check:   "pnpm -v",
	},
	"go": {
		Install: `brew install go`,
		Check:   "go version",
	},
	"bun": {
		Install: `brew install oven-sh/bun/bun`,
		Check:   "bun -v",
	},
	"deno": {
		Install: `brew install deno`,
		Check:   "deno --version",
	},
	"pg_dump": {
		Install: `brew install libpq`,
		Check:   "psql --version",
	},
}
