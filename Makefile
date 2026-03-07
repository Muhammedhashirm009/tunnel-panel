.PHONY: build build-server build-cli clean install dev

# Variables
BINARY_SERVER = tunnelpanel
BINARY_CLI = tunnelpanel-cli
BUILD_DIR = ./build
VERSION = 1.0.0
LDFLAGS = -s -w -X main.version=$(VERSION)

# Build both binaries
build: build-server build-cli

build-server:
	@echo "🔨 Building TunnelPanel server..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_SERVER) ./cmd/server/

build-cli:
	@echo "🔨 Building TunnelPanel CLI..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_CLI) ./cmd/cli/

# Install to system
install: build
	@echo "📦 Installing TunnelPanel..."
	sudo cp $(BUILD_DIR)/$(BINARY_SERVER) /usr/local/bin/$(BINARY_SERVER)
	sudo cp $(BUILD_DIR)/$(BINARY_CLI) /usr/local/bin/$(BINARY_CLI)
	sudo chmod +x /usr/local/bin/$(BINARY_SERVER)
	sudo chmod +x /usr/local/bin/$(BINARY_CLI)
	sudo mkdir -p /etc/tunnelpanel
	sudo mkdir -p /var/log/tunnelpanel
	@echo "✅ Installed to /usr/local/bin/"

# Development run
dev:
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
