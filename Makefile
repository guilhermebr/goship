# GoShip Makefile

.PHONY: all build clean test lint fmt vet tidy help install uninstall
.PHONY: build-goshipctl build-goship-init

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOVET=$(GOCMD) vet

# Binary names
GOSHIPCTL=goshipctl
GOSHIP_INIT=goship-init

# Build directories
BUILD_DIR=bin
CMD_DIR=cmd

# Install directories
PREFIX ?= /usr/local
BINDIR = $(PREFIX)/bin

# Version info
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LD_VERSION_FLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)
LDFLAGS := -ldflags "$(LD_VERSION_FLAGS)"

# Default target
all: build

# Build all binaries
build: build-goshipctl build-goship-init

# Build goshipctl CLI
build-goshipctl:
	@echo "Building $(GOSHIPCTL)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(GOSHIPCTL) ./$(CMD_DIR)/$(GOSHIPCTL)

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

# Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -s -w .

# Vet code
vet:
	@echo "Vetting code..."
	$(GOVET) ./...

# Lint code (requires golangci-lint)
lint:
	@echo "Linting code..."
	golangci-lint run

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GOMOD) tidy

# Install binaries to system path
install: build
	@echo "Installing GoShip to $(BINDIR)..."
	install -d $(BINDIR)
	install -m 755 $(BUILD_DIR)/$(GOSHIPCTL) $(BINDIR)/$(GOSHIPCTL)
	install -m 755 $(BUILD_DIR)/$(GOSHIP_INIT) $(BINDIR)/$(GOSHIP_INIT)
	@echo "GoShip installed successfully."
	@echo "  $(BINDIR)/$(GOSHIPCTL)"
	@echo "  $(BINDIR)/$(GOSHIP_INIT)"

# Remove installed binaries
uninstall:
	@echo "Removing GoShip from $(BINDIR)..."
	rm -f $(BINDIR)/$(GOSHIPCTL)
	rm -f $(BINDIR)/$(GOSHIP_INIT)
	@echo "GoShip uninstalled."

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
	@echo "  build-goshipctl  - Build goshipctl CLI"
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
	@echo "  help             - Show this help"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION          - Release version (default: dev)"
	@echo "  PREFIX           - Install prefix (default: /usr/local)"
