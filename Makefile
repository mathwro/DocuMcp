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

run: docker
	$(COMPOSE_CMD) up

lint:
	golangci-lint run

clean:
	rm -rf bin/

init:
	@if [ -f .env ]; then \
		echo ".env already exists, skipping"; \
	else \
		echo "DOCUMCP_SECRET_KEY=$$(openssl rand -hex 32)" > .env; \
		echo "Created .env with generated secret key"; \
	fi
	@if [ -f config.yaml ]; then \
		echo "config.yaml already exists, skipping"; \
	else \
		printf 'server:\n  port: 8080\n  data_dir: /app/data\n\nsources:\n  # - name: Example Docs\n  #   type: web\n  #   url: https://docs.example.com/\n  #   crawl_schedule: "@weekly"\n' > config.yaml; \
		echo "Created config.yaml with defaults"; \
	fi
