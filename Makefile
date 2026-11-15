# ykode — the commands this project runs, written down.

GO ?= go
BINARY := bin/ykode
ARGS ?=
PREFIX ?= $(HOME)/.local
INSTALL_DIR ?= $(PREFIX)/bin
# the version --version prints comes from git, so a build can be named later
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build vet test check cover fmt run install uninstall release clean

all: build check

build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/ykode

vet:
	$(GO) vet ./...

test:
	$(GO) test ./... -count=1

check: vet fmtcheck test

# the check target reports formatting rather than only offering to fix it: a file that never went
# through gofmt drifts one hunk at a time, and the next diff is harder to read for it
fmtcheck:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "gofmt: $$unformatted"; exit 1; fi

cover:
	$(GO) test ./... -count=1 -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out | tail -1

fmt:
	gofmt -w .

# lint is not part of check, and that is deliberate: golangci-lint is not vendored, and a build gate
# that fails because a machine lacks a third-party tool is a gate people stop trusting. It reports what
# vet does not - a helper nobody calls, a Close whose error nobody looks at. Where it is missing it says
# so and succeeds, so CI can call it either way and the difference stays visible.
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; \
	else echo "lint: golangci-lint is not installed; the findings it makes are unreported"; fi

run: build
	./$(BINARY) $(ARGS)

install: build
	install -d $(INSTALL_DIR)
	install -m 0755 $(BINARY) $(INSTALL_DIR)/ykode
	@echo "installed $(INSTALL_DIR)/ykode (version $(VERSION)); make sure $(INSTALL_DIR) is on PATH; settings and sessions land in ~/.ykode on the first run"

# make uninstall PURGE=1 also removes ~/.ykode (settings, sessions, logs)
uninstall:
	rm -f $(INSTALL_DIR)/ykode
	@if [ -n "$(PURGE)" ]; then rm -rf "$${YKODE_HOME:-$(HOME)/.ykode}"; echo "removed $(INSTALL_DIR)/ykode and ~/.ykode"; else echo "removed $(INSTALL_DIR)/ykode; settings and logs kept"; fi

# release cross-builds the binaries one tagged version ships as
release:
	@mkdir -p dist
	@for target in linux/amd64 linux/arm64 darwin/arm64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -ldflags '$(LDFLAGS)' -o dist/ykode-$$os-$$arch-$(VERSION) ./cmd/ykode; \
	done
	@ls -1 dist

clean:
	rm -rf bin dist coverage.out
