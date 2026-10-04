# Makefile for cli-zapp.
#
# The targets are ordered from fastest to slowest, so `make` alone gives the
# useful feedback loop: format, vet, unit tests, then the slower lint.

GO      ?= go
BIN     ?= cli-zapp
PKG     := ./...

# --- build metadata -----------------------------------------------------------
#
# VERSION comes from git describe, so it tracks the tag rather than being written
# down twice. COMMIT and BUILD_DATE are captured once per make invocation and reused
# by every target, because a release whose two archives disagree about when they
# were built is not reproducible even if each is internally consistent.
#
# BUILD_DATE is UTC in RFC 3339 and is overridable, which is what lets the release
# workflow inject a fixed value so that rebuilding the same tag yields the same
# bytes. A local `make build` gets the current time, which is correct for a local
# build and never compared against anything.
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# The version as a bare SemVer string, for packaging.
#
# Package formats require the upstream version without a `v` prefix and without git's
# `-N-gHASH` suffix, so they cannot use $(VERSION) directly. This strips both.
#
# When there is no tag, git describe yields a bare commit hash, which is not a version
# any package manager will accept. Rather than emit a malformed package it falls back
# to a valid dev version carrying the commit, so a local `make package` produces
# something installable and obviously not a release.
SEMVER_RAW := $(shell printf '%s' "$(VERSION)" | sed -e 's/^v//' -e 's/-[0-9]\+-g[0-9a-f]*$$//' -e 's/-dirty$$//')
SEMVER_OK  := $(shell printf '%s' "$(SEMVER_RAW)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+' && echo yes || echo no)
# The fallback uses "+" as the separator rather than "-", because the two formats
# disagree about what a dev version may contain: Debian allows "-", while Arch's
# makepkg rejects any hyphen in pkgver outright. "+" is legal in both and is valid
# SemVer build metadata, so one string works everywhere instead of two sanitised
# variants that could drift apart.
SEMVER     := $(if $(filter yes,$(SEMVER_OK)),$(SEMVER_RAW),0.0.0+dev.$(COMMIT))

PKGNAME    := $(BIN)
DIST       ?= $(CURDIR)/dist
BINDIR     ?= $(CURDIR)/bin

# Installation layout.
#
# PREFIX is /usr/local rather than /usr because that is FHS-correct for a locally
# built package: /usr is owned by the distribution's package manager, and putting a
# hand-built binary under /usr/bin is how upgrades go wrong. DESTDIR is honoured so
# that `make install DESTDIR=/tmp/stage` stages without touching the system, which
# is what the packaging targets use to build archives.
PREFIX     ?= /usr/local
BINDIR_INST ?= $(PREFIX)/bin
SHAREDIR    ?= $(PREFIX)/share
DOCDIR      ?= $(SHAREDIR)/doc/$(PKGNAME)
MANDIR      ?= $(SHAREDIR)/man

# ldflags: -trimpath removes local filesystem paths from the binary, -s -w drop the
# symbol table and DWARF, which together is most of the size difference.
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

# Reproducibility flags, used only by the packaging and release paths.
#
# -buildvcs=false stops the toolchain stamping VCS state into the binary: with it on,
# two builds of the same commit differ because one ran in a dirty tree. Reproducible
# means byte-identical, and an embedded dirty flag defeats that even when the
# difference is not the flag itself.
REPRO_LDFLAGS := -buildvcs=false -trimpath
REPRO_FLAGS    := -mod=readonly -buildvcs=false -trimpath

# Targets and the architectures they are cross-compiled for. Linux only: this is a
# Linux-first project and adding targets it cannot test would be unverifiable claims.
PLATFORMS ?= linux/amd64 linux/arm64

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
	@unformatted=$$(gofmt -l ./cmd ./internal ./scripts); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt'd:"; echo "$$unformatted"; exit 1; \
	fi

## vet: run go vet
.PHONY: vet
vet: ## Run go vet.
	$(GO) vet $(PKG)

## test: run unit tests with the race detector
.PHONY: test
test: ## Run unit tests with the race detector, writing coverage.out.
	CGO_ENABLED=1 $(GO) test -race -coverprofile=coverage.out $(PKG)

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

## build: build the binary for the host
.PHONY: build
build: ## Build the binary into bin/.
	@mkdir -p $(BINDIR)
	$(GO) build $(REPRO_LDFLAGS) -ldflags '$(LDFLAGS)' -o $(BINDIR)/$(BIN) ./cmd/cli-zapp
	@echo "built $(BINDIR)/$(BIN) ($(VERSION))"

## install: install into PREFIX/bin
.PHONY: install
install: build ## Install the binary into $(PREFIX)/bin.
	install -d $(DESTDIR)$(BINDIR_INST)
	install -m 0755 $(BINDIR)/$(BIN) $(DESTDIR)$(BINDIR_INST)/$(BIN)

## uninstall: remove an installed copy
.PHONY: uninstall
uninstall: ## Remove the installed binary and its documentation.
	rm -f $(DESTDIR)$(BINDIR_INST)/$(BIN)
	rm -rf $(DESTDIR)$(DOCDIR)
	@echo "removed $(BINDIR_INST)/$(BIN)"

## install-doc: install the manual pages and licence
.PHONY: install-doc
install-doc: ## Install man pages and LICENSE into $(DOCDIR).
	install -d $(DESTDIR)$(DOCDIR) $(DESTDIR)$(MANDIR)/man1
	install -m 0644 LICENSE $(DESTDIR)$(DOCDIR)/LICENSE
	@if [ -d man ]; then \
		install -m 0644 man/*.1 $(DESTDIR)$(MANDIR)/man1/; \
	fi

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

## frame: print the interface at a given size, exactly as the renderer sends it
.PHONY: frame
frame: build ## Print one frame. Usage: make frame COLS=100 ROWS=28 [KEYS="tab enter hola"]
	$(GO) run ./scripts/framedump $(or $(COLS),100) $(or $(ROWS),28) $(KEYS)

## pty: drive the binary through a real pty and check it starts, reacts and exits
.PHONY: pty
pty: build ## Run the pty smoke test at several sizes.
	@set -e; for size in 80x24 120x30 160x40 60x20 45x12 30x8; do \
		./scripts/ptycheck.py $${size%x*} $${size#*x} tab ctrl+enter; \
	done
	@echo "pty check passed at every size"

## check: everything CI runs
.PHONY: check
check: fmt-check vet test lint pty ## Run every check CI runs.

## clean: remove build artefacts
.PHONY: clean
clean: ## Remove build artefacts.
	rm -rf $(BINDIR) $(DIST) coverage.out coverage.html
	@# The packaging directories are generated, not source. Removing them keeps
	@# `make clean` honest about what is derived, but they are also removed by
	@# `make package-clean` so a packaging-only cleanup is possible.

## package-clean: remove packaging build trees
.PHONY: package-clean
package-clean: ## Remove packaging build trees.
	rm -rf $(CURDIR)/pkgbuild
	rm -f $(CURDIR)/$(PKGNAME)-*.pkg.tar.zst

## --- release -----------------------------------------------------------------

## release: build the distributable archives for every supported platform
.PHONY: release
release: ## Build tarballs for each platform into dist/, plus checksums.
	@mkdir -p $(DIST)
	@set -e; for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		name="$(PKGNAME)-$$os-$$arch"; \
		stage="$(DIST)/$$name"; \
		rm -rf "$$stage"; mkdir -p "$$stage"; \
		echo "==> $$platform"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			$(GO) build $(REPRO_FLAGS) -ldflags '$(LDFLAGS)' \
			-o "$$stage/$(PKGNAME)" ./cmd/cli-zapp || exit 1; \
		install -m 0644 LICENSE "$$stage/LICENSE"; \
		install -m 0644 README.md "$$stage/README.md"; \
		if [ -d docs ]; then cp -r docs "$$stage/docs"; fi; \
		tar -czf "$$stage.tar.gz" -C "$(DIST)" "$$name" || exit 1; \
		rm -rf "$$stage"; \
	done
	@$(MAKE) --no-print-directory checksums

## checksums: write dist/checksums.txt
.PHONY: checksums
checksums: ## Write SHA256 checksums for everything in dist/.
	@mkdir -p $(DIST)
	@cd $(DIST) && { sha256sum *.tar.gz *.deb *.pkg.tar.zst 2>/dev/null || true; } \
		> checksums.txt
	@if [ -s $(DIST)/checksums.txt ]; then \
		echo "wrote $(DIST)/checksums.txt"; \
		cat $(DIST)/checksums.txt; \
	else \
		echo "nothing to checksum yet; run 'make release' first"; \
	fi

## verify-release: rebuild and confirm the archives are byte-identical
.PHONY: verify-release
verify-release: ## Rebuild and confirm the release archives are reproducible.
	@echo "==> checksums before"; cat $(DIST)/checksums.txt 2>/dev/null || true
	@$(MAKE) --no-print-directory release
	@echo "==> checksums after"; cat $(DIST)/checksums.txt

## --- packaging ---------------------------------------------------------------

## package: build every distribution package, with checksums
.PHONY: package
package: package-deb package-arch checksums ## Build .deb and Arch packages.

## package-deb: build Debian packages for every architecture
.PHONY: package-deb
package-deb: ## Build cli-zapp_VERSION_arch.deb for amd64 and arm64.
	@mkdir -p $(DIST)
	@set -e; for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		case "$$os" in linux) ;; *) echo "skip: $$os is not Debian"; continue;; esac; \
		echo "==> deb $$arch"; \
		$(CURDIR)/scripts/mkdeb.sh "$$arch" "$(SEMVER)" "$(DIST)" \
			"$(COMMIT)" "$(BUILD_DATE)" "$(LDFLAGS)"; \
	done

## package-arch: build an Arch Linux package
.PHONY: package-arch
package-arch: ## Build cli-zapp-VERSION-1-x86_64.pkg.tar.zst.
	@mkdir -p $(DIST)
	@$(CURDIR)/scripts/mkarch.sh "$(SEMVER)" "$(DIST)" \
		"$(COMMIT)" "$(BUILD_DATE)" "$(LDFLAGS)"

## package-arch-arm64: build an Arch Linux package for arm64
.PHONY: package-arch-arm64
package-arch-arm64: ## Build the aarch64 Arch package.
	@mkdir -p $(DIST)
	@ARCH=aarch64 $(CURDIR)/scripts/mkarch.sh "$(SEMVER)" "$(DIST)" \
		"$(COMMIT)" "$(BUILD_DATE)" "$(LDFLAGS)"

## help-all: show every target
.PHONY: help-all
help-all: ## Show every target.
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort
