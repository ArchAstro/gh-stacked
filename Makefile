BINARY := gh-stacked
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
INSTALL_DIR := $(HOME)/.local/share/gh/extensions/gh-stacked

.PHONY: all build test cover clean install uninstall lint fmt

all: build

build:
	go build $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

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
	golangci-lint run

fmt:
	go fmt ./...

# Development helpers
dev: fmt test build

# Run a quick check before committing
check: fmt lint test
