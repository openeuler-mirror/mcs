.PHONY: all build fmt mock-micad clean-all build-vendor build-module vendor-update vendor-verify install remote help

SHIM_NAME := io.containerd.mica.v2

# containerd shim v2 naming convention
SHIM_PARTS := $(subst ., ,$(SHIM_NAME))
SHIM_PARTS_COUNT := $(words $(SHIM_PARTS))
RUNTIME_NAME := $(word $(shell echo $(SHIM_PARTS_COUNT) - 1 | bc),$(SHIM_PARTS))
RUNTIME_VERSION := $(lastword $(SHIM_PARTS))

BUILD_DIRS := builds/
SHIM_DIR := /usr/local/bin/
SHIM_DIR_NONROOT ?= $(HOME)/.local/bin
BINNAME := containerd-shim-$(RUNTIME_NAME)-$(RUNTIME_VERSION)
BIN := $(BUILD_DIRS)$(BINNAME)

# Build configuration
BUILD_MODE ?= vendor
BUILD_ARCH ?= $(if $(GOARCH),$(GOARCH),$(shell go env GOARCH))
BUILD_TYPE ?= debug

# Build mode flags
ifeq ($(BUILD_MODE),vendor)
	GO_BUILD_FLAGS := -mod=vendor
	GO_TEST_FLAGS := -mod=vendor
else ifeq ($(BUILD_MODE),module)
	GO_BUILD_FLAGS := -mod=mod
	GO_TEST_FLAGS := -mod=mod
else
	GO_BUILD_FLAGS := -mod=vendor
	GO_TEST_FLAGS := -mod=vendor
endif

# Build type flags
ifeq ($(BUILD_TYPE),debug)
	BUILD_FLAGS := -tags debug -ldflags "-X 'main.ShimName=${SHIM_NAME}'"
else
	BUILD_FLAGS := -ldflags "-s -w -X 'main.ShimName=${SHIM_NAME}'"
endif

# Cross-compilation flags
ifeq ($(BUILD_ARCH),arm64)
	CROSS_FLAGS := GOOS=linux GOARCH=arm64 CGO_ENABLED=0
	BIN := $(BIN)-arm64
endif

-include .env
TARGET_HOST ?= $(DEPLOY_HOST)
TARGET_PATH ?= $(DEPLOY_PATH)
TARGET_PASS ?= $(DEPLOY_PASS)

all: build

build:
	@echo "🏭 Building binary (MODE=${BUILD_MODE}, ARCH=${BUILD_ARCH}, TYPE=${BUILD_TYPE})..."
	$(CROSS_FLAGS) go build $(GO_BUILD_FLAGS) $(BUILD_FLAGS) -o $(BIN) .

fmt:
	go fmt ./...

mock-micad:
	@echo "🎭 Building and running mock_micad..."
	cd tests/mock_micad && make run

clean-all:
	@echo "🧹 Cleaning up all components..."
	cd tests/mock_micad && make clean
	cd tests/containerd_client && rm -f containerd_client
	rm -f $(BIN) $(BIN)-arm64

# Vendor-specific build targets
build-vendor:
	@echo "📦 Building with vendor dependencies..."
	@$(MAKE) build BUILD_MODE=vendor

build-module:
	@echo "📚 Building with Go modules..."
	@$(MAKE) build BUILD_MODE=module

# Vendor management targets
vendor-update:
	@echo "📦 Updating vendor directory..."
	go mod vendor

vendor-verify:
	@echo "🔍 Verifying vendor directory..."
	go mod verify

install: build
	@echo "🏭 Installing $(BIN) to $(SHIM_DIR)"
	sudo install -m 755 $(BIN) $(SHIM_DIR)$(BINNAME)
	@echo "md5sums:"
	@echo "Source:      $$(md5sum $(BIN))"
	@echo "Installed:   $$(md5sum $(SHIM_DIR)$(BINNAME))"
	@echo "pass --runtime $(SHIM_NAME) to use it"

remote: build
	@if [ -z "$(TARGET_HOST)" ] || [ -z "$(TARGET_PATH)" ]; then \
		echo "Error: Deployment requires environment variables:"; \
		echo "  DEPLOY_HOST - Target host (e.g., root@192.168.7.2)"; \
		echo "  DEPLOY_PATH - Target path (e.g., /root)"; \
		echo ""; \
		echo "Optional variable:"; \
		echo "  DEPLOY_PASS - SSH password (if using password authentication)"; \
		echo ""; \
		echo "  DEPLOY_HOST=root@192.168.7.2 DEPLOY_PATH=/root make deploy"; \
		echo "Usage examples:"; \
		echo "  DEPLOY_HOST=root@192.168.7.2 DEPLOY_PATH=/root DEPLOY_PASS=mypassword make deploy"; \
		exit 1; \
	fi
	@echo "Deploying to $(TARGET_HOST):$(TARGET_PATH)/"
	@if [ -n "$(TARGET_PASS)" ]; then \
		sshpass -p '$(TARGET_PASS)' scp $(BIN) $(TARGET_HOST):$(TARGET_PATH)/$(BINNAME); \
	else \
		scp $(BIN) $(TARGET_HOST):$(TARGET_PATH)/$(BINNAME); \
	fi
	@echo "Deployment complete."

# Doc
doc:
	@echo "Network mode: host network"
	@echo "Netns: implemente it in future"


# Help
help:
	@echo "🚀 Mica Shim Build System"
	@echo ""
	@echo "Configuration Variables:"
	@echo "  BUILD_MODE=vendor|module  - Build mode (default: vendor)"
	@echo "  BUILD_ARCH=amd64|arm64    - Target architecture (default: system arch)"
	@echo "  BUILD_TYPE=debug|release   - Build type (default: debug)"
	@echo ""
	@echo "Essential Commands:"
	@echo "  make build                - Build binary with current config"
	@echo "  make fmt                  - Format Go code"
	@echo "  make mock-micad           - Run mock micad server"
	@echo "  make clean-all            - Clean all build artifacts"
	@echo ""
	@echo "Build Mode Commands:"
	@echo "  make build-vendor         - Build with vendor dependencies"
	@echo "  make build-module         - Build with Go modules"
	@echo "  make vendor-update        - Update vendor directory"
	@echo "  make vendor-verify        - Verify vendor directory"
	@echo ""
	@echo "Deployment Commands:"
	@echo "  make install              - Install binary (requires sudo)"
	@echo "  make remote               - Deploy to remote host"
	@echo ""
	@echo "Examples:"
	@echo "  make build BUILD_ARCH=arm64 BUILD_TYPE=release"
	@echo "  make build BUILD_MODE=module BUILD_TYPE=debug"
	@echo "  make install BUILD_TYPE=release"
