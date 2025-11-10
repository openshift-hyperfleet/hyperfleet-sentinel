.DEFAULT_GOAL := help

GO ?= go
GOFMT ?= gofmt

# Binary name
BINARY_NAME := sentinel

# Version information
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go build flags
LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.date=$(BUILD_DATE)

# Container tool (docker or podman)
CONTAINER_TOOL ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Code Generation

.PHONY: generate
generate: ## Generate OpenAPI client from HyperFleet API spec
	@echo "Generating OpenAPI client..."
	rm -rf pkg/api/openapi
	$(CONTAINER_TOOL) build -t hyperfleet-sentinel-openapi -f Dockerfile.openapi .
	@OPENAPI_IMAGE_ID=$$($(CONTAINER_TOOL) create hyperfleet-sentinel-openapi) && \
		$(CONTAINER_TOOL) cp $$OPENAPI_IMAGE_ID:/local/pkg/api/openapi ./pkg/api/openapi && \
		$(CONTAINER_TOOL) rm $$OPENAPI_IMAGE_ID
	@echo "OpenAPI client generated successfully"

##@ Development

.PHONY: binary
binary: ## Build the sentinel binary
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/sentinel

.PHONY: build
build: binary ## Alias for binary

.PHONY: install
install: ## Build and install binary to GOPATH/bin
	$(GO) install -ldflags "$(LDFLAGS)" ./cmd/sentinel

.PHONY: run
run: binary ## Run the sentinel service
	./$(BINARY_NAME) serve

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)
	rm -rf pkg/api/openapi
	rm -f coverage.out

##@ Testing

.PHONY: test
test: ## Run unit tests
	$(GO) test -v -race -coverprofile=coverage.out ./...

.PHONY: test-coverage
test-coverage: test ## Run tests and show coverage
	$(GO) tool cover -html=coverage.out

##@ Code Quality

.PHONY: fmt
fmt: ## Format Go code
	$(GOFMT) -s -w .

.PHONY: fmt-check
fmt-check: ## Check if code is formatted
	@diff=$$($(GOFMT) -s -d .); \
	if [ -n "$$diff" ]; then \
		echo "Code is not formatted. Run 'make fmt' to fix:"; \
		echo "$$diff"; \
		exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint (requires golangci-lint to be installed)
	golangci-lint run

.PHONY: verify
verify: fmt-check vet ## Run all verification checks

##@ Dependencies

.PHONY: tidy
tidy: ## Tidy go.mod
	$(GO) mod tidy

.PHONY: download
download: ## Download dependencies
	$(GO) mod download

##@ Docker

.PHONY: docker-build
docker-build: ## Build docker image
	docker build -t sentinel:$(VERSION) .

.PHONY: docker-run
docker-run: ## Run docker container
	docker run -it --rm \
		-v $(PWD)/configs:/app/configs \
		sentinel:$(VERSION) serve
