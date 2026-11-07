# ykode — the commands this project runs, written down.

GO ?= go
BINARY := bin/ykode
ARGS ?=
INSTALL_DIR ?= $(HOME)/.local/bin
# the version --version prints comes from git, so a build can be named later
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build vet test check cover fmt run eval install uninstall release clean

all: build check

build:
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/ykode

vet:
	$(GO) vet ./...

test:
	$(GO) test ./... -count=1

check: vet test

cover:
	$(GO) test ./... -count=1 -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out | tail -1

fmt:
	gofmt -w .

run: build
	./$(BINARY) $(ARGS)

# eval runs the scripted tasks against a real server; ARGS filters with --only
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

clean:
	rm -rf bin dist coverage.out
