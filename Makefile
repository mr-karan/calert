APP-BIN := ./bin/calert.bin

LAST_COMMIT := $(shell git rev-parse --short HEAD)
LAST_COMMIT_DATE := $(shell git show -s --format=%ci ${LAST_COMMIT})
VERSION := $(shell git describe --tags)
BUILDSTR := ${VERSION} (Commit: ${LAST_COMMIT_DATE} (${LAST_COMMIT}), Build: $(shell date +"%Y-%m-%d% %H:%M:%S %z"))

.PHONY: build
build: ## Build binary.
	go build -o ${APP-BIN} -ldflags="-X 'main.buildString=${BUILDSTR}'" ./cmd/

.PHONY: build-linux
build-linux: ## Cross-compile a Linux amd64 binary (for deploying to the servers).
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bin/calert.linux-amd64 -ldflags="-X 'main.buildString=${BUILDSTR}'" ./cmd/

.PHONY: run
run: ## Run binary.
	./${APP-BIN} --config ./config.toml

.PHONY: clean
clean: ## Remove temporary files and the `bin` folder.
	rm -rf bin

.PHONY: fresh
fresh: build run

.PHONY: lint
lint:
	docker run --rm -v $(CURDIR):/app -w /app golangci/golangci-lint:v2.12.2 golangci-lint run -v

.PHONY: container-smoke
container-smoke: ## Build and smoke test the Ubuntu and scratch release images.
	./scripts/container-smoke.sh

.PHONY: dev-docker
dev-docker: clean build ## Build and spawns docker containers for the entire suite (Alertmanager/Prometheus/calert).
	cd dev; \
	echo "Current UID: $(shell id -u):$(shell id -g)"; \
	CURRENT_UID=$(shell id -u):$(shell id -g) docker compose up

.PHONY: rm-dev-docker
rm-dev-docker: clean build ## Delete the docker containers including volumes.
	cd dev; \
	docker compose down -v ; \
