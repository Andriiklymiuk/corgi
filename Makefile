fixHooks:
	chmod +x .githooks/pre-commit

# run it in public repo and add before GITHUB_TOKEN
release:
	goreleaser --rm-dist
tag:
	git describe --tags --abbrev=0

generateDocs:
	go run . docs --generate

VERSION := $(shell grep -E -o 'APP_VERSION\s*=\s*"[^"]*"' cmd/root.go | awk -F '"' '{print $$2}')

getVersion:
	echo $(VERSION)

getActionVersion:
	if [ -n "${GITHUB_ENV}" ]; then \
		echo "VERSION=$(shell grep -E -o 'APP_VERSION\s*=\s*"[^"]*"' cmd/root.go | awk -F '"' '{print $$2}')" >> "${GITHUB_ENV}"; \
	else \
		echo "GITHUB_ENV not set"; \
	fi

incrementVersionPatch:
	$(eval PATCH=$(shell echo $(VERSION) | cut -d '.' -f 3))
	$(eval NEW_PATCH=$(shell echo $$(($(PATCH) + 1))))
	sed -i "" "s/\(APP_VERSION = \"[0-9]*.[0-9]*.\)$(PATCH)\"/\1$(NEW_PATCH)\"/" cmd/root.go
	$(eval VERSION := $(shell grep -E -o 'APP_VERSION\s*=\s*"[^"]*"' cmd/root.go | awk -F '"' '{print $$2}'))

incrementVersionMinor:
	$(eval MINOR=$(shell echo $(VERSION) | cut -d '.' -f 2))
	$(eval PATCH=$(shell echo $(VERSION) | cut -d '.' -f 3))
	$(eval NEW_MINOR=$(shell echo $$(($(MINOR) + 1))))
	sed -i "" "s/\(APP_VERSION = \"[0-9]*.\)$(MINOR).$(PATCH)\"/\1$(NEW_MINOR).0\"/" cmd/root.go
	$(eval VERSION := $(shell grep -E -o 'APP_VERSION\s*=\s*"[^"]*"' cmd/root.go | awk -F '"' '{print $$2}'))

incrementVersionMajor:
	$(eval MAJOR=$(shell echo $(VERSION) | cut -d '.' -f 1))
	$(eval NEW_MAJOR=$(shell echo $$(($(MAJOR) + 1))))
	sed -i "" "s/\(APP_VERSION = \"\)$(MAJOR).[0-9]*.[0-9]*\"/\1$(NEW_MAJOR).0.0\"/" cmd/root.go
	$(eval VERSION := $(shell grep -E -o 'APP_VERSION\s*=\s*"[^"]*"' cmd/root.go | awk -F '"' '{print $$2}'))


.PHONY: \
fixHooks \
release \
tag \
generateDocs \
getVersion \
getActionVersion \
incrementVersionPatch \
incrementVersionMinor \
incrementVersionMajor