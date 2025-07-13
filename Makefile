.PHONY: deploy build test clean help

# Deploy by merging main into stage
deploy:
	@echo "Starting deployment process..."
	@git fetch origin
	@git checkout main
	@git pull origin main
	@git checkout stage
	@git pull origin stage
	@git merge main
	@git push origin stage
	@git checkout main
	@echo "Deployment complete! Back on main branch."

# Build the application
build:
	@echo "Building subsoxy..."
	@go build -o subsoxy

# Run tests
test:
	@echo "Running tests..."
	@go test ./...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -f subsoxy

# Show help
help:
	@echo "Available targets:"
	@echo "  deploy  - Merge main into stage and push (deployment)"
	@echo "  build   - Build the application"
	@echo "  test    - Run tests"
	@echo "  clean   - Clean build artifacts"
	@echo "  help    - Show this help message"

# Default target
.DEFAULT_GOAL := help