SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

DOCKERFILES_DIR  := dockerfiles

# All Dockerfiles in the dockerfiles directory (auto-discovered).
ALL_IMAGES := $(patsubst $(DOCKERFILES_DIR)/Dockerfile.%,%,$(wildcard $(DOCKERFILES_DIR)/Dockerfile.*))

# Build a subset of images or all if none specified.
# Usage:
#   make build                          # builds all images
#   make build IMAGES="go1.25"          # builds only go1.25
#   make build IMAGES="go1.25 go1.26"   # builds go1.25 and go1.26
IMAGES           ?= $(ALL_IMAGES)
REPO             ?= rancher/ci-image
TARGET_PLATFORMS ?= linux/amd64,linux/arm64
GIT_COMMIT       := $(or $(shell git rev-parse --short HEAD 2>/dev/null),unknown)
NO_CACHE         ?=

.PHONY: all help test generate verify build push clean

all: test generate build ## Run tests, generate Dockerfiles, and build all images

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

test: ## Run unit tests
	go test -v -count=1 ./...

generate: ## Generate Dockerfiles from templates and deps.yaml
	go run main.go

verify: ## Verify no uncommitted changes exist
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "Error: uncommitted changes detected:"; \
		git status --porcelain; \
		git diff; \
		exit 1; \
	fi

define buildx
	@if [ -z "$(IMAGES)" ]; then \
		echo "Error: no images found. Run 'make generate' first."; \
		exit 1; \
	fi
	@for img in $(IMAGES); do \
		echo "==> $(1) $${img}"; \
		docker buildx build \
			--file "$(DOCKERFILES_DIR)/Dockerfile.$${img}" \
			--platform "$(TARGET_PLATFORMS)" \
			--provenance mode=max \
			--sbom=true \
			$(if $(NO_CACHE),--no-cache) \
			--tag "$(REPO):$${img}" \
			--tag "$(REPO):$${img}-$(GIT_COMMIT)" \
			$(2) \
			. || exit 1; \
	done
endef

build: ## Build container images for all target platforms
	$(call buildx,Building,)

push: ## Build and push container images to the registry
	$(call buildx,Pushing,--push)

clean: ## Remove generated Dockerfiles
	rm -rf $(DOCKERFILES_DIR)
