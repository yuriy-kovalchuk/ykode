# ykode — the commands this project runs, written down.

GO ?= go
BINARY := bin/ykode
ARGS ?=

.PHONY: all build vet test check cover fmt run clean

all: build check

build:
	$(GO) build -o $(BINARY) ./cmd/ykode

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

clean:
	rm -rf bin coverage.out
