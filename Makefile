.PHONY: build build-server build-cli clean install dev deps

# Variables
BINARY_SERVER = portix
BINARY_CLI = portix-cli
BUILD_DIR = ./build
VERSION = 1.0.0
LDFLAGS = -s -w -X main.version=$(VERSION)

# Resolve dependencies first
deps:
	@echo "📦 Resolving dependencies..."
	go mod tidy

# Build both binaries
build: deps build-server build-cli

build-server:
	@echo "🔨 Building Portix server..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_SERVER) ./cmd/server/

build-cli:
	@echo "🔨 Building Portix CLI..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_CLI) ./cmd/cli/

# Install to system
install: build
	@echo "📦 Installing Portix..."
	sudo cp $(BUILD_DIR)/$(BINARY_SERVER) /usr/local/bin/$(BINARY_SERVER)
	sudo cp $(BUILD_DIR)/$(BINARY_CLI) /usr/local/bin/$(BINARY_CLI)
	sudo chmod +x /usr/local/bin/$(BINARY_SERVER)
	sudo chmod +x /usr/local/bin/$(BINARY_CLI)
	sudo mkdir -p /etc/portix
	sudo mkdir -p /var/log/portix
	@echo "✅ Installed to /usr/local/bin/"

# Development run
dev: deps
	@echo "🚀 Starting dev server..."
	go run ./cmd/server/

# Clean build artifacts
clean:
	@echo "🧹 Cleaning..."
	rm -rf $(BUILD_DIR)

# Run tests
test:
	go test ./internal/... -v

# Format code
fmt:
	gofmt -w .
	goimports -w .
