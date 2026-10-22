# Project configuration
BUILD_DIR=bin
BINARY_NAME=state_sync
MAIN_FILE=./cmd/main.go

# Version configuration (injected via -ldflags)
VERSION_PKG=statesync/internal/version
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "1.0.0")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS = -X '$(VERSION_PKG).Version=$(VERSION)' -X '$(VERSION_PKG).GitCommit=$(GIT_COMMIT)' -X '$(VERSION_PKG).BuildDate=$(BUILD_DATE)'

.PHONY: all build clean help version

# Default target
all: build

## build: Build the binary with version information.
build:
	@echo "⚙️  Building $(BINARY_NAME) (Version: $(VERSION), Commit: $(GIT_COMMIT), BuildDate: $(BUILD_DATE))..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_FILE)

## version: Show version variables that will be injected.
version:
	@echo "Version:    $(VERSION)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"

## clean: Remove built binary.
clean:
	@echo "🪄 Cleaning..."
	rm -rf $(BUILD_DIR)/$(BINARY_NAME)

## help: Show available targets.
help:
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
