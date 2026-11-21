# ykode — the commands this project runs, written down.

GO ?= go
BINARY := bin/ykode
ARGS ?=
PREFIX ?= $(HOME)/.local
INSTALL_DIR ?= $(PREFIX)/bin
# the version --version prints comes from git, so a build can be named later
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# the scanner image: no Trivy on PATH, so a scan needs no more than the Docker already here
TRIVY_IMAGE ?= aquasec/trivy:latest

.PHONY: all build vet test check cover fmt run eval install uninstall release clean deps vuln audit install-hooks

all: build check

build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/ykode

vet:
	$(GO) vet ./...

test:
	$(GO) test ./... -count=1

check: vet fmtcheck test lint

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

# lint is part of check: the code that is merely still there - a helper nobody calls, a Close whose
# error nobody looks at - is how a retired feature turns into dead weight, and vet does not see it.
# golangci-lint is not vendored, so where it is missing the target says so and succeeds: a build gate
# that fails because a machine lacks a third-party tool is a gate people stop trusting.
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; \
	else echo "lint: golangci-lint is not installed; the findings it makes are unreported"; fi

# deps asks the proxy what the newest version of every module is, direct and indirect, and prints
# only the ones that have moved: the rest of the list is not an answer anyone acts on. It decides
# nothing and upgrades nothing. It reaches the network, which is why it is not part of check.
deps:
	@out=$$($(GO) list -m -u all | grep ' \[' || true); \
	if [ -n "$$out" ]; then echo "$$out"; \
	else echo "deps: every module is at the newest version the proxy knows"; fi

# vuln asks which known CVEs the versions this build resolves are exposed to, out of Trivy's
# database rather than one of ours. Docker keeps the scanner out of the toolchain, and the named
# volume stops its database being downloaded on every run. It reports rather than gates, and says
# so where Docker is missing - the same shape as lint, so no target depends on a machine's setup.
vuln:
	@if command -v docker >/dev/null 2>&1; then \
		docker run --rm -v "$(CURDIR):/src:ro" -v ykode-trivy-cache:/root/.cache/trivy \
			$(TRIVY_IMAGE) fs --scanners vuln --quiet /src; \
	else echo "vuln: docker is not installed; the dependencies were not scanned"; fi

# audit asks both questions about the dependencies in one go
audit: deps vuln

run: build
	./$(BINARY) $(ARGS)

# eval runs the scripted tasks against a real server; ARGS takes the eval's flags and ykode's
eval:
	$(GO) run ./cmd/eval $(ARGS)

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

# Points git at .githooks/: commit-msg checks each subject, pre-push re-checks main, tag names and the gate.
install-hooks:
	git config core.hooksPath .githooks && chmod +x .githooks/*

clean:
	rm -rf bin dist coverage.out
