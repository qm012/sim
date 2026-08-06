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
