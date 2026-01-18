.PHONY: all build test clean proto install-tools

# Variables
BINARY_NAME=ironclad-db
BUILD_DIR=bin
PROTO_DIR=pkg/api

all: build

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_DIR)/ironclad.proto

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/ironclad-db

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Download dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

# Run linter
lint:
	@echo "Running linter..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping..."; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run the application locally (requires Azure credentials)
run: build
	@echo "Running $(BINARY_NAME)..."
	$(BUILD_DIR)/$(BINARY_NAME)
