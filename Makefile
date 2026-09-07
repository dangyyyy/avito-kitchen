BINARY_DIR := bin
COMPOSE := docker compose

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Sync go.mod and go.sum
	go mod tidy

.PHONY: build
build: ## Build both services into ./bin
	go build -o $(BINARY_DIR)/api ./cmd/api
	go build -o $(BINARY_DIR)/restaurant ./cmd/restaurant

.PHONY: fmt
fmt: ## Format source code
	golangci-lint fmt ./...

.PHONY: lint
lint: ## Run static analysis
	golangci-lint run ./...

.PHONY: test
test: ## Run tests with the race detector
	go test ./... -race -count=1

.PHONY: up
up: ## Start the whole stack
	$(COMPOSE) up -d --build

.PHONY: down
down: ## Stop the stack and remove volumes
	$(COMPOSE) down -v

.PHONY: ps
ps: ## Show running containers
	$(COMPOSE) ps

.PHONY: logs
logs: ## Follow service logs
	$(COMPOSE) logs -f api restaurant

.PHONY: migrate-up
migrate-up: ## Apply database migrations
	$(COMPOSE) run --rm migrator up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration
	$(COMPOSE) run --rm migrator down

.PHONY: migrate-status
migrate-status: ## Show migration status
	$(COMPOSE) run --rm migrator status

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BINARY_DIR)
