# Kubescope — build/dev/test targets. See CLAUDE.md "Dev commands".

IMAGE            ?= ghcr.io/skriptvalley/kubescope:latest
PLATFORMS        ?= linux/amd64,linux/arm64
BUILDER          ?= kubescope-builder
KIND_CLUSTER     ?= kubescope
ENVTEST_K8S_VERSION ?= 1.36.x
GO_PKG_DIRS       = $(shell go list -f '{{.Dir}}' ./...)

.PHONY: dev dev-backend build test lint docker-build docker-build-local docker-run \
        fe-dev fe-build fe-test kind-up kind-down smoke help

help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "%-20s %s\n", $$1, $$2}'

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

lint: ## Go + TS linters
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
