include .bingo/Variables.mk

.DEFAULT_GOAL := help

GO ?= go
GOFMT ?= gofmt

# Binary output directory and name
BIN_DIR := bin
BINARY_NAME := $(BIN_DIR)/sentinel


# Version information
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_DIRTY ?= $(shell git diff --quiet 2>/dev/null || echo "-modified")
VERSION:=$(GIT_SHA)$(GIT_DIRTY)

# Go build flags
LDFLAGS := -X main.version=$(VERSION) \
           -X main.commit=$(GIT_SHA) \
           -X main.date=$(BUILD_DATE)

# Container tool (docker or podman)
CONTAINER_TOOL ?= $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)

# =============================================================================
# Image Configuration
# =============================================================================
IMAGE_REGISTRY ?= quay.io/openshift-hyperfleet
IMAGE_NAME ?= hyperfleet-sentinel
IMAGE_TAG ?= $(VERSION)

# Dev image configuration - set QUAY_USER to push to personal registry
# Usage: QUAY_USER=myuser make image-dev
QUAY_USER ?=
DEV_TAG ?= dev-$(GIT_SHA)

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Code Generation

# OpenAPI spec configuration from hyperfleet-api repository
OPENAPI_SPEC_REF ?= main
OPENAPI_SPEC_URL ?= https://raw.githubusercontent.com/openshift-hyperfleet/hyperfleet-api/$(OPENAPI_SPEC_REF)/openapi/openapi.yaml


# Regenerate openapi types using oapi-codegen
generate: $(OAPI_CODEGEN) 
	@echo "Fetching OpenAPI spec from hyperfleet-api (ref: $(OPENAPI_SPEC_REF))..."
	@mkdir -p openapi
	@curl -sSL -o openapi/openapi.yaml "$(OPENAPI_SPEC_URL)" || \
		(echo "Failed to download OpenAPI spec from $(OPENAPI_SPEC_URL)" && exit 1)
	@echo "OpenAPI spec downloaded successfully"
	rm -rf pkg/api/openapi
	mkdir -p pkg/api/openapi
	$(OAPI_CODEGEN) --config openapi/oapi-codegen.yaml openapi/openapi.yaml
.PHONY: generate
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
test: generate ## Run unit tests (default)
	$(GO) test -v -race -coverprofile=coverage.out ./...

.PHONY: test-unit
test-unit: generate ## Run unit tests only
	$(GO) test -v -race -cover ./internal/config/
	$(GO) test -v -race -cover ./internal/client/
	$(GO) test -v -race -cover ./internal/engine/
	$(GO) test -v -race -cover ./internal/health/
	$(GO) test -v -race -cover ./internal/publisher/
	$(GO) test -v -race -cover ./internal/sentinel/
	$(GO) test -v -race -cover ./pkg/...

.PHONY: test-integration
test-integration: generate ## Run integration tests only
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
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

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

##@ Helm Charts

HELM_CHART_DIR := deployments/helm/sentinel

.PHONY: test-helm
test-helm: ## Test Helm charts (lint, template, validate)
	@echo "Testing Helm charts..."
	@if ! command -v helm > /dev/null; then \
		echo "Error: helm not found. Please install Helm:"; \
		echo "  brew install helm  # macOS"; \
		echo "  curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash  # Linux"; \
		exit 1; \
	fi
	@echo "Linting Helm chart..."
	helm lint $(HELM_CHART_DIR)/
	@echo ""
	@echo "Testing template rendering with default values..."
	helm template test-release $(HELM_CHART_DIR)/ > /dev/null
	@echo "Default values template OK"
	@echo ""
	@echo "Testing template with custom image registry..."
	helm template test-release $(HELM_CHART_DIR)/ \
		--set image.registry=quay.io/openshift-hyperfleet \
		--set image.tag=v1.0.0 > /dev/null
	@echo "Custom image config template OK"
	@echo ""
	@echo "Testing template with PDB enabled..."
	helm template test-release $(HELM_CHART_DIR)/ \
		--set podDisruptionBudget.enabled=true \
		--set podDisruptionBudget.maxUnavailable=1 > /dev/null
	@echo "PDB config template OK"
	@echo ""
	@echo "Testing template with PDB disabled..."
	helm template test-release $(HELM_CHART_DIR)/ \
		--set podDisruptionBudget.enabled=false > /dev/null
	@echo "PDB disabled template OK"
	@echo ""
	@echo "Testing template with RabbitMQ broker..."
	helm template test-release $(HELM_CHART_DIR)/ \
		--set broker.type=rabbitmq \
		--set broker.rabbitmq.url=amqp://user:pass@rabbitmq:5672/hyperfleet > /dev/null
	@echo "RabbitMQ broker template OK"
	@echo ""
	@echo "Testing template with Google Pub/Sub broker..."
	helm template test-release $(HELM_CHART_DIR)/ \
		--set broker.type=googlepubsub \
		--set broker.googlepubsub.projectId=test-project > /dev/null
	@echo "Google Pub/Sub broker template OK"
	@echo ""
	@echo "Testing template with PodMonitoring enabled..."
	helm template test-release $(HELM_CHART_DIR)/ \
		--set monitoring.podMonitoring.enabled=true \
		--set monitoring.podMonitoring.interval=15s > /dev/null
	@echo "PodMonitoring config template OK"
	@echo ""
	@echo "Testing template with ServiceMonitor enabled..."
	helm template test-release $(HELM_CHART_DIR)/ \
		--set monitoring.serviceMonitor.enabled=true \
		--set monitoring.serviceMonitor.interval=30s > /dev/null
	@echo "ServiceMonitor config template OK"
	@echo ""
	@echo "Testing template with PrometheusRule enabled..."
	helm template test-release $(HELM_CHART_DIR)/ \
		--set monitoring.prometheusRule.enabled=true > /dev/null
	@echo "PrometheusRule config template OK"
	@echo ""
	@echo "Testing template with custom resource selector..."
	helm template test-release $(HELM_CHART_DIR)/ \
		--set config.resourceType=nodepools \
		--set config.pollInterval=10s \
		--set config.maxAgeReady=1h > /dev/null
	@echo "Custom resource selector template OK"
	@echo ""
	@echo "All Helm chart tests passed!"

##@ Container Images

.PHONY: image
image: ## Build container image with configurable registry/tag
	@echo "Building image $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) ..."
	$(CONTAINER_TOOL) build \
  	--platform linux/amd64 \
		--build-arg GIT_SHA=$(GIT_SHA) \
		--build-arg GIT_DIRTY=$(GIT_DIRTY) \
		-t $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) .
	@echo "Image built: $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)"

.PHONY: image-push
image-push: image ## Build and push container image
	@echo "Pushing image $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) ..."
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
	@echo "Building dev image quay.io/$(QUAY_USER)/$(IMAGE_NAME):$(DEV_TAG) ..."
	$(CONTAINER_TOOL) build \
		--platform linux/amd64 \
		--build-arg BASE_IMAGE=alpine:3.21 \
		--build-arg GIT_SHA=$(GIT_SHA) \
		--build-arg GIT_DIRTY=$(GIT_DIRTY) \
    -t quay.io/$(QUAY_USER)/$(IMAGE_NAME):$(DEV_TAG) .
	@echo "Pushing dev image..."
	$(CONTAINER_TOOL) push quay.io/$(QUAY_USER)/$(IMAGE_NAME):$(DEV_TAG)
	@echo ""
	@echo "Dev image pushed: quay.io/$(QUAY_USER)/$(IMAGE_NAME):$(DEV_TAG)"
	@echo ""
	@echo "Add to your terraform.tfvars:"
	@echo "  sentinel_image_tag = \"$(DEV_TAG)\""

