# Gotham Makefile.

GO          ?= go
GOOS        ?= $(shell $(GO) env GOOS)
GOARCH      ?= $(shell $(GO) env GOARCH)
CGO_ENABLED ?= 0
BIN_DIR     ?= bin
LDFLAGS     ?= -s -w

.PHONY: all build test lint migrate dev fmt clean

all: build

## build: build both binaries into bin/
build:
	@mkdir -p $(BIN_DIR)
	@echo "==> building $(BIN_DIR)/gotham"
	@CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/gotham ./cmd/gotham
	@echo "==> building $(BIN_DIR)/gotham-agent"
	@CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/gotham-agent ./cmd/gotham-agent

## test: run the unit test suite
test:
	@echo "==> go test ./..."
	@$(GO) test ./...

## lint: run golangci-lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint is not installed or not on PATH."; \
		echo "Install it with:"; \
		echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
		exit 1; \
	}
	@echo "==> golangci-lint run"
	@golangci-lint run

## migrate: apply database migrations (wired up in BE-0.3)
migrate:
	@echo "migrate: not implemented until BE-0.3"

## dev: run the local development environment (not implemented yet)
dev:
	@echo "dev: not implemented yet"

## fmt: format Go sources with gofmt
fmt:
	@echo "==> gofmt -l -w ."
	@gofmt -l -w .
	@test -z "$$(gofmt -l .)" || { echo "gofmt: unformatted files remain"; exit 1; }

## clean: remove build artifacts
clean:
	@echo "==> removing $(BIN_DIR)/"
	@rm -rf $(BIN_DIR)
