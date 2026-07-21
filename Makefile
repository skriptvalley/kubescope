# Kubescope — build/dev/test targets. See CLAUDE.md "Dev commands".

IMAGE            ?= ghcr.io/skriptvalley/kubescope:latest
PLATFORMS        ?= linux/amd64,linux/arm64
BUILDER          ?= kubescope-builder
KIND_CLUSTER     ?= kubescope
COMPOSE_FILE     ?= build/docker-compose.yml
ENVTEST_K8S_VERSION ?= 1.36.x
GO_PKG_DIRS       = $(shell go list -f '{{.Dir}}' ./...)

.PHONY: dev dev-backend build test lint docker-build docker-build-local docker-run \
        fe-dev fe-build fe-test kind-up kind-down smoke help \
        testenv-up testenv-down testenv-status testenv-run testenv-run-docker \
        compose-config compose-up compose-down \
        e2e-eks-up e2e-eks-kubeconfig e2e-eks-down

help: ## List targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "%-20s %s\n", $$1, $$2}'

## --- Development ---

dev: ## Run backend + frontend in dev mode (Vite proxies /api)
	$(MAKE) -j2 dev-backend fe-dev

dev-backend:
	go run ./cmd/kubescope

fe-dev: web/node_modules ## Run the Vite dev server
	cd web && npm run dev

## --- Build ---

build: fe-build ## Build the kubescope binary with embedded frontend
	go build -trimpath -o bin/kubescope ./cmd/kubescope

fe-build: web/node_modules ## Build the production FE bundle into web/dist
	cd web && npm run build
	@touch web/dist/.gitkeep # vite empties outDir; keep the embed placeholder

web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci

## --- Test / lint ---

test: ## Go unit tests + envtest under the race detector (downloads apiserver binaries on first run)
	KUBEBUILDER_ASSETS="$$(go tool setup-envtest use -p path $(ENVTEST_K8S_VERSION))" go test -race ./...

fe-test: web/node_modules ## vitest + React Testing Library suite
	cd web && npm run test:run

lint: web/node_modules ## Go + TS linters
	@fmt_out="$$(gofmt -l $(GO_PKG_DIRS))"; if [ -n "$$fmt_out" ]; then \
		echo "gofmt needed on:"; echo "$$fmt_out"; exit 1; fi
	go vet ./...
	cd web && npm run lint && npm run typecheck

## --- Docker ---

docker-build: ## Build the multi-arch image (amd64+arm64); PUSH=1 to push
	docker buildx inspect $(BUILDER) >/dev/null 2>&1 || docker buildx create --name $(BUILDER)
	docker buildx build --builder $(BUILDER) --platform $(PLATFORMS) \
		-f build/Dockerfile -t $(IMAGE) $(if $(PUSH),--push) .

docker-build-local: ## Build a host-arch image into the local docker store
	docker buildx build -f build/Dockerfile -t $(IMAGE) --load .

docker-run: ## Run the image with ~/.kube/config mounted read-only
	docker run --rm -p 8080:8080 --user "$$(id -u):$$(id -g)" \
		-v $(HOME)/.kube/config:/kubeconfig:ro $(IMAGE)

## --- kind / smoke ---

kind-up: ## Create the local kind cluster (idempotent)
	@kind get clusters 2>/dev/null | grep -qx $(KIND_CLUSTER) || \
		kind create cluster --config deploy/kind-config.yaml --wait 120s

kind-down: ## Delete the local kind cluster
	kind delete cluster --name $(KIND_CLUSTER)

smoke: ## Build image + run against kind + assert node list end-to-end
	deploy/smoke.sh

testenv-up: ## Bring up the local test environment (2 kind clusters + sample resources)
	deploy/testenv/testenv.sh up

testenv-down: ## Tear down the local test environment
	deploy/testenv/testenv.sh down

testenv-status: ## Show the local test environment status
	deploy/testenv/testenv.sh status

testenv-run: ## Build (if needed) and run kubescope (native) against the test environment
	deploy/testenv/testenv.sh run

testenv-run-docker: ## Run the kubescope container image against the test environment
	deploy/testenv/testenv.sh run --docker

## --- e2e harness: docker-compose (kind default) + opt-in EKS (FB-12) ---

compose-config: ## Prep the kind kubeconfig for docker-compose (writes build/.e2e-kubeconfig)
	deploy/testenv/testenv.sh compose-config

compose-up: ## Launch Kubescope via docker-compose (prep a kubeconfig first; build image: make docker-build-local)
	@test -f build/.e2e-kubeconfig || { echo "no build/.e2e-kubeconfig — run 'make compose-config' (kind) or 'make e2e-eks-kubeconfig' (EKS) first"; exit 1; }
	@u=65532:65532; [ "$$(uname -s)" = Linux ] && u="$$(id -u):$$(id -g)"; \
		KUBESCOPE_COMPOSE_USER="$$u" docker compose -f $(COMPOSE_FILE) up -d
	@echo "Kubescope: http://127.0.0.1:8080  (logs: docker compose -f $(COMPOSE_FILE) logs -f  |  stop: make compose-down)"

compose-down: ## Stop the docker-compose stack and remove the adapted kubeconfig copy
	docker compose -f $(COMPOSE_FILE) down
	@rm -f build/.e2e-kubeconfig

e2e-eks-up: ## [BILLED — teardown mandatory] Provision the opt-in EKS cluster + seed fixtures (ADR-0010)
	cd deploy/e2e-eks && terraform init -input=false && terraform apply

e2e-eks-kubeconfig: ## Mint a static token-kubeconfig for the EKS cluster (compose mounts it; ~15-min TTL)
	deploy/e2e-eks/kubeconfig.sh

e2e-eks-down: ## Destroy the EKS cluster (terraform destroy) — run this when finished to stop billing
	cd deploy/e2e-eks && terraform destroy
