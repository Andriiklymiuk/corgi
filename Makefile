fixHooks:
	chmod +x .githooks/pre-commit

# run it in public repo and add before GITHUB_TOKEN
release:
	goreleaser --rm-dist
tag:
	git describe --tags --abbrev=0

generateDocs:
	go run . docs -g

VERSION := $(shell grep -E -o 'APP_VERSION\s*=\s*"[^"]*"' cmd/root.go | awk -F '"' '{print $$2}')

getVersion:
	echo $(VERSION)

getActionVersion:
	if [ -n "${GITHUB_ENV}" ]; then \
		echo "VERSION=$(shell grep -E -o 'APP_VERSION\s*=\s*"[^"]*"' cmd/root.go | awk -F '"' '{print $$2}')" >> "${GITHUB_ENV}"; \
	else \
		echo "GITHUB_ENV not set"; \
	fi

.PHONY: fixHooks release tag generateDocs getVersion getActionVersion