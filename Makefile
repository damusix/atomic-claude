.PHONY: help docker-build docker-up docker-shell hooks hooks-uninstall render bundle link dev-setup triage-scan

.DEFAULT_GOAL := help

export HOST_UID ?= 1000

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

hooks: ## Install repo git hooks (sets core.hooksPath to .githooks)
	git config core.hooksPath .githooks
	@echo "git hooks installed (.githooks/). pre-commit will regen the embedded bundle when source artifacts change."

hooks-uninstall: ## Restore default git hooks path
	git config --unset core.hooksPath
	@echo "git hooks restored to default (.git/hooks/)."

render: ## Render templates/ into commands/ (delegates to atomic/)
	$(MAKE) -C atomic render

bundle: ## Regenerate the embedded artifact bundle (delegates to atomic/)
	$(MAKE) -C atomic bundle

link: ## Symlink root artifacts into .claude/ for dogfooding
	./scripts/link-local.sh

dev-setup: hooks link ## One-shot contributor setup: install git hooks + symlink .claude/
	@echo "dev-setup complete. edit root artifacts; .claude/ mirrors them via symlink."

docker-build: ## Build the eval image
	docker compose build

docker-up: ## Run claude in the eval container
	docker compose run --rm atomic-eval

docker-shell: ## Open a bash shell in the eval container
	docker compose run --rm --entrypoint=bash atomic-eval

triage-scan: ## Classify open GitHub issues by staleness as JSON (deterministic half of /triage-issues; ISSUES="43 50" to scope)
	@./scripts/triage-scan.sh $(ISSUES)
