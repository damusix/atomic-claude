.PHONY: help docker-build docker-up docker-shell

.DEFAULT_GOAL := help

export HOST_UID ?= 1000

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

docker-build: ## Build the eval image
	docker compose build

docker-up: ## Run claude in the eval container
	docker compose run --rm atomic-eval

docker-shell: ## Open a bash shell in the eval container
	docker compose run --rm --entrypoint=bash atomic-eval
