.PHONY: test test-unit test-integration test-stress test-race test-all test-coverage test-bench clean help

# Default target
.DEFAULT_GOAL := help

## test: Run unit tests only
test:
	@echo "Running unit tests..."
	@go test -v -short -timeout 30s ./...

## test-unit: Run unit tests with coverage
test-unit:
	@echo "Running unit tests with coverage..."
	@go test -v -short -timeout 30s -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out

## test-integration: Run integration tests (BadgerDB + Mock)
test-integration:
	@echo "Running integration tests..."
	@go test -v -run "Integration" -timeout 2m ./...

## test-stress: Run stress tests
test-stress:
	@echo "Running stress tests..."
	@go test -v -run "Stress" -timeout 5m ./...

## test-race: Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	@go test -race -run "Race" -timeout 2m ./...

## test-all: Run all tests (unit, integration, stress, race)
test-all:
	@echo "Running all tests..."
	@go test -v -timeout 10m ./...
	@echo ""
	@echo "Running race detector tests..."
	@go test -race -run "Race" -timeout 2m ./...

## test-coverage: Generate coverage report and open in browser
test-coverage:
	@echo "Generating coverage report..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@open coverage.html 2>/dev/null || xdg-open coverage.html 2>/dev/null || echo "Please open coverage.html in your browser"

## test-bench: Run benchmark tests
test-bench:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem -run=^$$ ./...

## test-bench-compare: Run benchmarks and save results
test-bench-compare:
	@echo "Running benchmarks and saving results..."
	@go test -bench=. -benchmem -run=^$$ ./... | tee bench.txt

## clean: Remove test artifacts
clean:
	@echo "Cleaning test artifacts..."
	@rm -f coverage.out coverage.html bench.txt
	@rm -rf ./queue_data
	@find . -name "*.test" -type f -delete
	@echo "Clean complete"

## test-quick: Run quick smoke tests
test-quick:
	@echo "Running quick smoke tests..."
	@go test -short -timeout 10s ./...

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "Running tests with verbose output..."
	@go test -v -timeout 5m ./...

## test-store-badger: Run tests specifically for BadgerDB store
test-store-badger:
	@echo "Running BadgerDB store tests..."
	@go test -v -run "Badger" -timeout 2m ./...

## test-store-mock: Run tests specifically for Mock store
test-store-mock:
	@echo "Running Mock store tests..."
	@go test -v -run "Mock" -timeout 1m ./...

## test-parallel: Run tests in parallel with maximum parallelism
test-parallel:
	@echo "Running tests in parallel..."
	@go test -v -parallel 8 -timeout 5m ./...

## test-count: Run tests multiple times to check for flaky tests
test-count:
	@echo "Running tests 10 times to check for flakiness..."
	@go test -count=10 -short -timeout 2m ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

## fmt: Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...

## lint: Run linter (requires golangci-lint)
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Install from https://golangci-lint.run/usage/install/" && exit 1)
	@golangci-lint run ./...

## check: Run fmt, vet, and tests
check: fmt vet test

## ci: Run all checks for CI (fmt, vet, test, race)
ci: fmt vet test-all test-race
	@echo "CI checks passed!"

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

## help: Show this help message
help:
	@echo "Available targets:"
	@echo ""
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'
	@echo ""
	@echo "Examples:"
	@echo "  make test           # Run unit tests"
	@echo "  make test-all       # Run all tests"
	@echo "  make test-race      # Run race detector"
	@echo "  make test-coverage  # Generate coverage report"
	@echo "  make ci             # Run all CI checks"
