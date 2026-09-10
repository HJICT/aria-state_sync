# Project configuration
BUILD_DIR=bin
BINARY_NAME=state-sync
MAIN_FILE=./cmd/main.go

# Version configuration (injected via -ldflags)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

LDFLAGS = -X 'main.Version=$(VERSION)' -X 'main.GitCommit=$(GIT_COMMIT)' -X 'main.BuildTime=$(BUILD_TIME)'

.PHONY: all build clean help version

# Default target
all: build

## build: Build the binary with version information.
build:
	@echo "⚙️  Building $(BINARY_NAME) (Version: $(VERSION), Commit: $(GIT_COMMIT), BuildTime: $(BUILD_TIME))..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_FILE)

## version: Show version variables that will be injected.
version:
	@echo "Version:    $(VERSION)"
	@echo "Git Commit: $(GIT_COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"

## clean: Remove built binary.
clean:
	@echo "🪄 Cleaning..."
	rm -rf $(BUILD_DIR)/$(BINARY_NAME)

## help: Show available targets.
help:
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
