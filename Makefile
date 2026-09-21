# Runnerly
#
# Run `make help` for the available targets.

BINARY      := runnerly
CMD         := ./cmd/runnerly
AGENT       := runnerly-agent
AGENT_CMD   := ./cmd/runnerly-agent
SERVER      := runnerly-server
SERVER_CMD  := ./cmd/runnerly-server
DIST        := dist
PREFIX      ?= /usr/local

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

PKG         := github.com/bablilayoub/runnerly/internal/version
LDFLAGS     := -s -w \
	-X $(PKG).Version=$(VERSION) \
	-X $(PKG).Commit=$(COMMIT) \
	-X $(PKG).Date=$(DATE)

GO          ?= go
# node_modules can contain vendored Go source that is not ours; the go tool
# ignores it and so should the format check.
GOFILES     := $(shell find . -name '*.go' -not -path './$(DIST)/*' \
	-not -path './web/node_modules/*' -not -path './site/node_modules/*')

NPM         ?= npm
WEB         := web
WEB_DIST    := internal/web/dist
SITE        := site
SITE_DIST   := site/dist

# Pinned so a new linter release cannot turn a green checkout red. CI runs
# `make lint`, so this is the only place the version is written down.
# No install step: `go run` fetches it into the module cache once.
# Override with an installed binary: make lint GOLANGCI_LINT=golangci-lint
GOLANGCI_LINT_VERSION ?= v2.13.2
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

# Same pinning for the workflow linter, which CI runs from the same version.
ACTIONLINT_VERSION ?= v1.7.7

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: web
web: ## Build the dashboard into internal/web/dist (needs Node)
	@command -v $(NPM) >/dev/null 2>&1 || { \
		echo "npm is not installed, so the dashboard cannot be built."; \
		echo "The Go build works without it; the server then serves a page saying so."; \
		exit 1; \
	}
	cd $(WEB) && $(NPM) ci --no-fund --no-audit
	cd $(WEB) && $(NPM) run build

.PHONY: web-check
web-check: ## Typecheck, lint and format-check the dashboard
	cd $(WEB) && $(NPM) run typecheck
	cd $(WEB) && $(NPM) run lint
	cd $(WEB) && $(NPM) run format:check

.PHONY: site
site: ## Build the landing page into site/dist (needs Node)
	cd $(SITE) && $(NPM) ci --no-fund --no-audit
	cd $(SITE) && $(NPM) run build

.PHONY: site-dev
site-dev: ## Run the landing page with hot reload
	cd $(SITE) && $(NPM) run dev

.PHONY: site-check
site-check: ## Typecheck and lint the landing page
	cd $(SITE) && $(NPM) run typecheck
	cd $(SITE) && $(NPM) run lint

.PHONY: site-clean
site-clean: ## Remove the built landing page
	rm -rf $(SITE_DIST)

.PHONY: web-clean
web-clean: ## Remove the built dashboard
	find $(WEB_DIST) -mindepth 1 ! -name .gitkeep -delete

.PHONY: all
all: web build ## Build the dashboard and then the binaries

.PHONY: build
build: ## Build all three binaries into dist/ (run `make web` first to include the dashboard)
	@mkdir -p $(DIST)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BINARY) $(CMD)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(AGENT) $(AGENT_CMD)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(SERVER) $(SERVER_CMD)

.PHONY: install
install: build ## Install all three binaries to PREFIX/bin (default /usr/local)
	install -m 0755 $(DIST)/$(BINARY) $(PREFIX)/bin/$(BINARY)
	install -m 0755 $(DIST)/$(AGENT) $(PREFIX)/bin/$(AGENT)
	install -m 0755 $(DIST)/$(SERVER) $(PREFIX)/bin/$(SERVER)

.PHONY: run
run: ## Build and run (make run ARGS="doctor --offline")
	@$(MAKE) --no-print-directory build
	@$(DIST)/$(BINARY) $(ARGS)

.PHONY: test
test: ## Run unit tests
	$(GO) test ./...

.PHONY: test-race
test-race: ## Run unit tests with the race detector
	$(GO) test -race ./...

.PHONY: cover
cover: ## Run tests and open a coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: fmt
fmt: ## Format all Go code
	$(GO) fmt ./...

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is unformatted
	@unformatted=$$(gofmt -l $(GOFILES)); \
	if [ -n "$$unformatted" ]; then \
		echo "These files are not gofmt'd:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint (fetches the pinned version on first use)
	$(GOLANGCI_LINT) run

# Build tags hide code from the linter: running it here only ever checks the
# files that build for this machine. A gosec finding in a linux-only file
# sailed past a clean local run and failed CI, so every platform Runnerly
# compiles for gets a pass of its own.
#
# The linter is built once, natively, because GOOS applies to what `go run`
# compiles as well as to what the tool then analyses: setting it on `go run`
# produces a linter for the target platform, which this machine cannot
# execute.
LINT_PLATFORMS ?= linux/amd64 linux/arm64 darwin/arm64 windows/amd64
GOLANGCI_LINT_BIN := $(DIST)/golangci-lint

$(GOLANGCI_LINT_BIN):
	@mkdir -p $(DIST)
	GOOS= GOARCH= GOBIN=$(abspath $(DIST)) \
		$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: lint-platforms
lint-platforms: $(GOLANGCI_LINT_BIN) ## Run golangci-lint once per supported platform
	@for target in $(LINT_PLATFORMS); do \
		echo "==> golangci-lint $$target"; \
		GOOS=$${target%/*} GOARCH=$${target#*/} $(GOLANGCI_LINT_BIN) run || exit 1; \
	done

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

.PHONY: lint-actions
lint-actions: ## Check the GitHub workflows (runs shellcheck over run: blocks)
	$(GO) run github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

.PHONY: lint-sh
lint-sh: ## Check install.sh with shellcheck, when it is installed
	@command -v shellcheck >/dev/null 2>&1 \
		&& shellcheck --shell=sh install.sh \
		|| { echo "shellcheck is not installed; checking syntax only."; sh -n install.sh; }

.PHONY: check
check: fmt-check vet lint-platforms lint-sh lint-actions test ## Everything CI runs

.PHONY: clean
clean: web-clean site-clean ## Remove build artifacts
	rm -rf $(DIST) coverage.out
