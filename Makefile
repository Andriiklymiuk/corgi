fixHooks:
	chmod +x .githooks/pre-commit

# run it in public repo and add before GITHUB_TOKEN
release:
	goreleaser --rm-dist
getLatestTag:
	git describe --tags --abbrev=0

.PHONY: fixHooks release getLatestTag