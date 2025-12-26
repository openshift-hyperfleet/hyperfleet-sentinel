.DEFAULT_GOAL := help

GO ?= go
GOFMT ?= gofmt

# Binary output directory and name
BIN_DIR := bin
BINARY_NAME := $(BIN_DIR)/sentinel

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

# =============================================================================
# Image Configuration
# =============================================================================
IMAGE_REGISTRY ?= quay.io/openshift-hyperfleet
IMAGE_NAME ?= sentinel
IMAGE_TAG ?= $(VERSION)

# Dev image configuration - set QUAY_USER to push to personal registry
# Usage: QUAY_USER=myuser make image-dev
QUAY_USER ?=
DEV_TAG ?= dev-$(COMMIT)

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Code Generation

# OpenAPI spec configuration from hyperfleet-api repository
OPENAPI_SPEC_REF ?= main
OPENAPI_SPEC_URL = https://raw.githubusercontent.com/openshift-hyperfleet/hyperfleet-api/$(OPENAPI_SPEC_REF)/openapi/openapi.yaml

.PHONY: generate
generate: ## Generate OpenAPI client from HyperFleet API spec
	@echo "Fetching OpenAPI spec from hyperfleet-api (ref: $(OPENAPI_SPEC_REF))..."
	@mkdir -p openapi
	@curl -sSL -o openapi/openapi.yaml "$(OPENAPI_SPEC_URL)" || \
		(echo "Failed to download OpenAPI spec from $(OPENAPI_SPEC_URL)" && exit 1)
	@echo "OpenAPI spec downloaded successfully"
	@echo "Generating OpenAPI client..."
	@rm -rf pkg/api/openapi
	@mkdir -p pkg/api
	$(CONTAINER_TOOL) build -t hyperfleet-sentinel-openapi -f Dockerfile.openapi .
	@OPENAPI_IMAGE_ID=$$($(CONTAINER_TOOL) create hyperfleet-sentinel-openapi) && \
		$(CONTAINER_TOOL) cp $$OPENAPI_IMAGE_ID:/local/pkg/api/openapi ./pkg/api/openapi && \
		$(CONTAINER_TOOL) rm $$OPENAPI_IMAGE_ID
	@echo "OpenAPI client generated successfully"

##@ Development

.PHONY: build
build: generate ## Build the sentinel binary
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) ./cmd/sentinel

.PHONY: install
install: ## Build and install binary to GOPATH/bin
	$(GO) install -ldflags "$(LDFLAGS)" ./cmd/sentinel

.PHONY: run
run: build ## Run the sentinel service
	./$(BINARY_NAME) serve

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)
	rm -rf pkg/api/openapi
	rm -f coverage.out

##@ Testing

.PHONY: test
test: ## Run unit tests (default)
	$(GO) test -v -race -coverprofile=coverage.out ./...

.PHONY: test-unit
test-unit: ## Run unit tests only
	$(GO) test -v -race -cover ./internal/config/
	$(GO) test -v -race -cover ./internal/client/
	$(GO) test -v -race -cover ./internal/engine/
	$(GO) test -v -race -cover ./internal/publisher/
	$(GO) test -v -race -cover ./internal/sentinel/
	$(GO) test -v -race -cover ./pkg/...

.PHONY: test-integration
test-integration: ## Run integration tests only
	@echo "Running integration tests..."
	TESTCONTAINERS_RYUK_DISABLED=true $(GO) test -v -race -tags=integration ./test/integration/... -timeout 30m

.PHONY: test-all
test-all: ## Run both unit and integration tests
	@echo "Running unit tests..."
	$(MAKE) test
	@echo "Running integration tests..."
	TESTCONTAINERS_RYUK_DISABLED=true $(MAKE) test-integration

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

.PHONY: lint-check
lint-check: fmt-check vet ## Run static code analysis (alias for verify, follows architecture naming)

##@ Dependencies

.PHONY: tidy
tidy: ## Tidy go.mod
	$(GO) mod tidy

.PHONY: download
download: ## Download dependencies
	$(GO) mod download

##@ Container Images

.PHONY: image
image: ## Build container image with configurable registry/tag
	@echo "Building image $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)..."
	$(CONTAINER_TOOL) build -t $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) .
	@echo "Image built: $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)"

.PHONY: image-push
image-push: image ## Build and push container image
	@echo "Pushing image $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)..."
	$(CONTAINER_TOOL) push $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	@echo "Image pushed: $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)"

.PHONY: image-dev
image-dev: ## Build and push to personal Quay registry (requires QUAY_USER)
ifndef QUAY_USER
	@echo "Error: QUAY_USER is not set"
	@echo ""
	@echo "Usage: QUAY_USER=myuser make image-dev"
	@echo ""
	@echo "This will build and push to: quay.io/\$$QUAY_USER/$(IMAGE_NAME):$(DEV_TAG)"
	@exit 1
endif
	@echo "Building dev image quay.io/$(QUAY_USER)/$(IMAGE_NAME):$(DEV_TAG)..."
	$(CONTAINER_TOOL) build -t quay.io/$(QUAY_USER)/$(IMAGE_NAME):$(DEV_TAG) .
	@echo "Pushing dev image..."
	$(CONTAINER_TOOL) push quay.io/$(QUAY_USER)/$(IMAGE_NAME):$(DEV_TAG)
	@echo ""
	@echo "Dev image pushed: quay.io/$(QUAY_USER)/$(IMAGE_NAME):$(DEV_TAG)"
	@echo ""
	@echo "Add to your terraform.tfvars:"
	@echo "  sentinel_image_tag = \"$(DEV_TAG)\""

