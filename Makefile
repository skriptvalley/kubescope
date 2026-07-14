# Kubescope — target stubs. Real implementations land in sprints (see docs/BUILD-PLAN.md).

.PHONY: dev build test lint docker-build docker-run fe-dev fe-build fe-test kind-up kind-down smoke

dev: ## Run backend + frontend in dev mode
	@echo "not implemented (Sprint 0)"

build: ## Build the kubescope binary with embedded frontend
	@echo "not implemented (Sprint 0)"

test: ## Run Go + frontend tests
	@echo "not implemented (Sprint 0)"

lint: ## Lint Go + TypeScript
	@echo "not implemented (Sprint 0)"

docker-build: ## Build the multi-arch Docker image
	@echo "not implemented (Sprint 0)"

docker-run: ## Run the container with a mounted kubeconfig
	@echo "not implemented (Sprint 0)"

fe-dev: ## Run the Vite dev server
	@echo "not implemented (Sprint 0)"

fe-build: ## Build the frontend for embedding
	@echo "not implemented (Sprint 0)"

fe-test: ## Run frontend tests (vitest + React Testing Library)
	@echo "not implemented (Sprint 0)"

kind-up: ## Create the local kind cluster for testing
	@echo "not implemented (Sprint 0)"

kind-down: ## Delete the local kind cluster
	@echo "not implemented (Sprint 0)"

smoke: ## Smoke-test the container against kind
	@echo "not implemented (Sprint 0)"
