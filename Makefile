BINARY=chawrtd
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -X 'chawrtd/internal/version.Version=$(VERSION)' -X 'chawrtd/internal/version.GitCommit=$(GIT_COMMIT)' -X 'chawrtd/internal/version.BuildTime=$(BUILD_TIME)'

.PHONY: build run fmt clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/chawrtd

run:
	go run ./cmd/chawrtd

fmt:
	gofmt -w ./cmd ./internal

clean:
	rm -rf bin/
