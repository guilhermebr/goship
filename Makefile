# GoShip Makefile

.PHONY: all build clean test lint fmt vet tidy help
.PHONY: build-goshipctl

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

# Build directories
BUILD_DIR=bin
CMD_DIR=cmd

# Version info
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LD_VERSION_FLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)
LDFLAGS := -ldflags "$(LD_VERSION_FLAGS)"

# Default target
all: build

# Build all binaries
build: build-goshipctl

# Build goshipctl CLI
build-goshipctl:
	@echo "Building $(GOSHIPCTL)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(GOSHIPCTL) ./$(CMD_DIR)/$(GOSHIPCTL)

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
	@echo "  test             - Run tests"
	@echo "  test-coverage    - Run tests with coverage"
	@echo "  fmt              - Format code"
	@echo "  vet              - Vet code"
	@echo "  lint             - Lint code"
	@echo "  tidy             - Tidy dependencies"
	@echo "  clean            - Clean build artifacts"
	@echo "  help             - Show this help"
