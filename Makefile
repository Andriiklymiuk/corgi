fixHooks:
	chmod +x .githooks/pre-commit

# run it in public repo and add before GITHUB_TOKEN
release:
	goreleaser --rm-dist
tag:
	git describe --tags --abbrev=0

generateDocs:
	go run . docs -g

.PHONY: fixHooks release tag generateDocs