# GoShip Makefile

.PHONY: all build clean test lint fmt vet tidy help install uninstall
.PHONY: build-goship build-goship-init release-local release-check setup

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOVET=$(GOCMD) vet

# Binary names
GOSHIP=goship
GOSHIP_INIT=goship-init

# Build directories
BUILD_DIR=bin
CMD_DIR=cmd

# Install directories
PREFIX ?= $(HOME)/.local
BINDIR = $(PREFIX)/bin

# Version info
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LD_VERSION_FLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)
LDFLAGS := -ldflags "$(LD_VERSION_FLAGS)"

# Install mise and project toolchain (Go, golangci-lint, etc.)
setup:
	@command -v mise >/dev/null 2>&1 || { echo "Installing mise..."; curl https://mise.run | sh; }
	@echo "Installing project tools via mise..."
	mise install

# Default target
all: build

# Build all binaries
build: build-goship build-goship-init

# Build goship (CLI + API server)
build-goship:
	@echo "Building $(GOSHIP)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 $(GOBUILD) $(LDFLAGS) -tags libvirt_dlopen -o $(BUILD_DIR)/$(GOSHIP) ./$(CMD_DIR)/$(GOSHIP)

# Build goship-init (static binary for use inside VMs)
build-goship-init:
	@echo "Building $(GOSHIP_INIT) (static)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(GOSHIP_INIT) ./$(CMD_DIR)/$(GOSHIP_INIT)

# Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Format code (golangci-lint v2 handles gci, gofmt, gofumpt, goimports, golines)
fmt:
	@echo "Formatting code..."
	golangci-lint fmt ./...

# Vet code
vet:
	@echo "Vetting code..."
	$(GOVET) ./...

# Lint code (requires golangci-lint v2)
lint:
	@echo "Linting code..."
	golangci-lint run ./...

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy

# Install binaries
install: build
	@echo "Installing GoShip..."
	install -d $(BINDIR)
	install -m 755 $(BUILD_DIR)/$(GOSHIP) $(BINDIR)/$(GOSHIP)
	install -d $(HOME)/.goship/bin
	install -m 755 $(BUILD_DIR)/$(GOSHIP_INIT) $(HOME)/.goship/bin/$(GOSHIP_INIT)
	@echo "GoShip installed successfully."
	@echo "  $(BINDIR)/$(GOSHIP)"
	@echo "  $(HOME)/.goship/bin/$(GOSHIP_INIT)"

# Remove installed binaries
uninstall:
	@echo "Removing GoShip..."
	rm -f $(BINDIR)/$(GOSHIP)
	rm -f $(HOME)/.goship/bin/$(GOSHIP_INIT)
	@echo "GoShip uninstalled."

# GoReleaser: local snapshot release (dry run)
release-local:
	@echo "Building local snapshot release..."
	goreleaser release --snapshot --clean

# GoReleaser: validate config
release-check:
	@echo "Validating .goreleaser.yml..."
	goreleaser check

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Show help
help:
	@echo "GoShip Makefile targets:"
	@echo ""
	@echo "  all              - Build all binaries (default)"
	@echo "  build            - Build all binaries"
	@echo "  build-goship     - Build goship (CLI + API server)"
	@echo "  build-goship-init - Build goship-init (static, for VMs)"
	@echo "  test             - Run tests"
	@echo "  test-coverage    - Run tests with coverage"
	@echo "  fmt              - Format code"
	@echo "  vet              - Vet code"
	@echo "  lint             - Lint code"
	@echo "  tidy             - Tidy dependencies"
	@echo "  install          - Build and install to $(PREFIX)/bin"
	@echo "  uninstall        - Remove installed binaries"
	@echo "  clean            - Clean build artifacts"
	@echo "  release-local    - Build snapshot release locally (dry run)"
	@echo "  release-check    - Validate .goreleaser.yml config"
	@echo "  setup            - Install mise and project tools (Go, etc.)"
	@echo "  help             - Show this help"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION          - Release version (default: dev)"
	@echo "  PREFIX           - Install prefix (default: /usr/local)"
	@echo ""
