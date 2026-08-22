# Make targets operate on the root module only, even when a go.work
# exists for local development (go tool -modfile rejects workspaces).
export GOWORK := off

.PHONY: test-unit
test-unit: ## Run unit tests
	go test -v ./...

.PHONY: test
test: ## Run full test suite
	@echo "Running full test suite"
	@$(MAKE) test-unit

.PHONY: lint
lint: ## Run golangci lint
	go tool '-modfile=tools/golangci-lint.mod' golangci-lint run -v
