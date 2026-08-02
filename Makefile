.PHONY: test-unit
test-unit: ## Run unit tests with coverage report
	go test -v -covermode=atomic -coverpkg=./... ./...

.PHONY: test
test: ## Run full test suite
	@echo "Running full test suite"
	@$(MAKE) test-unit
