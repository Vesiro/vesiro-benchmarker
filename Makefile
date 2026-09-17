GO      ?= go
BIN_DIR := bin
PKGS    := ./cmd/... ./internal/...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/Vesiro/vesiro-benchmarker/internal/version.Version=$(VERSION)
DIST_DIR  := dist
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
SHASUM    := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo 'shasum -a 256')

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the bench binary into bin/
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/bench ./cmd/bench

.PHONY: dist
dist: ## Cross-compile release archives with checksums into dist/
	@rm -rf $(DIST_DIR) && mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		stage=$(DIST_DIR)/bench_$(VERSION)_$${os}_$${arch}; \
		mkdir -p $$stage; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $$stage/bench ./cmd/bench || exit 1; \
		cp LICENSE README.md $$stage/; \
		tar -czf $$stage.tar.gz -C $(DIST_DIR) "$$(basename $$stage)"; \
		rm -rf $$stage; \
		echo "  $$stage.tar.gz"; \
	done
	@cd $(DIST_DIR) && $(SHASUM) *.tar.gz > SHA256SUMS
	@echo "  $(DIST_DIR)/SHA256SUMS"

.PHONY: test
test: ## Run all tests
	$(GO) test ./...

.PHONY: test-race
test-race: ## Run all tests under the race detector
	$(GO) test -race ./...

.PHONY: cover
cover: ## Report test coverage per package
	$(GO) test -cover ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format all Go code
	gofmt -w $(shell find cmd internal -name '*.go')

.PHONY: fmt-check
fmt-check: ## Fail if any Go code is not gofmt-clean
	@out=$$(gofmt -l cmd internal); \
	if [ -n "$$out" ]; then \
		echo "not gofmt-clean:"; echo "$$out"; \
		echo "run 'make fmt'"; exit 1; \
	fi

.PHONY: tidy-check
tidy-check: ## Fail if go.mod/go.sum are not tidy
	$(GO) mod tidy
	@if ! git diff --quiet -- go.mod go.sum; then \
		echo "go.mod/go.sum are not tidy — commit the changes made by 'go mod tidy'"; \
		git --no-pager diff --stat -- go.mod go.sum; exit 1; \
	fi

.PHONY: lint
lint: ## Run golangci-lint (must be installed separately)
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found — https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run

.PHONY: check
check: fmt-check vet test-race ## Run the checks CI gates on

.PHONY: clean
clean: ## Remove build output
	rm -rf $(BIN_DIR) $(DIST_DIR)
