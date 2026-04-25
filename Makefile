.PHONY: build test lint docker run clean init

# Auto-detect container runtime: prefer podman if available, fall back to docker
CONTAINER_RUNTIME := $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
COMPOSE_CMD := $(shell command -v podman-compose 2>/dev/null || echo "$(CONTAINER_RUNTIME) compose")

build:
	CGO_ENABLED=1 go build -tags sqlite_fts5 -o bin/documcp ./cmd/documcp

test:
	CGO_ENABLED=1 go test -tags sqlite_fts5 ./... -v -timeout 60s

docker:
	$(CONTAINER_RUNTIME) build -t documcp:local .

run: $(CONTAINER_RUNTIME)
	$(COMPOSE_CMD) up

lint:
	golangci-lint run

clean:
	rm -rf bin/

init:
	@touch .env
	@if grep -q '^DOCUMCP_SECRET_KEY=' .env; then \
		echo "DOCUMCP_SECRET_KEY already in .env, skipping"; \
	else \
		echo "DOCUMCP_SECRET_KEY=$$(openssl rand -hex 32)" >> .env; \
		echo "Added DOCUMCP_SECRET_KEY to .env"; \
	fi
	@echo "config.yaml is optional. Copy config.example.yaml to config.yaml if you want declarative source seeding."
