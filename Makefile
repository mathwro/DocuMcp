.PHONY: build test lint docker run clean

# Auto-detect container runtime: prefer podman if available, fall back to docker
CONTAINER_RUNTIME := $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
COMPOSE_CMD := $(shell command -v podman-compose 2>/dev/null || echo "$(CONTAINER_RUNTIME) compose")

build:
	CGO_ENABLED=1 go build -tags sqlite_fts5 -o bin/documcp ./cmd/documcp

test:
	CGO_ENABLED=1 go test -tags sqlite_fts5 ./... -v -timeout 60s

docker:
	$(CONTAINER_RUNTIME) build -t documcp:local .

run: docker
	$(COMPOSE_CMD) up

lint:
	golangci-lint run

clean:
	rm -rf bin/
