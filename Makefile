BINARY   := firex
CMD      := ./cmd/firex
PKG      := ./...
UI_DIR   := web
DIST_DIR := internal/web/dist
BIN_DIR  := bin
LDFLAGS  := -s -w

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: dist-stub
dist-stub: ## Restore the placeholder internal/web/dist the go:embed needs
	@mkdir -p $(DIST_DIR)
	@touch $(DIST_DIR)/.gitkeep

.PHONY: ui-deps
ui-deps: ## Install frontend dependencies
	cd $(UI_DIR) && npm install

.PHONY: ui
ui: ## Build the management UI into internal/web/dist (embedded by the Go build)
	cd $(UI_DIR) && npm run build
	@$(MAKE) --no-print-directory dist-stub

.PHONY: build
build: ui ## Build the single binary with the UI embedded
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

.PHONY: build-go
build-go: dist-stub ## Build the binary without rebuilding the UI
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

.PHONY: run
run: dist-stub ## Run from source (build the UI once first for a usable panel)
	go run $(CMD)

.PHONY: dev
dev: ## Run the Vite dev server against a local backend on :8080
	cd $(UI_DIR) && npm run dev

.PHONY: test
test: dist-stub ## Run the Go test suite
	go test -shuffle=on $(PKG)

.PHONY: vet
vet: dist-stub ## Run go vet
	go vet $(PKG)

.PHONY: fmt
fmt: ## Format Go sources
	gofmt -w .

.PHONY: typecheck
typecheck: ## Typecheck the frontend
	cd $(UI_DIR) && npx tsc -b

.PHONY: verify
verify: vet test typecheck build ## Vet, test, typecheck, then build everything

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)
	rm -rf $(DIST_DIR)
	$(MAKE) dist-stub
