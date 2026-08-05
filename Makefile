.PHONY: test-unit
test-unit: ## Run unit tests
	go test -v ./...

.PHONY: test
test: ## Run full test suite
	@echo "Running full test suite"
	@$(MAKE) test-unit

GOLANGCI_LINT := $(shell go env GOPATH)/bin/golangci-lint

.PHONY: lint
lint: ## Run linter (installs golangci-lint via go install if missing)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
	$(GOLANGCI_LINT) run ./... -v
