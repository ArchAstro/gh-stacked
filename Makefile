BINARY := gh-stacked
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
INSTALL_DIR := $(HOME)/.local/share/gh/extensions/gh-stacked

.PHONY: all build test test-race cover clean install uninstall lint fmt fmt-check vet check

all: build

build:
	go build $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

test-race:
	go test -race ./...

test-verbose:
	go test ./... -v

cover:
	go test ./... -cover

cover-html:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

clean:
	rm -f $(BINARY)
	rm -f coverage.out coverage.html

install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/

uninstall:
	rm -rf $(INSTALL_DIR)

lint:
	go vet ./...

fmt:
	go fmt ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Run 'make fmt' to format these files:"; gofmt -l .; exit 1)

vet:
	go vet ./...

# Development helpers
dev: fmt test build

# Run a quick check before committing
check: fmt-check lint test-race
