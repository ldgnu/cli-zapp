# Makefile for wterm.
#
# The targets are ordered from fastest to slowest, so `make` alone gives the
# useful feedback loop: format, vet, unit tests, then the slower lint.

GO      ?= go
BIN     ?= wterm
BINDIR  ?= $(CURDIR)/bin
PKG     := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# CGO is disabled for builds only, not globally: modernc.org/sqlite is a pure-Go
# SQLite, so the shipped binary is static and runs on any Linux of the same
# architecture with no libc version to match. The race detector, however, needs
# cgo, so exporting CGO_ENABLED=0 at the top would break `make test`.
CGO_ENABLED ?= 0
export CGO_ENABLED

.DEFAULT_GOAL := help

## help: show this help
.PHONY: help
help: ## Show this help.
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## fmt: format all Go source
.PHONY: fmt
fmt: ## Format all Go source.
	$(GO) fmt $(PKG)

## fmt-check: fail if anything is unformatted
.PHONY: fmt-check
fmt-check: ## Fail if any file is unformatted.
	@unformatted=$$(gofmt -l ./cmd ./internal); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

## vet: run go vet
.PHONY: vet
vet: ## Run go vet.
	$(GO) vet $(PKG)

## test: run unit tests with the race detector
.PHONY: test
test: ## Run unit tests with the race detector.
	CGO_ENABLED=1 $(GO) test -race $(PKG)

## test-plain: run unit tests without the race detector
.PHONY: test-plain
test-plain: ## Run unit tests without the race detector.
	$(GO) test $(PKG)

## test-cover: run tests and report coverage
.PHONY: test-cover
test-cover: ## Run tests and report coverage per package.
	$(GO) test -coverprofile=coverage.out $(PKG)
	$(GO) tool cover -func=coverage.out | tail -1

## lint: run golangci-lint
.PHONY: lint
lint: ## Run golangci-lint.
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed; run: make tools"; exit 1; \
	fi

## lint-fix: run golangci-lint with autofix
.PHONY: lint-fix
lint-fix: ## Run golangci-lint with autofix.
	golangci-lint run --fix

## tools: install the development tools
.PHONY: tools
tools: ## Install development tools.
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

## build: build the binary
.PHONY: build
build: ## Build the binary into bin/.
	@mkdir -p $(BINDIR)
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINDIR)/$(BIN) ./cmd/wterm
	@echo "built $(BINDIR)/$(BIN) ($(VERSION))"

## install: install the binary into GOBIN
.PHONY: install
install: ## Install the binary into GOBIN.
	$(GO) install -trimpath -ldflags '$(LDFLAGS)' ./cmd/wterm

## run: build and run against demo data
.PHONY: run
run: build ## Build and run with demo data.
	$(BINDIR)/$(BIN)

## keys: print the default keybindings as TOML
.PHONY: keys
keys: build ## Print the default keybindings in TOML form.
	$(BINDIR)/$(BIN) --help-keys

## tidy: tidy go.mod
.PHONY: tidy
tidy: ## Tidy go.mod and go.sum.
	$(GO) mod tidy

## vendor: vendor dependencies for a reproducible build
.PHONY: vendor
vendor: ## Vendor dependencies.
	$(GO) mod vendor

## check: everything CI runs
.PHONY: check
check: fmt-check vet test lint ## Run every check CI runs.

## clean: remove build artefacts
.PHONY: clean
clean: ## Remove build artefacts.
	rm -rf $(BINDIR) coverage.out

## help-all: show every target
.PHONY: help-all
help-all: ## Show every target.
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort
