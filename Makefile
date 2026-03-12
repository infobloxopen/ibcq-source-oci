GOTOOLCHAIN ?= go1.26.1
export GOTOOLCHAIN

MODULE     := github.com/infobloxopen/ibcq-source-oci
BINARY     := cq-source-oci
IMAGE      := ghcr.io/infobloxopen/cq-source-oci
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo development)
LDFLAGS    := -s -w -X $(MODULE)/plugin.Version=$(VERSION)
GOFLAGS    := -trimpath -ldflags="$(LDFLAGS)"

# MK-007: default target prints help
.DEFAULT_GOAL := help

.PHONY: help build test test-unit test-coverage e2e lint vet tidy clean \
        docker-build docker-build-multiarch docker-push tag-release \
        setup-e2e teardown-e2e

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'

build: ## Build the plugin binary to bin/
	go build $(GOFLAGS) -o bin/$(BINARY) .

test: lint test-unit ## Run lint + unit tests

test-unit: ## Run unit tests with race detector
	go test -v -count=1 -race ./client/... ./internal/... ./plugin/... ./resources/...

test-coverage: ## Run unit tests with coverage report
	go test -v -count=1 -race -coverprofile=coverage.out ./client/... ./internal/... ./plugin/... ./resources/...
	go tool cover -func=coverage.out

e2e: ## Run e2e tests (requires k3d cluster)
	go test -v -count=1 -race ./tests/e2e/... -timeout 300s

lint: vet ## Run all linters
	@echo "==> Checking gofmt..."
	@test -z "$$(gofmt -l . 2>/dev/null)" || { echo "gofmt needed on:"; gofmt -l .; exit 1; }
	@echo "==> Checking go fix..."
	@go fix ./... 2>/dev/null; \
	 if [ -n "$$(git diff --name-only)" ]; then echo "go fix made changes:"; git diff --name-only; exit 1; fi

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy go modules
	go mod tidy

clean: ## Remove build artifacts
	rm -rf bin/ dist/ coverage.out

# MK-002: single-platform local dev image
docker-build: ## Build Docker image for local dev
	docker build \
		--build-arg VERSION=$(VERSION) \
		-t $(BINARY):local .

# MK-003: multi-arch build
docker-build-multiarch: ## Build multi-arch Docker image (amd64+arm64)
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):latest .

# MK-004: push to GHCR
docker-push: ## Push image to GHCR
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):latest \
		--push .

# MK-005: create and push annotated release tag
tag-release: ## Create release tag (usage: make tag-release RELEASE_VERSION=1.2.3)
ifndef RELEASE_VERSION
	$(error RELEASE_VERSION is required. Usage: make tag-release RELEASE_VERSION=1.2.3)
endif
	git tag -a "v$(RELEASE_VERSION)-$$(git rev-parse --short HEAD)" \
		-m "Release v$(RELEASE_VERSION)"
	git push origin "v$(RELEASE_VERSION)-$$(git rev-parse --short HEAD)"

setup-e2e: ## Set up k3d cluster for e2e tests
	./tests/e2e/setup.sh

teardown-e2e: ## Tear down k3d cluster
	./tests/e2e/setup.sh teardown
