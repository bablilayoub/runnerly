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
GOFILES     := $(shell find . -name '*.go' -not -path './$(DIST)/*')

# Pinned so a new linter release cannot turn a green checkout red. CI runs
# `make lint`, so this is the only place the version is written down.
# No install step: `go run` fetches it into the module cache once.
# Override with an installed binary: make lint GOLANGCI_LINT=golangci-lint
GOLANGCI_LINT_VERSION ?= v2.13.2
GOLANGCI_LINT ?= $(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[1m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build all three binaries into dist/
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

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

.PHONY: check
check: fmt-check vet lint test ## Everything CI runs

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(DIST) coverage.out
